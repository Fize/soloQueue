package tools

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/sandbox"
)

// ─── Provider interface ───────────────────────────────────────────────

type imageProvider interface {
	buildSubmitReq(m ImgModelCfg, req submitReq) (url string, body string, headers map[string]string, err error)
	buildQueryReq(m ImgModelCfg, jobID string) (url string, body string, headers map[string]string, err error)
	parseSubmitResp(body []byte) (jobID string, err error)
	parseQueryResp(body []byte) (status string, imageURLs, revisedPrompt []string, err error)
}

type submitReq struct {
	Prompt     string
	Resolution string
	Seed       *int64
	Revise     *int64
	Images     []string
}

var providers = map[string]imageProvider{
	"tencent": &tencentProvider{},
}

// ─── Constants ─────────────────────────────────────────────────────────

const (
	maxPolls     = 150              // 5 minutes / 2s
	pollInterval = 2 * time.Second //
	httpTimeout  = 30 * time.Second // per-request timeout
)

// ─── tencentProvider (TC3-HMAC-SHA256) ────────────────────────────────

type tencentProvider struct{}

const tencentAPIVersion = "2022-12-29"
const tencentService = "aiart"

func (p *tencentProvider) buildSubmitReq(m ImgModelCfg, req submitReq) (string, string, map[string]string, error) {
	payload := map[string]any{
		"Prompt":  req.Prompt,
		"LogoAdd": 0,
	}
	if req.Resolution != "" {
		payload["Resolution"] = req.Resolution
	}
	if req.Seed != nil {
		payload["Seed"] = *req.Seed
	}
	if req.Revise != nil {
		payload["Revise"] = *req.Revise
	}
	if len(req.Images) > 0 {
		payload["Images"] = req.Images
	}
	return p.buildRequest(m, "SubmitTextToImageJob", payload)
}

func (p *tencentProvider) buildQueryReq(m ImgModelCfg, jobID string) (string, string, map[string]string, error) {
	payload := map[string]any{
		"JobId": jobID,
	}
	return p.buildRequest(m, "QueryTextToImageJob", payload)
}

func (p *tencentProvider) buildRequest(m ImgModelCfg, action string, payload map[string]any) (string, string, map[string]string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", "", nil, fmt.Errorf("marshal payload: %w", err)
	}

	host := m.APIBaseHost
	timestamp := time.Now().Unix()
	secretId := m.SecretId
	if secretId == "" {
		secretId = os.Getenv(m.SecretIdEnv)
	}
	secretKey := m.SecretKey
	if secretKey == "" {
		secretKey = os.Getenv(m.SecretKeyEnv)
	}

	authorization, err := tc3Sign(secretId, secretKey, host, tencentService, string(body), timestamp)
	if err != nil {
		return "", "", nil, fmt.Errorf("tc3 sign: %w", err)
	}

	url := "https://" + host + "/"
	headers := map[string]string{
		"Host":           host,
		"X-TC-Action":    action,
		"X-TC-Version":   tencentAPIVersion,
		"X-TC-Timestamp": fmt.Sprintf("%d", timestamp),
		"X-TC-Region":    m.Region,
		"Authorization":  authorization,
		"Content-Type":   "application/json; charset=utf-8",
	}

	return url, string(body), headers, nil
}

