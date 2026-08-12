package wechat

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/channel"
)

const maxWechatMediaBytes = 5 << 20

type uploadedMedia struct {
	downloadParam string
	aesKeyHex     string
	plainSize     int64
	cipherSize    int64
}

// SendMedia uploads and sends each item using the route captured from the
// inbound message. It never consults mutable client or bridge route state.
func (c *Client) SendMedia(ctx context.Context, msg channel.Message, media []channel.OutboundMedia) error {
	if msg.UserID == "" || msg.ReplyToken == "" {
		return fmt.Errorf("wechat_media_route_missing: user id and context token are required")
	}
	if msg.AccountID != "" && msg.AccountID != c.cfg.BotID {
		return fmt.Errorf("wechat_media_account_mismatch: route account does not match client")
	}
	for i, item := range media {
		data, name, err := c.loadOutboundMedia(ctx, item)
		if err != nil {
			return fmt.Errorf("wechat_media_load_failed: item %d: %w", i+1, err)
		}
		kind := item.Kind
		if kind == channel.MediaVoice && !isSILK(data) {
			kind = channel.MediaFile
		}
		uploaded, err := c.uploadMedia(ctx, msg.UserID, kind, data)
		if err != nil {
			return fmt.Errorf("wechat_media_upload_failed: item %d: %w", i+1, err)
		}
		if err := c.sendUploadedMedia(ctx, msg, kind, name, uploaded); err != nil {
			return fmt.Errorf("wechat_media_send_failed: item %d: %w", i+1, err)
		}
	}
	return nil
}

func (c *Client) loadOutboundMedia(ctx context.Context, media channel.OutboundMedia) ([]byte, string, error) {
	name := filepath.Base(media.FileName)
	if media.Path != "" {
		f, err := os.Open(media.Path)
		if err != nil {
			return nil, "", err
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, maxWechatMediaBytes+1))
		if err != nil {
			return nil, "", err
		}
		if len(data) > maxWechatMediaBytes {
			return nil, "", fmt.Errorf("media exceeds %d bytes", maxWechatMediaBytes)
		}
		if name == "." || name == "" {
			name = filepath.Base(media.Path)
		}
		return data, name, nil
	}
	parsed, err := url.Parse(media.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, "", fmt.Errorf("invalid public media URL")
	}
	if parsed.User != nil {
		return nil, "", fmt.Errorf("media URL credentials are not allowed")
	}
	if err := validatePublicMediaHost(ctx, parsed.Hostname()); err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", err
	}
	mediaHTTP := *c.http
	existingRedirectCheck := mediaHTTP.CheckRedirect
	mediaHTTP.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if existingRedirectCheck != nil {
			if err := existingRedirectCheck(req, via); err != nil {
				return err
			}
		} else if len(via) >= 10 {
			return fmt.Errorf("too many media redirects")
		}
		if req.URL.User != nil {
			return fmt.Errorf("media redirect credentials are not allowed")
		}
		return validatePublicMediaHost(req.Context(), req.URL.Hostname())
	}
	resp, err := mediaHTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("remote media returned %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxWechatMediaBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxWechatMediaBytes {
		return nil, "", fmt.Errorf("media exceeds %d bytes", maxWechatMediaBytes)
	}
	if name == "." || name == "" {
		name = filepath.Base(parsed.Path)
	}
	if name == "." || name == "" || name == "/" {
		name = "attachment.bin"
	}
	return data, name, nil
}

