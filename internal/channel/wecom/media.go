package wecom

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/xiaobaitu/soloqueue/internal/channel"
)

const maxMediaBytes = 10 << 20

type mediaImage struct {
	URL    string `json:"url"`
	AESKey string `json:"aeskey"`
}
type mixedItem struct {
	Type string `json:"msgtype"`
	Text struct {
		Content string `json:"content"`
	} `json:"text"`
	Image mediaImage `json:"image"`
}

// Download targets are remote platform URLs, never local network resources.
func mediaHTTPClient() *http.Client {
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, errors.New("media host has no addresses")
		}
		for _, ip := range ips {
			if !ip.IP.IsGlobalUnicast() || ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() {
				return nil, errors.New("media address denied")
			}
		}
		return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || req.URL.Scheme != "https" {
			return errors.New("media redirect denied")
		}
		return nil
	}}
}
func downloadMedia(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return nil, errors.New("invalid media URL")
	}
	client := mediaHTTPClient()
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, errors.New("invalid media request")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("media download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("media download HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMediaBytes+65))
	if err != nil {
		return nil, errors.New("media read failed")
	}
	if len(data) > maxMediaBytes+64 {
		return nil, errors.New("media too large")
	}
	return data, nil
}
func decryptImage(data []byte, encodedKey string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil || len(key) != 32 {
		return nil, errors.New("invalid media key")
	}
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, errors.New("invalid ciphertext")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, key[:aes.BlockSize]).CryptBlocks(plain, data)
	n := int(plain[len(plain)-1])
	if n < 1 || n > 32 || n > len(plain) {
		return nil, errors.New("invalid padding")
	}
	for _, b := range plain[len(plain)-n:] {
		if int(b) != n {
			return nil, errors.New("invalid padding")
		}
	}
	return plain[:len(plain)-n], nil
}
func (c *Client) normalize(ctx context.Context, msg channel.Message, in incoming) (channel.Message, error) {
	add := func(image mediaImage) error {
		if len(msg.Attachments) >= 10 {
			return errors.New("too many images")
		}
		downloadCtx := ctx
		if scoped, ok := ctx.Value(downloadContextKey{}).(context.Context); ok {
			downloadCtx = scoped
		}
		data, err := downloadMedia(downloadCtx, image.URL)
		if err != nil {
			return err
		}
		data, err = decryptImage(data, image.AESKey)
		if err != nil {
			return err
		}
		mime := http.DetectContentType(data)
		if !strings.HasPrefix(mime, "image/") || len(data) > maxMediaBytes {
			return errors.New("invalid image")
		}
		ext := ".jpg"
		if mime == "image/png" {
			ext = ".png"
		}
		msg.Attachments = append(msg.Attachments, channel.Attachment{Kind: channel.AttachmentImage, Data: data, MIMEType: mime, Name: fmt.Sprintf("image-%d%s", len(msg.Attachments)+1, ext)})
		return nil
	}
	switch in.MsgType {
	case "voice":
		msg.Text = strings.TrimSpace(in.Voice.Content)
		if msg.Text == "" {
			return msg, errors.New("empty voice transcript")
		}
		msg.Attachments = []channel.Attachment{{Kind: channel.AttachmentAudio, Transcript: msg.Text}}
	case "image":
		if err := add(in.Image); err != nil {
			return msg, err
		}
	case "mixed":
		var parts []string
		for _, item := range in.Mixed.Items {
			switch item.Type {
			case "text":
				parts = append(parts, item.Text.Content)
			case "image":
				if err := add(item.Image); err != nil {
					return msg, err
				}
				parts = append(parts, fmt.Sprintf("[Image %d]", len(msg.Attachments)))
			default:
				return msg, errors.New("unsupported mixed item")
			}
		}
		msg.Text = strings.Join(parts, "\n")
	}
	if strings.TrimSpace(msg.Text) == "" && len(msg.Attachments) == 0 {
		return msg, errors.New("empty message")
	}
	return msg, nil
}

// SendMedia delivers to the immutable current conversation, independently of text streams.
func (c *Client) SendMedia(ctx context.Context, msg channel.Message, items []channel.OutboundMedia) error {
	if msg.ConversationID == "" || msg.AccountID != c.cfg.AccountID || msg.AccountIdentity != c.cfg.BotID {
		return errors.New("wecom media route mismatch")
	}
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	for i, item := range items {
		if item.Kind != channel.MediaImage && item.Kind != channel.MediaVoice {
			return fmt.Errorf("media item %d: unsupported kind", i+1)
		}
		var data []byte
		var err error
		if item.Path != "" {
			var f *os.File
			f, err = os.Open(item.Path)
			if err == nil {
				data, err = io.ReadAll(io.LimitReader(f, maxMediaBytes+1))
				f.Close()
			}
		} else {
			data, err = downloadMedia(ctx, item.URL)
		}
		if err != nil {
			return fmt.Errorf("media item %d: unable to load", i+1)
		}
		if len(data) == 0 || len(data) > maxMediaBytes {
			return errors.New("media size unsupported")
		}
		kind := string(item.Kind)
		if kind == "image" && !strings.HasPrefix(http.DetectContentType(data), "image/") {
			return errors.New("invalid outbound image")
		}
		if kind == "voice" && !strings.HasPrefix(string(data), "#!AMR\n") {
			return errors.New("voice requires AMR audio; automatic conversion is not available")
		}
		name := filepath.Base(item.FileName)
		if name == "." || name == "" {
			name = filepath.Base(item.Path)
		}
		if name == "." || name == "" {
			name = "media"
		}
		call := func(cmd string, body any, out any) error {
			frame, e := c.requestFrame(ctx, conn, cmd, cmd+"_"+uuid.NewString(), body)
			if e != nil {
				return e
			}
			if out != nil {
				return json.Unmarshal(frame.Body, out)
			}
			return nil
		}
		sum := md5.Sum(data)
		chunks := (len(data) + (512 << 10) - 1) / (512 << 10)
		var init struct {
			ID string `json:"upload_id"`
		}
		if err = call("aibot_upload_media_init", map[string]any{"type": kind, "filename": name, "total_size": len(data), "total_chunks": chunks, "md5": hex.EncodeToString(sum[:])}, &init); err != nil {
			return err
		}
		if init.ID == "" {
			return errors.New("upload id missing")
		}
		for n := 0; n < chunks; n++ {
			end := (n + 1) * (512 << 10)
			if end > len(data) {
				end = len(data)
			}
			if err = call("aibot_upload_media_chunk", map[string]any{"upload_id": init.ID, "chunk_index": n, "base64_data": base64.StdEncoding.EncodeToString(data[n*(512<<10) : end])}, nil); err != nil {
				return err
			}
		}
		var finish struct {
			ID string `json:"media_id"`
		}
		if err = call("aibot_upload_media_finish", map[string]string{"upload_id": init.ID}, &finish); err != nil {
			return err
		}
		if finish.ID == "" {
			return errors.New("media id missing")
		}
		if err = call("aibot_send_msg", map[string]any{"chatid": msg.ConversationID, "msgtype": kind, kind: map[string]string{"media_id": finish.ID}}, nil); err != nil {
			return fmt.Errorf("media item %d (previous items sent): %w", i+1, err)
		}
	}
	return nil
}