func (tencentProvider) parseSubmitResp(body []byte) (string, error) {
	var resp struct {
		Response struct {
			JobId     string `json:"JobId"`
			RequestId string `json:"RequestId"`
			Error     *struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error,omitempty"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("unmarshal submit response: %w", err)
	}
	if resp.Response.Error != nil {
		return "", fmt.Errorf("API error [%s]: %s", resp.Response.Error.Code, resp.Response.Error.Message)
	}
	if resp.Response.JobId == "" {
		return "", fmt.Errorf("empty JobId in response")
	}
	return resp.Response.JobId, nil
}

func (tencentProvider) parseQueryResp(body []byte) (string, []string, []string, error) {
	var resp struct {
		Response struct {
			JobStatusCode  string   `json:"JobStatusCode"`
			JobStatusMsg   string   `json:"JobStatusMsg"`
			JobErrorCode   string   `json:"JobErrorCode"`
			JobErrorMsg    string   `json:"JobErrorMsg"`
			ResultImage    []string `json:"ResultImage"`
			ResultDetails  []string `json:"ResultDetails"`
			RevisedPrompt  []string `json:"RevisedPrompt"`
			Error          *struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error,omitempty"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", nil, nil, fmt.Errorf("unmarshal query response: %w", err)
	}
	if resp.Response.Error != nil {
		return "", nil, nil, fmt.Errorf("API error [%s]: %s", resp.Response.Error.Code, resp.Response.Error.Message)
	}
	return resp.Response.JobStatusCode, resp.Response.ResultImage, resp.Response.RevisedPrompt, nil
}

// ─── TC3-HMAC-SHA256 signing ──────────────────────────────────────────

func tc3Sign(secretId, secretKey, host, service, payload string, timestamp int64) (string, error) {
	algorithm := "TC3-HMAC-SHA256"
	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")

	canonicalHeaders := "content-type:application/json; charset=utf-8\nhost:" + host + "\n"
	signedHeaders := "content-type;host"
	hashedPayload := sha256Hex(payload)

	canonicalRequest := strings.Join([]string{
		"POST",
		"/",
		"",
		canonicalHeaders,
		signedHeaders,
		hashedPayload,
	}, "\n")

	credentialScope := date + "/" + service + "/" + "tc3_request"
	stringToSign := strings.Join([]string{
		algorithm,
		fmt.Sprintf("%d", timestamp),
		credentialScope,
		sha256Hex(canonicalRequest),
	}, "\n")

	signature, err := tc3HMACSHA256(secretKey, date, service, stringToSign)
	if err != nil {
		return "", err
	}

	authorization := fmt.Sprintf(
		"%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		algorithm, secretId, credentialScope, signedHeaders, signature,
	)
	return authorization, nil
}

func tc3HMACSHA256(secretKey, date, service, stringToSign string) (string, error) {
	mac := hmac.New(sha256.New, []byte("TC3"+secretKey))
	if _, err := mac.Write([]byte(date)); err != nil {
		return "", err
	}
	secretDate := mac.Sum(nil)

	mac = hmac.New(sha256.New, secretDate)
	if _, err := mac.Write([]byte(service)); err != nil {
		return "", err
	}
	secretService := mac.Sum(nil)

	mac = hmac.New(sha256.New, secretService)
	if _, err := mac.Write([]byte("tc3_request")); err != nil {
		return "", err
	}
	secretSigning := mac.Sum(nil)

	mac = hmac.New(sha256.New, secretSigning)
	if _, err := mac.Write([]byte(stringToSign)); err != nil {
		return "", err
	}
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// ─── Shared helpers ───────────────────────────────────────────────────

func findDefaultModel(models []ImgModelCfg) (*ImgModelCfg, error) {
	for i := range models {
		if models[i].IsDefault && models[i].Enabled {
			return &models[i], nil
		}
	}
	return nil, ErrImageGenNoDefaultModel
}

func checkCredentials(m *ImgModelCfg) error {
	switch m.Provider {
	case "tencent":
		sid := m.SecretId
		if sid == "" {
			sid = os.Getenv(m.SecretIdEnv)
		}
		sk := m.SecretKey
		if sk == "" {
			sk = os.Getenv(m.SecretKeyEnv)
		}
		if sid == "" || sk == "" {
			return fmt.Errorf("%w: %s / %s", ErrImageGenAuth, m.SecretIdEnv, m.SecretKeyEnv)
		}
	}
	return nil
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// ─── Image saving ─────────────────────────────────────────────────────

var imgDir string
var artifactDir string

func init() {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	imgDir = filepath.Join(home, ".soloqueue", "images")
	artifactDir = filepath.Join(home, ".soloqueue", "artifacts")
}

func saveImages(ctx context.Context, exec sandbox.Executor, urls []string, log *logger.Logger) []string {
	var paths []string
	for i, url := range urls {
		fname := urlBaseName(url, i+1)
		fpath := filepath.Join(imgDir, fname)

		if err := downloadTo(ctx, exec, url, fpath); err != nil {
			if log != nil {
				log.WarnContext(ctx, logger.CatTool, "image_gen: save failed",
					"url", url, "path", fpath, "err", err.Error())
			}
			continue
		}
		paths = append(paths, fpath)
		if log != nil {
			log.InfoContext(ctx, logger.CatTool, "image_gen: saved",
				"url", url, "path", fpath)
		}
	}
	return paths
}

func saveEditedImage(ctx context.Context, exec sandbox.Executor, url string, log *logger.Logger) []string {
	fname := urlBaseName(url, 1)
	fpath := filepath.Join(artifactDir, fname)

	if err := downloadTo(ctx, exec, url, fpath); err != nil {
		if log != nil {
			log.WarnContext(ctx, logger.CatTool, "image_edit: save failed",
				"url", url, "path", fpath, "err", err.Error())
		}
		return nil
	}
	if log != nil {
		log.InfoContext(ctx, logger.CatTool, "image_edit: saved",
			"url", url, "path", fpath)
	}
	return []string{fpath}
}

func urlBaseName(rawURL string, seq int) string {
	base := rawURL
	if idx := strings.Index(base, "?"); idx >= 0 {
		base = base[:idx]
	}
	name := filepath.Base(base)
	if name == "" || name == "." || name == "/" {
		return fmt.Sprintf("image_%d.jpg", seq)
	}
	ext := filepath.Ext(name)
	if ext == "" || len(ext) > 5 {
		ext = ".jpg"
		name = name + ext
		extLen := len(ext)
		nameNoExt := name[:len(name)-extLen]
		return fmt.Sprintf("%s_%d%s", nameNoExt, seq, ext)
	}
	nameNoExt := name[:len(name)-len(ext)]
	return fmt.Sprintf("%s_%d%s", nameNoExt, seq, ext)
}

func downloadTo(ctx context.Context, exec sandbox.Executor, url, fpath string) error {
	if err := os.MkdirAll(filepath.Dir(fpath), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	resp, err := exec.HTTPGet(ctx, url, sandbox.HTTPOptions{})
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download status %d", resp.StatusCode)
	}
	f, err := os.Create(fpath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(f, bytes.NewReader(resp.Body)); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

func doPost(ctx context.Context, exec sandbox.Executor, url string, body string, headers map[string]string) ([]byte, error) {
	resp, err := exec.HTTPPost(ctx, url, body, sandbox.HTTPOptions{Headers: headers, MaxBody: 64 << 10})
	if err != nil {
		return nil, fmt.Errorf("http post %s: %w", url, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http post %s: status %d, body: %s", url, resp.StatusCode, string(resp.Body))
	}
	return resp.Body, nil
}
