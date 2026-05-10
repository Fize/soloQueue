package qqbot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── Errors ──────────────────────────────────────────────────────────────────

var (
	ErrAccessTokenExpired = errors.New("qqbot: access token expired")
	ErrAPICallFailed      = errors.New("qqbot: api call failed")
)

// ─── Token Response ──────────────────────────────────────────────────────────

// tokenResponse represents the response from the QQ Bot access_token API.
type tokenResponse struct {
	Code        int    `json:"code"`
	Message     string `json:"message"`
	AccessToken string `json:"access_token"`
	ExpiresIn   string `json:"expires_in"` // seconds, returned as string by QQ API
}

// ─── Message Send Request/Response ──────────────────────────────────────────

// MessageReq is the request body for sending a message.
type MessageReq struct {
	Content string `json:"content"`
	MsgType int    `json:"msg_type"` // 0=text, 1=markdown, 2=ark, 3=embed
	MsgID   string `json:"msg_id,omitempty"`  // event ID for passive reply
	MsgSeq  int    `json:"msg_seq,omitempty"`  // seq for passive reply
}

// MessageResp is the response from sending a message.
type MessageResp struct {
	ID string `json:"id"`
}

// ─── APIClient ────────────────────────────────────────────────────────────────

// APIClient manages HTTP calls to the QQ OpenAPI, including access_token lifecycle.
type APIClient struct {
	appID     string
	appSecret string
	baseURL   string
	log       *logger.Logger
	client    *http.Client

	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

// NewAPIClient creates a new QQ OpenAPI client.
func NewAPIClient(cfg Config, log *logger.Logger) *APIClient {
	return &APIClient{
		appID:     cfg.AppID,
		appSecret: cfg.AppSecret,
		baseURL:   cfg.APIBaseURL(),
		log:       log,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ─── Token Management ────────────────────────────────────────────────────────

// AccessToken returns a valid access token, refreshing if necessary.
func (a *APIClient) AccessToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Return cached token if still valid (with 5 min buffer)
	if a.accessToken != "" && time.Now().Before(a.expiresAt.Add(-5*time.Minute)) {
		return a.accessToken, nil
	}

	return a.refreshTokenLocked(ctx)
}

func (a *APIClient) refreshTokenLocked(ctx context.Context) (string, error) {
	// QQ Bot access_token endpoint
	url := "https://bots.qq.com/app/getAppAccessToken"

	reqBody := map[string]string{
		"appId":     a.appID,
		"clientSecret": a.appSecret,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal token request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var tokenResp tokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}

		if tokenResp.Code != 0 {
			return "", fmt.Errorf("token request failed (code %d): %s", tokenResp.Code, tokenResp.Message)
		}

	if tokenResp.AccessToken == "" {
		return "", errors.New("empty access token received")
	}

	expiresSec, err := strconv.Atoi(tokenResp.ExpiresIn)
	if err != nil {
		return "", fmt.Errorf("parse expires_in: %w", err)
	}

	a.accessToken = tokenResp.AccessToken
	a.expiresAt = time.Now().Add(time.Duration(expiresSec) * time.Second)

	a.log.DebugContext(ctx, logger.CatApp, "qqbot access token refreshed",
		"expires_in_sec", expiresSec,
		"resp_body", string(respBody))

	return a.accessToken, nil
}

// ─── Message Sending ─────────────────────────────────────────────────────────

// SendC2CMessage sends a text message to a C2C (private chat) user.
func (a *APIClient) SendC2CMessage(ctx context.Context, openid, content string, msgID string, msgSeq int) error {
	return a.sendMessage(ctx, "/v2/users/"+openid+"/messages", content, 0, msgID, msgSeq)
}

// SendGroupMessage sends a text message to a group.
func (a *APIClient) SendGroupMessage(ctx context.Context, groupOpenid, content string, msgID string, msgSeq int) error {
	return a.sendMessage(ctx, "/v2/groups/"+groupOpenid+"/messages", content, 0, msgID, msgSeq)
}

// SendDirectMessage sends a text message to a guild direct message channel.
func (a *APIClient) SendDirectMessage(ctx context.Context, channelID, content string, msgID string, msgSeq int) error {
	return a.sendMessage(ctx, "/v2/dms/"+channelID+"/messages", content, 0, msgID, msgSeq)
}

func (a *APIClient) sendMessage(ctx context.Context, path, content string, msgType int, msgID string, msgSeq int) error {
	token, err := a.AccessToken(ctx)
	if err != nil {
		return fmt.Errorf("get access token: %w", err)
	}

	reqBody := MessageReq{
		Content: content,
		MsgType: msgType,
		MsgID:   msgID,
		MsgSeq:  msgSeq,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal message request: %w", err)
	}

	url := a.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create message request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "QQBot "+token)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("message request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		// Token might be expired, force refresh on next call
		a.mu.Lock()
		a.accessToken = ""
		a.mu.Unlock()
		return ErrAccessTokenExpired
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: status %d, body: %s", ErrAPICallFailed, resp.StatusCode, string(respBody))
	}

	return nil
}

// ReplyMessage sends a reply to a QQMessage based on its source.
// This is a convenience method that dispatches to the correct send method.
func (a *APIClient) ReplyMessage(ctx context.Context, msg QQMessage, content string) error {
	switch msg.Source {
	case SourceC2C:
		return a.SendC2CMessage(ctx, msg.TargetOpenID, content, msg.EventID, msg.Seq)
	case SourceGroup:
		return a.SendGroupMessage(ctx, msg.TargetOpenID, content, msg.EventID, msg.Seq)
	case SourceDirect:
		return a.SendDirectMessage(ctx, msg.ChatID, content, msg.EventID, msg.Seq)
	case SourcePublicGuild:
		return a.SendDirectMessage(ctx, msg.ChatID, content, msg.EventID, msg.Seq)
	default:
		return fmt.Errorf("unknown message source: %d", msg.Source)
	}
}
