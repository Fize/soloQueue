package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

type Client struct {
	cfg  Config
	http *http.Client
	log  *logger.Logger
}

func NewClient(cfg Config, log *logger.Logger) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}, log: log}
}

func NewClientWithHTTP(cfg Config, httpClient *http.Client, log *logger.Logger) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{cfg: cfg, http: httpClient, log: log}
}

func (c *Client) call(ctx context.Context, method string, in any, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.cfg.EffectiveAPIURL(), "/")+"/bot"+c.cfg.Token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("telegram %s: http %s: %s", method, resp.Status, strings.TrimSpace(string(b)))
	}
	var envelope apiResponse[json.RawMessage]
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if !envelope.OK {
		return fmt.Errorf("telegram %s: %s", method, envelope.Description)
	}
	if out != nil && len(envelope.Result) > 0 {
		return json.Unmarshal(envelope.Result, out)
	}
	return nil
}

func (c *Client) GetMe(ctx context.Context) (User, error) {
	var u User
	err := c.call(ctx, "getMe", struct{}{}, &u)
	return u, err
}
func (c *Client) GetUpdates(ctx context.Context, offset int64) ([]Update, error) {
	var u []Update
	err := c.call(ctx, "getUpdates", getUpdatesRequest{Offset: offset, Limit: 100, Timeout: 30, AllowedUpdates: []string{"message"}}, &u)
	return u, err
}
func (c *Client) SendText(ctx context.Context, msg channel.Message, text string) error {
	if msg.ConversationID == "" {
		return fmt.Errorf("telegram reply requires conversation id")
	}
	chatID, err := strconv.ParseInt(msg.ConversationID, 10, 64)
	if err != nil {
		return err
	}
	formatted := FormatText(text)
	parts := SplitText(formatted, 4096)
	parseMode := "HTML"
	if len(parts) > 1 {
		// Splitting arbitrary HTML can cut through a tag. Preserve delivery by
		// falling back to plain text for oversized formatted replies.
		parts = SplitText(text, 4096)
		parseMode = ""
	}
	for _, part := range parts {
		if err := c.call(ctx, "sendMessage", sendMessageRequest{ChatID: chatID, Text: part, ParseMode: parseMode}, nil); err != nil {
			return err
		}
	}
	return nil
}
func (c *Client) StartResponseActivity(ctx context.Context, msg channel.Message) (func(), error) {
	chatID, err := strconv.ParseInt(msg.ConversationID, 10, 64)
	if err != nil {
		return func() {}, err
	}
	activityCtx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-activityCtx.Done():
				return
			case <-ticker.C:
				_ = c.call(activityCtx, "sendChatAction", chatActionRequest{ChatID: chatID, Action: "typing"}, nil)
			}
		}
	}()
	_ = c.call(ctx, "sendChatAction", chatActionRequest{ChatID: chatID, Action: "typing"}, nil)
	return cancel, nil
}
func (c *Client) SendMedia(ctx context.Context, msg channel.Message, media []channel.OutboundMedia) error {
	chatID, err := strconv.ParseInt(msg.ConversationID, 10, 64)
	if err != nil {
		return err
	}
	for _, item := range media {
		if item.Path == "" {
			continue
		}
		method := "sendDocument"
		field := "document"
		switch item.Kind {
		case channel.MediaImage:
			method, field = "sendPhoto", "photo"
		case channel.MediaVideo:
			method, field = "sendVideo", "video"
		case channel.MediaVoice:
			method, field = "sendVoice", "voice"
		}
		if err := c.upload(ctx, method, field, chatID, item.Path, item.FileName); err != nil {
			return err
		}
	}
	return nil
}
func (c *Client) upload(ctx context.Context, method, field string, chatID int64, path, name string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("chat_id", strconv.FormatInt(chatID, 10)); err != nil {
		return err
	}
	if name == "" {
		name = filepath.Base(path)
	}
	part, err := mw.CreateFormFile(field, name)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, file); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.cfg.EffectiveAPIURL(), "/")+"/bot"+c.cfg.Token+"/"+method, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("telegram %s: http %s", method, resp.Status)
	}
	var result apiResponse[json.RawMessage]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if !result.OK {
		return fmt.Errorf("telegram %s: %s", method, result.Description)
	}
	return nil
}
func (c *Client) Download(ctx context.Context, fileID string) ([]byte, string, error) {
	var f File
	if err := c.call(ctx, "getFile", map[string]string{"file_id": fileID}, &f); err != nil {
		return nil, "", err
	}
	u := strings.TrimRight(c.cfg.EffectiveAPIURL(), "/") + "/file/bot" + c.cfg.Token + "/" + f.FilePath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, "", fmt.Errorf("telegram file download: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	return data, f.FilePath, err
}