func (c *Client) uploadMedia(ctx context.Context, userID string, kind channel.MediaKind, data []byte) (uploadedMedia, error) {
	mediaType := 3
	switch kind {
	case channel.MediaImage:
		mediaType = 1
	case channel.MediaVideo:
		mediaType = 2
	case channel.MediaVoice:
		mediaType = 4
	}
	key := make([]byte, aes.BlockSize)
	fileKey := make([]byte, aes.BlockSize)
	if _, err := rand.Read(key); err != nil {
		return uploadedMedia{}, err
	}
	if _, err := rand.Read(fileKey); err != nil {
		return uploadedMedia{}, err
	}
	ciphertext, err := encryptAESECB(data, key)
	if err != nil {
		return uploadedMedia{}, err
	}
	sum := md5.Sum(data)
	keyHex := hex.EncodeToString(key)
	fileKeyHex := hex.EncodeToString(fileKey)
	var resp GetUploadURLResponse
	if err := c.post(ctx, c.cfg.EffectiveBaseURL(), "ilink/bot/getuploadurl", map[string]any{
		"filekey": fileKeyHex, "media_type": mediaType, "to_user_id": userID,
		"rawsize": len(data), "rawfilemd5": hex.EncodeToString(sum[:]), "filesize": len(ciphertext),
		"no_need_thumb": true, "aeskey": keyHex, "base_info": c.baseInfo(),
	}, c.cfg.Token, &resp); err != nil {
		return uploadedMedia{}, err
	}
	if err := responseError("getuploadurl", resp.APIResponse); err != nil {
		return uploadedMedia{}, err
	}
	uploadURL := strings.TrimSpace(resp.UploadFullURL)
	if uploadURL == "" && resp.UploadParam != "" {
		uploadURL = strings.TrimRight(c.cfg.EffectiveCDNBaseURL(), "/") + "/upload?encrypted_query_param=" + url.QueryEscape(resp.UploadParam) + "&filekey=" + url.QueryEscape(fileKeyHex)
	}
	if uploadURL == "" {
		return uploadedMedia{}, fmt.Errorf("getuploadurl returned no upload target")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(ciphertext))
	if err != nil {
		return uploadedMedia{}, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	uploadResp, err := c.http.Do(req)
	if err != nil {
		return uploadedMedia{}, err
	}
	defer uploadResp.Body.Close()
	if uploadResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(uploadResp.Body, 4096))
		return uploadedMedia{}, fmt.Errorf("CDN upload %s: %s", uploadResp.Status, strings.TrimSpace(string(body)))
	}
	downloadParam := uploadResp.Header.Get("x-encrypted-param")
	if downloadParam == "" {
		return uploadedMedia{}, fmt.Errorf("CDN response missing x-encrypted-param")
	}
	return uploadedMedia{downloadParam: downloadParam, aesKeyHex: keyHex, plainSize: int64(len(data)), cipherSize: int64(len(ciphertext))}, nil
}

func (c *Client) sendUploadedMedia(ctx context.Context, msg channel.Message, kind channel.MediaKind, name string, uploaded uploadedMedia) error {
	mediaRef := &CDNMedia{EncryptQueryParam: uploaded.downloadParam, AESKey: base64.StdEncoding.EncodeToString([]byte(uploaded.aesKeyHex)), EncryptType: 1}
	item := MessageItem{Type: 4, FileItem: &FileItem{Media: mediaRef, FileName: name, Len: strconv.FormatInt(uploaded.plainSize, 10)}}
	switch kind {
	case channel.MediaImage:
		item = MessageItem{Type: 2, ImageItem: &ImageItem{Media: mediaRef, MidSize: uploaded.cipherSize}}
	case channel.MediaVideo:
		item = MessageItem{Type: 5, VideoItem: &VideoItem{Media: mediaRef, VideoSize: uploaded.cipherSize}}
	case channel.MediaVoice:
		item = MessageItem{Type: 3, VoiceItem: &VoiceItem{Media: mediaRef}}
	}
	ctx, cancel := context.WithTimeout(ctx, defaultAPITimeout)
	defer cancel()
	var resp APIResponse
	err := c.post(ctx, c.cfg.EffectiveBaseURL(), "ilink/bot/sendmessage", map[string]any{
		"msg":       Message{ToUserID: msg.UserID, ClientID: newClientID(), MessageType: 2, MessageState: 2, ContextToken: msg.ReplyToken, ItemList: []MessageItem{item}},
		"base_info": c.baseInfo(),
	}, c.cfg.Token, &resp)
	if err != nil {
		return err
	}
	return responseError("sendmessage", resp)
}

