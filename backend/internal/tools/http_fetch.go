package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

// httpFetchTool 对外发 HTTP GET；含 SSRF 防护
//
// Schema:
//
//	{
//	  "url":"https://example.com/...",
//	  "headers":{"Accept":"application/json"}   // 可选
//	}
//
// 安全：
//   - scheme 仅 http / https；其余 → ErrSchemeNotAllowed
//   - URL 含 userinfo@（credentials）→ 拒绝
//   - 若 HTTPAllowedHosts 非空，host 必须命中之一（否则 ErrHostNotAllowed）
//   - HTTPBlockPrivate=true 时：DNS 查 host；若任一 IP 是私有/环回/链路本地
//     → ErrPrivateAddress（DNS rebinding 简化防护：仅检查一次）
//   - body size 用 http.MaxBytesReader 限 HTTPMaxBody
//   - 超时：HTTPTimeout（优先）或 ctx deadline
type httpFetchTool struct {
	cfg    Config
	client *http.Client
	// allow override of DNS for testing
	lookup func(host string) ([]net.IP, error)
}

func newHTTPFetchTool(cfg Config) *httpFetchTool {
	timeout := cfg.HTTPTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &httpFetchTool{
		cfg: cfg,
		client: &http.Client{
			Timeout: timeout,
			// disable redirects by default — LLM can follow via subsequent calls
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		lookup: net.LookupIP,
	}
}

func (httpFetchTool) Name() string { return "http_fetch" }

func (httpFetchTool) Description() string {
	return "HTTP GET against public URLs (http/https only). Private / loopback / link-local IPs blocked. " +
		"Returns {status,headers,body,truncated}."
}

func (httpFetchTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "url":{"type":"string","description":"Absolute http(s) URL"},
    "headers":{"type":"object","additionalProperties":{"type":"string"}}
  },
  "required":["url"]
}`)
}

type httpFetchArgs struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

type httpFetchResult struct {
	Status    int               `json:"status"`
	Headers   map[string]string `json:"headers"`
	Body      string            `json:"body"`
	Truncated bool              `json:"truncated"`
}

func (t *httpFetchTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	var a httpFetchArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("url", a.URL); err != nil {
		return "", err
	}

	u, err := url.Parse(a.URL)
	if err != nil {
		return "", fmt.Errorf("%w: invalid URL: %v", ErrInvalidArgs, err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		// ok
	default:
		return "", fmt.Errorf("%w: %s", ErrSchemeNotAllowed, u.Scheme)
	}
	if u.User != nil {
		return "", fmt.Errorf("%w: URL must not contain userinfo", ErrInvalidArgs)
	}

	// host allow list
	if len(t.cfg.HTTPAllowedHosts) > 0 {
		host := u.Hostname()
		allowed := false
		for _, h := range t.cfg.HTTPAllowedHosts {
			if strings.EqualFold(h, host) {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", fmt.Errorf("%w: %s", ErrHostNotAllowed, host)
		}
	}

	// SSRF: check resolved IPs
	if t.cfg.HTTPBlockPrivate {
		if ip := net.ParseIP(u.Hostname()); ip != nil {
			if isPrivateIP(ip) {
				return "", fmt.Errorf("%w: %s", ErrPrivateAddress, ip)
			}
		} else {
			ips, lerr := t.lookup(u.Hostname())
			if lerr != nil {
				return "", fmt.Errorf("dns lookup: %w", lerr)
			}
			for _, ip := range ips {
				if isPrivateIP(ip) {
					return "", fmt.Errorf("%w: %s → %s", ErrPrivateAddress, u.Hostname(), ip)
				}
			}
		}
	}

	// timeout: ctx + HTTPTimeout (client already has Timeout)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	for k, v := range a.Headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	maxBody := t.cfg.HTTPMaxBody
	if maxBody <= 0 {
		maxBody = 5 << 20
	}
	// read up to maxBody+1 to detect truncation
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if readErr != nil {
		return "", fmt.Errorf("read body: %w", readErr)
	}
	truncated := false
	if int64(len(data)) > maxBody {
		data = data[:maxBody]
		truncated = true
	}

	// flatten headers (first value each)
	h := make(map[string]string, len(resp.Header))
	for k, vs := range resp.Header {
		if len(vs) > 0 {
			h[k] = vs[0]
		}
	}
	out := httpFetchResult{
		Status:    resp.StatusCode,
		Headers:   h,
		Body:      string(data),
		Truncated: truncated,
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// isPrivateIP reports whether ip is in a non-routable range we want to block.
//
// Covered ranges (IPv4 & IPv6):
//   - 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16  (RFC 1918)
//   - 127.0.0.0/8, ::1                            (loopback)
//   - 169.254.0.0/16, fe80::/10                   (link-local)
//   - fc00::/7                                    (ULA)
//   - 0.0.0.0/8                                   (unspecified)
//   - 100.64.0.0/10                               (CGNAT)
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		switch {
		case ip4[0] == 10:
			return true
		case ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31:
			return true
		case ip4[0] == 192 && ip4[1] == 168:
			return true
		case ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127: // 100.64.0.0/10 (CGNAT)
			return true
		case ip4[0] == 0:
			return true
		}
		return false
	}
	// IPv6: ULA fc00::/7 and similar (first octet 0xfc or 0xfd)
	if len(ip) == net.IPv6len && (ip[0]&0xfe) == 0xfc {
		return true
	}
	return false
}

// CheckConfirmation 实现 agent.Confirmable：HTTP 请求始终需要确认。
func (t *httpFetchTool) CheckConfirmation(raw string) (bool, string) {
	var a httpFetchArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return true, fmt.Sprintf("HTTP request (unable to parse args). Allow?")
	}
	return true, fmt.Sprintf("HTTP GET %q. Allow?", a.URL)
}

// ConfirmationOptions 实现 agent.Confirmable：二元确认。
func (t *httpFetchTool) ConfirmationOptions(_ string) []string { return nil }

// ConfirmArgs 实现 agent.Confirmable：无需修改 args。
func (t *httpFetchTool) ConfirmArgs(original string, choice agent.ConfirmChoice) string {
	if choice != agent.ChoiceApprove {
		return original
	}
	return original
}

// SupportsSessionWhitelist 实现 agent.Confirmable：支持 allow-in-session。
func (t *httpFetchTool) SupportsSessionWhitelist() bool { return true }

// Compile-time checks
var _ agent.Tool = (*httpFetchTool)(nil)
var _ agent.Confirmable = (*httpFetchTool)(nil)
