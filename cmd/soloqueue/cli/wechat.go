package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/channel"
	qqbot "github.com/xiaobaitu/soloqueue/internal/channel/qq"
	"github.com/xiaobaitu/soloqueue/internal/channel/wechat"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/runtime"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

type WechatBotManager struct {
	mu            sync.Mutex
	cfg           *config.GlobalService
	mgr           *session.SessionManager
	l2Store       *session.L2SessionStore
	rt            *runtime.Stack
	workDir       string
	version       string
	mainLog       *logger.Logger
	supervisorsFn func() []*agent.Supervisor
	registry      *agent.Registry
	gateways      []*wechat.Gateway
	bridges       []*channel.TextBridge
	clients       map[string]*wechat.Client
}

type wechatCredentialStore struct {
	cfg     *config.GlobalService
	version string
	onSaved func()
}

func (s *wechatCredentialStore) LocalTokens() []string {
	settings := s.cfg.Get()
	tokens := make([]string, 0, len(settings.WechatBots))
	for _, account := range settings.WechatBots {
		if account.BotToken != "" {
			tokens = append(tokens, account.BotToken)
		}
	}
	return tokens
}

func (s *wechatCredentialStore) SaveWechatCredential(_ context.Context, req wechat.LoginRequest, status wechat.QRStatusResponse) error {
	bots := s.cfg.Get().WechatBots
	account := config.WechatBotConfig{
		ID: req.AccountID, Name: req.Name, Enabled: true,
		BotToken: status.BotToken, BotID: status.BotID, BaseURL: status.BaseURL,
		BotAgent: "SoloQueue/" + s.version, BindType: req.BindType, BindAgent: req.BindAgent,
	}
	updated := false
	for i := range bots {
		if bots[i].ID == req.AccountID {
			account.WhitelistEnabled = bots[i].WhitelistEnabled
			account.Whitelist = bots[i].Whitelist
			bots[i] = account
			updated = true
			break
		}
	}
	if !updated {
		bots = append(bots, account)
	}
	if err := s.cfg.UpdateWechatBots(bots); err != nil {
		return err
	}
	if s.onSaved != nil {
		s.onSaved()
	}
	return nil
}

func NewWechatBotManager(cfg *config.GlobalService, mgr *session.SessionManager, l2Store *session.L2SessionStore, rt *runtime.Stack, workDir, version string, mainLog *logger.Logger, supervisorsFn func() []*agent.Supervisor, registry *agent.Registry) *WechatBotManager {
	m := &WechatBotManager{cfg: cfg, mgr: mgr, l2Store: l2Store, rt: rt, workDir: workDir, version: version, mainLog: mainLog, supervisorsFn: supervisorsFn, registry: registry, clients: make(map[string]*wechat.Client)}

	channel.RegisterSenderFactory("wechat", func(ctx context.Context, data []byte, text string) error {
		var msg channel.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			return err
		}

		m.mu.Lock()
		client := m.clients[msg.AccountID]
		if client == nil && msg.AccountID == "" && len(m.clients) == 1 {
			for _, only := range m.clients {
				client = only
			}
		}
		m.mu.Unlock()

		if client == nil {
			return fmt.Errorf("no active wechat bridge available")
		}
		return client.SendText(ctx, msg, text)
	})
	channel.RegisterMediaSenderFactory("wechat", func(ctx context.Context, data []byte, media []channel.OutboundMedia) error {
		var msg channel.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			return err
		}
		m.mu.Lock()
		client := m.clients[msg.AccountID]
		if client == nil && msg.AccountID == "" && len(m.clients) == 1 {
			for _, only := range m.clients {
				client = only
			}
		}
		m.mu.Unlock()
		if client == nil {
			return fmt.Errorf("no active wechat bridge for account %q", msg.AccountID)
		}
		return client.SendMedia(ctx, msg, media)
	})

	return m
}

func (m *WechatBotManager) Reload() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, gateway := range m.gateways {
		gateway.Close()
	}
	m.gateways = nil
	m.bridges = nil
	m.clients = make(map[string]*wechat.Client)

	settings := m.cfg.Get()
	var transcriber *qqbot.Transcriber
	if settings.Speech.Enabled {
		modelDir := settings.Speech.ModelDir
		if modelDir == "" {
			modelDir = filepath.Join(m.workDir, "models")
		}
		candidate := qqbot.NewTranscriber(settings.Speech.Model, modelDir)
		if candidate.Available() {
			transcriber = candidate
		}
	}
	for _, baseCfg := range settings.WechatBots {
		wxCfg := baseCfg.ToWechatConfig(m.version)
		if !wxCfg.Enabled {
			continue
		}
		if wxCfg.Token == "" || wxCfg.BotID == "" {
			m.mainLog.Warn(logger.CatApp, "wechat bot enabled but token/botId not configured, skipping", "bot_id", baseCfg.ID)
			continue
		}
		wxLog := m.mainLog
		if log, err := logger.New(m.workDir,
			logger.WithLevel(logger.ParseLogLevel(settings.Log.Level)),
			logger.WithConsole(settings.Log.Console),
			logger.WithFile(settings.Log.File),
			logger.WithLogSubdir("wechat-"+baseCfg.ID),
		); err == nil {
			wxLog = log
		} else {
			m.mainLog.Warn(logger.CatApp, "failed to create wechat logger, using main logger", "bot_id", baseCfg.ID, "err", err)
		}

		provider := newChannelSessionProvider(channelBinding{
			channelID: "wechat",
			accountID: wxCfg.BotID,
			bindType:  baseCfg.BindType,
			bindAgent: baseCfg.BindAgent,
		}, m.mgr, m.l2Store, m.rt, m.workDir, wxLog, m.supervisorsFn, m.registry)
		client := wechat.NewClientWithLogger(wxCfg, wxLog)
		bridge := channel.NewTextBridge(provider, client, wxLog, m.version, baseCfg.WhitelistEnabled, baseCfg.Whitelist)
		if transcriber != nil {
			bridge.SetVoiceTranscriber(transcriber.Transcribe)
		}
		gateway := wechat.NewGateway(wxCfg, client, bridge, wxLog)
		m.gateways = append(m.gateways, gateway)
		m.bridges = append(m.bridges, bridge)
		m.clients[wxCfg.BotID] = client

		go func() {
			if err := gateway.Run(context.Background()); err != nil && err != wechat.ErrClosed {
				wxLog.Warn(logger.CatApp, "wechat gateway stopped", "err", err.Error())
			}
		}()
	}
	m.mainLog.Info(logger.CatApp, "WeChat bots hot-reloaded", "bot_count", len(m.gateways))
}

