package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/channel/wechat"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/infra/db"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
	"github.com/xiaobaitu/soloqueue/internal/runtime"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

func TestWechatNotificationsRestoreAndReloadSavedCredentials(t *testing.T) {
	dir := t.TempDir()
	log, err := logger.New(dir, logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	var mu sync.Mutex
	var sends []string
	stopPolling := make(chan struct{})
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ilink/bot/getupdates":
			select {
			case <-r.Context().Done():
			case <-stopPolling:
			}
			return
		case "/ilink/bot/getuploadurl":
			fmt.Fprintf(w, `{"ret":0,"upload_full_url":%q}`, server.URL+"/upload")
			return
		case "/upload":
			w.Header().Set("x-encrypted-param", "fake-download-param")
			w.WriteHeader(200)
			return
		case "/ilink/bot/sendmessage":
			var body struct {
				Msg struct {
					To      string `json:"to_user_id"`
					Context string `json:"context_token"`
				} `json:"msg"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Msg.To != "recipient" || body.Msg.Context != "fake-context" {
				t.Error("wrong saved recipient/context")
			}
			mu.Lock()
			sends = append(sends, r.Header.Get("Authorization"))
			mu.Unlock()
			fmt.Fprint(w, `{"ret":0}`)
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	defer close(stopPolling)
	cfg, err := config.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	account := config.WechatBotConfig{ID: "logical-account", Enabled: true, BotID: "bot-identity", BotToken: "fake-token-v1", BaseURL: server.URL, BindType: "l1", WhitelistEnabled: true, Whitelist: []string{"recipient"}}
	if err := cfg.UpdateWechatBots([]config.WechatBotConfig{account}); err != nil {
		t.Fatal(err)
	}
	route := channel.Message{Channel: "wechat", AccountID: account.BotID, ConversationID: "conversation", UserID: "recipient", ReplyToken: "fake-context"}
	data, _ := json.Marshal(route)
	databasePath := filepath.Join(dir, "notifications.db")
	initial, err := db.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := initial.SaveChannelSenderData("l1-session", "wechat", string(data)); err != nil {
		t.Fatal(err)
	}
	if err := initial.SaveChannelSenderData("research", "wechat", string(data)); err != nil {
		t.Fatal(err)
	}
	if err := initial.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := db.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	restoredConfig, err := config.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := restoredConfig.Load(); err != nil {
		t.Fatal(err)
	}
	sessions := session.NewSessionManager(func(context.Context, string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
		return &agent.Agent{Def: agent.Definition{NotifyChannel: "wechat", Channels: map[string]string{"wechat": "logical-account"}}}, nil, nil, nil
	}, log)
	sessions.SetChannelMetadataStore(restored)
	permanent, err := sessions.Init(context.Background(), "L1")
	if err != nil {
		t.Fatal(err)
	}
	defer permanent.Close()
	l2Store := session.NewL2SessionStore(nil, dir, log)
	l2Store.SetChannelMetadataStore(restored)
	oldText, oldMedia := channel.GetSenderFactory("wechat"), channel.GetMediaSenderFactory("wechat")
	defer channel.RegisterSenderFactory("wechat", oldText)
	defer channel.RegisterMediaSenderFactory("wechat", oldMedia)
	manager := NewWechatBotManager(restoredConfig, sessions, l2Store, &runtime.Stack{}, dir, "test", log, nil, nil)
	defer manager.Shutdown()
	manager.Reload()
	// No inbound messages have been received in this process.
	if err := permanent.SendViaChannel(context.Background(), "after restart"); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(dir, "attachment.txt")
	if err := os.WriteFile(mediaPath, []byte("attachment"), 0600); err != nil {
		t.Fatal(err)
	}
	media := []channel.OutboundMedia{{Kind: channel.MediaFile, Path: mediaPath, FileName: "attachment.txt"}}
	if err := permanent.SendMediaViaChannel(context.Background(), media); err != nil {
		t.Fatal(err)
	}
	// Register live closures, then renew credentials without receiving another message.
	manager.bridges[0].OnMessage(context.Background(), channel.Message{Channel: "wechat", AccountID: account.BotID, ConversationID: "conversation", UserID: "recipient", ReplyToken: "fake-context", Text: "/help"})
	account.BotToken = "fake-token-v2"
	if err := restoredConfig.UpdateWechatBots([]config.WechatBotConfig{account}); err != nil {
		t.Fatal(err)
	}
	manager.Reload()
	if err := permanent.SendViaChannel(context.Background(), "after token renewal"); err != nil {
		t.Fatal(err)
	}
	if err := permanent.SendMediaViaChannel(context.Background(), media); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := append([]string(nil), sends...)
	mu.Unlock()
	if len(got) != 5 || got[0] != "Bearer fake-token-v1" || got[1] != "Bearer fake-token-v1" || got[3] != "Bearer fake-token-v2" || got[4] != "Bearer fake-token-v2" {
		t.Fatalf("unexpected credential versions for %d sends", len(got))
	}
	// A changed L1 account binding cannot reuse the old recipient route.
	roles := filepath.Join(dir, "channels.yaml")
	if err := os.WriteFile(roles, []byte("notify_channel: wechat\nchannels:\n  wechat: different-account\n"), 0600); err != nil {
		t.Fatal(err)
	}
	permanent.SetL1NotifyChannelPath(roles)
	if err := permanent.SendViaChannel(context.Background(), "wrong binding"); err == nil || !strings.Contains(err.Error(), "binding_mismatch") {
		t.Fatalf("binding error = %v", err)
	}
	// L2 routes are loaded by group after restart, without activating any interactive session.
	account.BindType = "l2"
	account.BindAgent = "research"
	manager.ReloadWithSettings(config.Settings{WechatBots: []config.WechatBotConfig{account}})
	temporary := session.NewSession("cron-random-session", "research", &agent.Agent{Def: agent.Definition{NotifyChannel: "wechat", Channels: map[string]string{"wechat": "logical-account"}}}, nil, nil, log.Child())
	defer temporary.Close()
	l2Store.ApplyChannelSendersTo("research", temporary)
	if err := temporary.SendViaChannel(context.Background(), "L2 restored"); err != nil {
		t.Fatal(err)
	}
	if err := temporary.SendMediaViaChannel(context.Background(), media); err != nil {
		t.Fatal(err)
	}
}

func TestWechatNotificationRejectsStaleOrAmbiguousRoutes(t *testing.T) {
	account := config.WechatBotConfig{ID: "logical", Enabled: true, BotID: "bot", BotToken: "fake-token", BindType: "l1", WhitelistEnabled: true, Whitelist: []string{"recipient"}}
	route := channel.Message{Channel: "wechat", AccountID: "bot", UserID: "recipient", ReplyToken: "fake-context"}
	for _, tc := range []struct {
		name    string
		mutate  func(*config.WechatBotConfig, *channel.Message)
		binding channel.NotificationBinding
		want    string
	}{
		{"disabled", func(a *config.WechatBotConfig, _ *channel.Message) { a.Enabled = false }, channel.NotificationBinding{}, "account_unavailable"},
		{"removed", func(a *config.WechatBotConfig, _ *channel.Message) { a.BotID = "other-bot" }, channel.NotificationBinding{}, "account_unavailable"},
		{"credentials missing", func(a *config.WechatBotConfig, _ *channel.Message) { a.BotToken = "" }, channel.NotificationBinding{}, "account_unavailable"},
		{"legacy missing account", func(_ *config.WechatBotConfig, m *channel.Message) { m.AccountID = "" }, channel.NotificationBinding{}, "route_missing"},
		{"context missing", func(_ *config.WechatBotConfig, m *channel.Message) { m.ReplyToken = "" }, channel.NotificationBinding{}, "route_missing"},
		{"whitelist", func(a *config.WechatBotConfig, _ *channel.Message) { a.Whitelist = []string{"another"} }, channel.NotificationBinding{}, "recipient_denied"},
		{"binding", func(*config.WechatBotConfig, *channel.Message) {}, channel.NotificationBinding{AccountID: "other-account"}, "binding_mismatch"},
		{"L2 group", func(a *config.WechatBotConfig, _ *channel.Message) { a.BindType = "l2"; a.BindAgent = "research" }, channel.NotificationBinding{Group: "different-group"}, "binding_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, m := account, route
			tc.mutate(&a, &m)
			manager := &WechatBotManager{accounts: map[string]config.WechatBotConfig{a.BotID: a}, clients: map[string]*wechat.Client{a.BotID: wechat.NewClient(a.ToWechatConfig("test"))}}
			raw, _ := json.Marshal(m)
			_, _, err := manager.notificationClient(channel.ContextWithNotificationBinding(context.Background(), tc.binding), raw)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("diagnostic = %v, want %s", err, tc.want)
			}
		})
	}
	manager := &WechatBotManager{}
	if _, _, err := manager.notificationClient(context.Background(), []byte(`{"ReplyToken":`)); err == nil || !strings.Contains(err.Error(), "route_invalid") {
		t.Fatalf("malformed route = %v", err)
	}
}
