package wechat

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/channel"
)

const (
	defaultAPITimeout      = 15 * time.Second
	defaultLongPollTimeout = 40 * time.Second
)

type Client struct {
	cfg  Config
	http *http.Client
}

func NewClient(cfg Config) *Client {
	return NewClientWithHTTP(cfg, &http.Client{})
}

func NewClientWithHTTP(cfg Config, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{cfg: cfg, http: httpClient}
}

func (c *Client) GetUpdates(ctx context.Context, cursor string, timeout time.Duration) (GetUpdatesResponse, error) {
	if timeout <= 0 {
		timeout = defaultLongPollTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var resp GetUpdatesResponse
	err := c.post(ctx, c.cfg.EffectiveBaseURL(), "ilink/bot/getupdates", map[string]any{
		"get_updates_buf": cursor,
		"base_info":       c.baseInfo(),
	}, c.cfg.Token, &resp)
	if err != nil {
		return resp, err
	}
	if resp.Ret != 0 || resp.ErrCode != 0 {
		return resp, fmt.Errorf("wechat getupdates ret=%d errcode=%d: %s", resp.Ret, resp.ErrCode, resp.ErrMsg)
	}
	return resp, nil
}

func (c *Client) SendText(ctx context.Context, msg channel.Message, text string) error {
	if msg.UserID == "" || msg.ReplyToken == "" {
		return fmt.Errorf("wechat reply requires user id and context token")
	}
	text = FormatText(text)
	ctx, cancel := context.WithTimeout(ctx, defaultAPITimeout)
	defer cancel()
	var resp struct {
		Ret    int    `json:"ret,omitempty"`
		ErrMsg string `json:"errmsg,omitempty"`
	}
	err := c.post(ctx, c.cfg.EffectiveBaseURL(), "ilink/bot/sendmessage", map[string]any{
		"msg": Message{
			ToUserID:     msg.UserID,
			MessageType:  2,
			MessageState: 2,
			ContextToken: msg.ReplyToken,
			ItemList:     []MessageItem{{Type: 1, TextItem: &TextItem{Text: text}}},
		},
		"base_info": c.baseInfo(),
	}, c.cfg.Token, &resp)
	if err != nil {
		return err
	}
	if resp.Ret != 0 {
		return fmt.Errorf("wechat sendmessage ret=%d: %s", resp.Ret, resp.ErrMsg)
	}
	return nil
}

func (c *Client) StartLogin(ctx context.Context, localTokens []string) (QRCodeResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultAPITimeout)
	defer cancel()
	var resp QRCodeResponse
	err := c.post(ctx, DefaultBaseURL, "ilink/bot/get_bot_qrcode?bot_type=3", map[string]any{
		"local_token_list": localTokens,
	}, "", &resp)
	return resp, err
}

func (c *Client) PollLogin(ctx context.Context, baseURL, qrCode, verifyCode string) (QRStatusResponse, error) {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	query := "ilink/bot/get_qrcode_status?qrcode=" + url.QueryEscape(qrCode)
	if verifyCode != "" {
		query += "&verify_code=" + url.QueryEscape(verifyCode)
	}
	ctx, cancel := context.WithTimeout(ctx, defaultLongPollTimeout)
	defer cancel()
	var resp QRStatusResponse
	err := c.get(ctx, baseURL, query, &resp)
	return resp, err
}

func (c *Client) baseInfo() BaseInfo {
	botAgent := c.cfg.BotAgent
	if botAgent == "" {
		botAgent = "SoloQueue"
	}
	return BaseInfo{ChannelVersion: c.cfg.Version, BotAgent: botAgent}
}

func (c *Client) post(ctx context.Context, baseURL, endpoint string, body any, token string, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL(baseURL, endpoint), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	c.setHeaders(req, token, true)
	return c.do(req, out)
}

func (c *Client) get(ctx context.Context, baseURL, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointURL(baseURL, endpoint), nil)
	if err != nil {
		return err
	}
	c.setHeaders(req, "", false)
	return c.do(req, out)
}

func (c *Client) setHeaders(req *http.Request, token string, authenticated bool) {
	req.Header.Set("iLink-App-Id", "bot")
	req.Header.Set("iLink-App-ClientVersion", clientVersion(c.cfg.Version))
	if authenticated || token != "" {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("AuthorizationType", "ilink_bot_token")
		req.Header.Set("X-WECHAT-UIN", randomWechatUIN())
	}
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("wechat API %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode wechat API response: %w", err)
	}
	return nil
}

func endpointURL(baseURL, endpoint string) string {
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(endpoint, "/")
}

func randomWechatUIN() string {
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return base64.StdEncoding.EncodeToString([]byte(strconv.FormatInt(time.Now().UnixNano()&0xffffffff, 10)))
	}
	n := binary.BigEndian.Uint32(raw[:])
	return base64.StdEncoding.EncodeToString([]byte(strconv.FormatUint(uint64(n), 10)))
}

func clientVersion(version string) string {
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	values := [3]uint64{}
	for i := 0; i < len(parts) && i < 3; i++ {
		part := parts[i]
		if j := strings.IndexByte(part, '-'); j >= 0 {
			part = part[:j]
		}
		values[i], _ = strconv.ParseUint(part, 10, 8)
	}
	return strconv.FormatUint((values[0]<<16)|(values[1]<<8)|values[2], 10)
}
