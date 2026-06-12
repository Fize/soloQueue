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

// ─── Message type constants ──────────────────────────────────────────────────

const (
	MsgTypeText     = 0 // text message
	MsgTypeMarkdown = 2 // markdown message
	MsgTypeArk      = 3 // ark (template card) message
	MsgTypeEmbed    = 4 // embed message
	MsgTypeMedia    = 7 // rich media message (image/video/voice/file)
)

// File type constants for rich media uploads.
const (
	FileTypeImage = 1
	FileTypeVideo = 2
	FileTypeVoice = 3
	FileTypeFile  = 4
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
// Only the field matching MsgType should be populated; omitempty ensures
// the correct JSON shape per message type.
type MessageReq struct {
	Content  string           `json:"content,omitempty"`
	Markdown *MarkdownContent `json:"markdown,omitempty"`
	Ark      *ArkContent      `json:"ark,omitempty"`
	Embed    *EmbedContent    `json:"embed,omitempty"`
	Media    *MediaContent    `json:"media,omitempty"`
	MsgType  int              `json:"msg_type"`
	MsgID    string           `json:"msg_id,omitempty"`
	MsgSeq   int              `json:"msg_seq,omitempty"`
}

// MarkdownContent wraps a markdown payload.
type MarkdownContent struct {
	Content string `json:"content"`
}

// MediaContent references uploaded file_info for rich media messages.
type MediaContent struct {
	FileInfo string `json:"file_info"`
}

// ArkContent represents an Ark template card message.
type ArkContent struct {
	TemplateID int           `json:"template_id"`
	KV         []ArkKeyValue `json:"kv"`
}

// ArkKeyValue is a single key-value pair in an Ark template.
type ArkKeyValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// EmbedContent represents the embed message payload.
type EmbedContent struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
}

// MessageResp is the response from sending a message.
type MessageResp struct {
	ID string `json:"id"`
}

// FileInfo is returned by the file upload endpoint.
type FileInfo struct {
	FileUUID string `json:"file_uuid"`
	FileInfo string `json:"file_info"`
	TTL      int    `json:"ttl"`
	ID       string `json:"id,omitempty"`
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

// buildMessageReq constructs a MessageReq with the correct payload shape for msgType.
func buildMessageReq(msgType int, content string, msgID string, msgSeq int) MessageReq {
	req := MessageReq{
		MsgType: msgType,
		MsgID:   msgID,
		MsgSeq:  msgSeq,
	}
	switch msgType {
	case MsgTypeMarkdown:
		req.Markdown = &MarkdownContent{Content: content}
	case MsgTypeMedia:
		req.Media = &MediaContent{FileInfo: content}
	case MsgTypeArk:
		// content is ignored for ark; caller should construct ArkContent separately
		req.Ark = &ArkContent{}
	default: // MsgTypeText and others
		req.Content = content
	}
	return req
}

// SendC2CMessage sends a message to a C2C (private chat) user.
// msgType should be one of MsgTypeText, MsgTypeMarkdown, etc.
func (a *APIClient) SendC2CMessage(ctx context.Context, openid string, msgType int, content string, msgID string, msgSeq int) error {
	req := buildMessageReq(msgType, content, msgID, msgSeq)
	return a.sendMessage(ctx, "/v2/users/"+openid+"/messages", req)
}

// SendGroupMessage sends a message to a group.
func (a *APIClient) SendGroupMessage(ctx context.Context, groupOpenid string, msgType int, content string, msgID string, msgSeq int) error {
	req := buildMessageReq(msgType, content, msgID, msgSeq)
	return a.sendMessage(ctx, "/v2/groups/"+groupOpenid+"/messages", req)
}

// SendDirectMessage sends a message to a guild direct message channel.
func (a *APIClient) SendDirectMessage(ctx context.Context, channelID string, msgType int, content string, msgID string, msgSeq int) error {
	req := buildMessageReq(msgType, content, msgID, msgSeq)
	return a.sendMessage(ctx, "/v2/dms/"+channelID+"/messages", req)
}

func (a *APIClient) sendMessage(ctx context.Context, path string, req MessageReq) error {
	token, err := a.AccessToken(ctx)
	if err != nil {
		return fmt.Errorf("get access token: %w", err)
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal message request: %w", err)
	}

	url := a.baseURL + path
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create message request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "QQBot "+token)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("message request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
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
func (a *APIClient) ReplyMessage(ctx context.Context, msg QQMessage, msgType int, content string) error {
	switch msg.Source {
	case SourceC2C:
		return a.SendC2CMessage(ctx, msg.TargetOpenID, msgType, content, msg.EventID, msg.Seq)
	case SourceGroup:
		return a.SendGroupMessage(ctx, msg.TargetOpenID, msgType, content, msg.EventID, msg.Seq)
	case SourceDirect:
		return a.SendDirectMessage(ctx, msg.ChatID, msgType, content, msg.EventID, msg.Seq)
	case SourcePublicGuild:
		return a.SendDirectMessage(ctx, msg.ChatID, msgType, content, msg.EventID, msg.Seq)
	default:
		return fmt.Errorf("unknown message source: %d", msg.Source)
	}
}

// ─── Rich Media Upload ───────────────────────────────────────────────────────

// UploadFile uploads a file for rich media messaging.
// targetType must be "user" or "group". targetID is the openid or group_openid.
// fileType is one of FileTypeImage/Video/Voice/File.
// url must be a publicly accessible URL to the file content.
func (a *APIClient) UploadFile(ctx context.Context, targetType, targetID string, fileType int, url string) (*FileInfo, error) {
	// targetType only supports "user" and "group"
	if targetType != "user" && targetType != "group" {
		return nil, fmt.Errorf("invalid targetType: %s (must be user or group)", targetType)
	}

	token, err := a.AccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}

	reqBody := map[string]any{
		"file_type": fileType,
		"url":       url,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal upload request: %w", err)
	}

	path := fmt.Sprintf("/v2/%ss/%s/files", targetType, targetID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create upload request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "QQBot "+token)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("upload request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		a.mu.Lock()
		a.accessToken = ""
		a.mu.Unlock()
		return nil, ErrAccessTokenExpired
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: status %d, body: %s", ErrAPICallFailed, resp.StatusCode, string(respBody))
	}

	var fi FileInfo
	if err := json.NewDecoder(resp.Body).Decode(&fi); err != nil {
		return nil, fmt.Errorf("decode upload response: %w", err)
	}
	return &fi, nil
}
