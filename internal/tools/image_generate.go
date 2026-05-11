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
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── Tool ──────────────────────────────────────────────────────────────

type imageGenTool struct {
	cfg    Config
	logger *logger.Logger
}

func newImageGenTool(cfg Config) *imageGenTool {
	return &imageGenTool{cfg: cfg, logger: cfg.Logger}
}

func (imageGenTool) Name() string { return "ImageGenerate" }

func (imageGenTool) Description() string {
	return "Generate images from text descriptions using configured AI image models " +
		"(Tencent Hunyuan, Doubao Seedream, etc.). Returns temporary image URLs (valid ~1 hour)."
}

func (imageGenTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "prompt":{"type":"string","description":"Text description for the image (Chinese recommended, max 8192 chars)"},
    "resolution":{"type":"string","description":"Output resolution W:H, e.g. 1024:1024"},
    "seed":{"type":"integer","description":"Random seed (1-4294967295), omit for random"},
    "revise":{"type":"integer","description":"1=enable prompt revision (recommended), 0=off"},
    "images":{"type":"array","items":{"type":"string"},"description":"Reference images (max 3), base64 or URL"}
  },
  "required":["prompt"]
}`)
}

// ─── Args / Result ─────────────────────────────────────────────────────

type imageGenArgs struct {
	Prompt     string   `json:"prompt"`
	Resolution string   `json:"resolution,omitempty"`
	Seed       *int64   `json:"seed,omitempty"`
	Revise     *int64   `json:"revise,omitempty"`
	Images     []string `json:"images,omitempty"`
}

type imageGenResult struct {
	Model         string   `json:"model"`
	Status        string   `json:"status"`
	ImageURLs     []string `json:"image_urls,omitempty"`
	LocalPaths    []string `json:"local_paths,omitempty"`
	RevisedPrompt []string `json:"revised_prompt,omitempty"`
	ErrorCode     string   `json:"error_code,omitempty"`
	ErrorMsg      string   `json:"error_msg,omitempty"`
}

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
	"doubao":  &doubaoProvider{},
}

// ─── Execute ──────────────────────────────────────────────────────────

const (
	maxPolls      = 150               // 5 minutes / 2s
	pollInterval  = 2 * time.Second   //
	httpTimeout   = 30 * time.Second  // per-request timeout
)

func (t *imageGenTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	var a imageGenArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("prompt", a.Prompt); err != nil {
		return "", err
	}

	model, err := findDefaultModel(t.cfg.ImageModels)
	if err != nil {
		return "", err
	}

	prov, ok := providers[model.Provider]
	if !ok {
		return "", fmt.Errorf("%w: unknown provider %q", ErrInvalidArgs, model.Provider)
	}

	if err := checkCredentials(model); err != nil {
		return "", err
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "image_gen: submitting",
			"model", model.ID,
			"prompt_len", len(a.Prompt))
	}

	sr := submitReq{
		Prompt:     a.Prompt,
		Resolution: a.Resolution,
		Seed:       a.Seed,
		Revise:     a.Revise,
		Images:     a.Images,
	}
	url, body, headers, err := prov.buildSubmitReq(*model, sr)
	if err != nil {
		return "", fmt.Errorf("build submit request: %w", err)
	}

	respBody, err := doPost(ctx, url, body, headers)
	if err != nil {
		return "", fmt.Errorf("submit request: %w", err)
	}

	jobID, err := prov.parseSubmitResp(respBody)
	if err != nil {
		return "", fmt.Errorf("parse submit response: %w", err)
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "image_gen: submitted",
			"model", model.ID,
			"job_id", jobID)
	}

	for i := 0; i < maxPolls; i++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(pollInterval):
		}

		url, body, headers, err := prov.buildQueryReq(*model, jobID)
		if err != nil {
			return "", fmt.Errorf("build query request: %w", err)
		}

		respBody, err := doPost(ctx, url, body, headers)
		if err != nil {
			if t.logger != nil {
				t.logger.WarnContext(ctx, logger.CatTool, "image_gen: query failed",
					"model", model.ID,
					"job_id", jobID,
					"err", err)
			}
			continue
		}

		status, urls, revised, err := prov.parseQueryResp(respBody)
		if err != nil {
			if t.logger != nil {
				t.logger.WarnContext(ctx, logger.CatTool, "image_gen: parse query failed",
					"model", model.ID,
					"job_id", jobID,
					"err", err)
			}
			continue
		}

		switch status {
		case "5":
			if t.logger != nil {
				t.logger.InfoContext(ctx, logger.CatTool, "image_gen: completed",
					"model", model.ID,
					"job_id", jobID,
					"num_urls", len(urls))
			}
			localPaths := saveImages(ctx, urls, t.logger)
			r := imageGenResult{
				Model:         model.ID,
				Status:        "completed",
				ImageURLs:     urls,
				LocalPaths:    localPaths,
				RevisedPrompt: revised,
			}
			b, _ := json.Marshal(r)
			return string(b), nil
		case "4":
			if t.logger != nil {
				t.logger.ErrorContext(ctx, logger.CatTool, "image_gen: failed",
					"model", model.ID,
					"job_id", jobID,
					"status", status)
			}
			r := imageGenResult{
				Model:     model.ID,
				Status:    "failed",
				ErrorCode: "JOB_FAILED",
				ErrorMsg:  status,
			}
			b, _ := json.Marshal(r)
			return string(b), fmt.Errorf("%w: job %s", ErrImageGenFailed, jobID)
		}
	}

	return "", ErrImageGenTimeout
}

// ─── Helpers ──────────────────────────────────────────────────────────

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
		if os.Getenv(m.SecretIdEnv) == "" || os.Getenv(m.SecretKeyEnv) == "" {
			return fmt.Errorf("%w: %s / %s", ErrImageGenAuth, m.SecretIdEnv, m.SecretKeyEnv)
		}
	case "doubao":
		if os.Getenv(m.APIKeyEnv) == "" {
			return fmt.Errorf("%w: %s", ErrImageGenAuth, m.APIKeyEnv)
		}
	}
	return nil
}

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
	secretId := os.Getenv(m.SecretIdEnv)
	secretKey := os.Getenv(m.SecretKeyEnv)

	authorization, err := tc3Sign(secretId, secretKey, host, tencentService, string(body), timestamp)
	if err != nil {
		return "", "", nil, fmt.Errorf("tc3 sign: %w", err)
	}

	url := "https://" + host + "/"
	headers := map[string]string{
		"Host":               host,
		"X-TC-Action":        action,
		"X-TC-Version":       tencentAPIVersion,
		"X-TC-Timestamp":     fmt.Sprintf("%d", timestamp),
		"X-TC-Region":        m.Region,
		"Authorization":      authorization,
		"Content-Type":       "application/json; charset=utf-8",
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

// ─── Image saving ─────────────────────────────────────────────────────

var imgDir string

func init() {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	imgDir = filepath.Join(home, ".soloqueue", "images")
}

func saveImages(ctx context.Context, urls []string, log *logger.Logger) []string {
	var paths []string
	for i, url := range urls {
		fname := urlBaseName(url, i+1)
		fpath := filepath.Join(imgDir, fname)

		if err := downloadTo(ctx, url, fpath); err != nil {
			if log != nil {
				log.WarnContext(ctx, logger.CatTool, "image_gen: save failed",
					"url", url, "path", fpath, "err", err)
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

func downloadTo(ctx context.Context, url, fpath string) error {
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download status %d", resp.StatusCode)
	}
	f, err := os.Create(fpath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// ─── HTTP client ───────────────────────────────────────────────────────

var httpClient = &http.Client{
	Timeout: httpTimeout,
}

func doPost(ctx context.Context, url string, body string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte(body)))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http post %s: %w", url, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http post %s: status %d, body: %s", url, resp.StatusCode, string(respBody))
	}
	return respBody, nil
}

// ─── doubaoProvider (stub) ────────────────────────────────────────────

type doubaoProvider struct{}

func (doubaoProvider) buildSubmitReq(m ImgModelCfg, req submitReq) (string, string, map[string]string, error) {
	return "", "", nil, fmt.Errorf("doubao provider not yet implemented")
}

func (doubaoProvider) buildQueryReq(m ImgModelCfg, jobID string) (string, string, map[string]string, error) {
	return "", "", nil, fmt.Errorf("doubao provider not yet implemented")
}

func (doubaoProvider) parseSubmitResp(body []byte) (string, error) {
	return "", fmt.Errorf("doubao provider not yet implemented")
}

func (doubaoProvider) parseQueryResp(body []byte) (string, []string, []string, error) {
	return "", nil, nil, fmt.Errorf("doubao provider not yet implemented")
}

// ─── Compile-time checks ──────────────────────────────────────────────

var _ Tool = (*imageGenTool)(nil)