func (c *Client) DownloadMedia(ctx context.Context, media CDNMedia) ([]byte, error) {
	if media.EncryptQueryParam == "" || media.AESKey == "" {
		return nil, fmt.Errorf("wechat_media_reference_invalid: missing encrypted query or AES key")
	}
	key, err := decodeMediaKey(media.AESKey)
	if err != nil {
		return nil, fmt.Errorf("wechat_media_key_invalid: %w", err)
	}
	downloadURL := strings.TrimRight(c.cfg.EffectiveCDNBaseURL(), "/") + "/download?encrypted_query_param=" + url.QueryEscape(media.EncryptQueryParam)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wechat_media_download_failed: %s", resp.Status)
	}
	ciphertext, err := io.ReadAll(io.LimitReader(resp.Body, maxWechatMediaBytes+aes.BlockSize+1))
	if err != nil {
		return nil, err
	}
	if len(ciphertext) > maxWechatMediaBytes+aes.BlockSize {
		return nil, fmt.Errorf("wechat_media_too_large: encrypted payload exceeds limit")
	}
	return decryptAESECB(ciphertext, key)
}

func encryptAESECB(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	padding := aes.BlockSize - len(plaintext)%aes.BlockSize
	padded := append(append([]byte(nil), plaintext...), bytes.Repeat([]byte{byte(padding)}, padding)...)
	out := make([]byte, len(padded))
	for offset := 0; offset < len(padded); offset += aes.BlockSize {
		block.Encrypt(out[offset:offset+aes.BlockSize], padded[offset:offset+aes.BlockSize])
	}
	return out, nil
}

func decryptAESECB(ciphertext, key []byte) ([]byte, error) {
	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("invalid AES-ECB ciphertext size")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(ciphertext))
	for offset := 0; offset < len(ciphertext); offset += aes.BlockSize {
		block.Decrypt(out[offset:offset+aes.BlockSize], ciphertext[offset:offset+aes.BlockSize])
	}
	padding := int(out[len(out)-1])
	if padding == 0 || padding > aes.BlockSize || padding > len(out) {
		return nil, fmt.Errorf("invalid PKCS7 padding")
	}
	for _, value := range out[len(out)-padding:] {
		if int(value) != padding {
			return nil, fmt.Errorf("invalid PKCS7 padding")
		}
	}
	return out[:len(out)-padding], nil
}

func decodeMediaKey(encoded string) ([]byte, error) {
	if len(encoded) == aes.BlockSize*2 {
		if key, err := hex.DecodeString(encoded); err == nil {
			return key, nil
		}
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		decoded = []byte(encoded)
	}
	if len(decoded) == aes.BlockSize {
		return decoded, nil
	}
	if len(decoded) == aes.BlockSize*2 {
		key := make([]byte, aes.BlockSize)
		if _, err := hex.Decode(key, decoded); err == nil {
			return key, nil
		}
	}
	return nil, fmt.Errorf("unsupported key encoding")
}

func validatePublicMediaHost(ctx context.Context, host string) error {
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateMediaIP(ip) {
			return fmt.Errorf("media URL resolves to a private address")
		}
		return nil
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("media URL DNS lookup failed: %w", err)
	}
	for _, address := range addresses {
		if isPrivateMediaIP(address.IP) {
			return fmt.Errorf("media URL resolves to a private address")
		}
	}
	return nil
}

func isPrivateMediaIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

func isSILK(data []byte) bool {
	return bytes.HasPrefix(data, []byte("#!SILK_V3")) || bytes.HasPrefix(data, []byte("\x02#!SILK_V3"))
}