func (m *WechatBotManager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, gateway := range m.gateways {
		gateway.Close()
	}
	m.gateways = nil
	m.bridges = nil
	m.clients = make(map[string]*wechat.Client)
}

func WechatCmd(version string) *cobra.Command {
	cmd := &cobra.Command{Use: "wechat", Short: "Manage WeChat iLink integration"}
	cmd.AddCommand(wechatLoginCmd(version))
	return cmd
}

func wechatLoginCmd(version string) *cobra.Command {
	var id, name, bindType, bindAgent string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authorize a WeChat account by QR code",
		RunE: func(cmd *cobra.Command, args []string) error {
			if bindType != "l1" && bindType != "l2" {
				return fmt.Errorf("bind-type must be l1 or l2")
			}
			if bindType == "l2" && bindAgent == "" {
				return fmt.Errorf("bind-agent is required when bind-type is l2")
			}
			workDir, err := config.DefaultWorkDir()
			if err != nil {
				return err
			}
			cfg, err := config.Init(workDir)
			if err != nil {
				return err
			}
			client := wechat.NewClient(wechat.Config{Version: version, BotAgent: "SoloQueue/" + version})
			var localTokens []string
			for _, account := range cfg.Get().WechatBots {
				if account.BotToken != "" {
					localTokens = append(localTokens, account.BotToken)
				}
			}
			qr, err := client.StartLogin(cmd.Context(), localTokens)
			if err != nil {
				return fmt.Errorf("start WeChat login: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Please scan the QR code using WeChat (open link if terminal cannot render image):\n%s\n", qr.QRCodeImageURL)

			pollBaseURL := wechat.DefaultBaseURL
			verifyCode := ""
			reader := bufio.NewReader(cmd.InOrStdin())
			for {
				status, err := client.PollLogin(cmd.Context(), pollBaseURL, qr.QRCode, verifyCode)
				if err != nil {
					if cmd.Context().Err() != nil {
						return cmd.Context().Err()
					}
					time.Sleep(time.Second)
					continue
				}
				switch status.Status {
				case "wait", "scaned":
					verifyCode = ""
				case "scaned_but_redirect":
					if status.RedirectHost != "" {
						pollBaseURL = "https://" + status.RedirectHost
					}
				case "need_verifycode":
					fmt.Fprint(cmd.OutOrStdout(), "Enter the verification code displayed on your WeChat app: ")
					line, readErr := reader.ReadString('\n')
					if readErr != nil {
						return readErr
					}
					verifyCode = strings.TrimSpace(line)
					continue
				case "confirmed":
					if status.BotToken == "" || status.BotID == "" {
						return fmt.Errorf("WeChat login confirmed without bot token or bot id")
					}
					bots := cfg.Get().WechatBots
					account := config.WechatBotConfig{ID: id, Name: name, Enabled: true, BotToken: status.BotToken, BotID: status.BotID, BaseURL: status.BaseURL, BotAgent: "SoloQueue/" + version, BindType: bindType, BindAgent: bindAgent}
					updated := false
					for i := range bots {
						if bots[i].ID == id {
							bots[i] = account
							updated = true
						}
					}
					if !updated {
						bots = append(bots, account)
					}
					if err := cfg.UpdateWechatBots(bots); err != nil {
						return err
					}
					fmt.Fprintf(cmd.OutOrStdout(), "WeChat account connected and saved as %s.\n", id)
					return nil
				case "expired", "verify_code_blocked":
					return fmt.Errorf("WeChat QR code expired or verification was blocked; run login again")
				case "binded_redirect":
					return fmt.Errorf("this WeChat account is already bound; existing local credentials are required")
				}
				time.Sleep(time.Second)
			}
		},
	}
	cmd.Flags().StringVar(&id, "id", "default", "local account identifier")
	cmd.Flags().StringVar(&name, "name", "WeChat", "display name")
	cmd.Flags().StringVar(&bindType, "bind-type", "l1", "session binding: l1 or l2")
	cmd.Flags().StringVar(&bindAgent, "bind-agent", "", "L2 agent template id")
	return cmd
}
