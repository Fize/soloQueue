package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// httpFetchTool performs outbound HTTP GET requests with SSRF protection.
//
// Schema:
//
//	{
//	  "url":"https://example.com/...",
//	  "headers":{"Accept":"application/json"}   // optional
//	}
//
// Security behavior:
//   - Only http/https schemes are allowed; others return ErrSchemeNotAllowed.
//   - URLs containing userinfo@ credentials are rejected.
//   - If HTTPAllowedHosts is non-empty, the host must match one of them or ErrHostNotAllowed is returned.
//   - When HTTPBlockPrivate=true, the host is resolved via DNS and any IP that is private, loopback, or link-local returns ErrPrivateAddress (a simplified DNS rebinding protection check).
//   - Body size is limited via http.MaxBytesReader using HTTPMaxBody.
//   - Timeout uses HTTPTimeout first, or the context deadline if present.
type httpFetchTool struct {
	cfg    Config
	logger *logger.Logger
	// allow override of DNS for testing
	lookup func(ctx context.Context, host string) ([]net.IP, error)
}

func newHTTPFetchTool(cfg Config) *httpFetchTool {
	ensureSandbox(&cfg)
	return &httpFetchTool{
		cfg:    cfg,
		logger: cfg.Logger,
		lookup: func(ctx context.Context, host string) ([]net.IP, error) {
			var r net.Resolver
			addrs, err := r.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			ips := make([]net.IP, len(addrs))
			for i, a := range addrs {
				ips[i] = a.IP
			}
			return ips, nil
		},
	}
}

func (httpFetchTool) Name() string { return "WebFetch" }

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

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "http_fetch: starting",
			"url", a.URL)
	}
	start := time.Now()

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
			ips, lerr := t.lookup(ctx, u.Hostname())
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
	maxBody := t.cfg.HTTPMaxBody
	if maxBody <= 0 {
		maxBody = 5 << 20
	}

	httpResp, err := t.cfg.Sandbox.HTTPGet(ctx, a.URL, HTTPOptions{
		Timeout:      t.cfg.HTTPTimeout,
		MaxBody:      maxBody,
		Headers:      a.Headers,
		BlockPrivate: t.cfg.HTTPBlockPrivate,
	})
	if err != nil {
		return "", err
	}

	data := httpResp.Body
	truncated := false
	if int64(len(data)) > maxBody {
		data = data[:maxBody]
		truncated = true
	}

	out := httpFetchResult{
		Status:    httpResp.StatusCode,
		Body:      string(data),
		Truncated: truncated,
	}
	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "http_fetch: completed",
			"url", a.URL,
			"status", httpResp.StatusCode,
			"body_len", len(data),
			"truncated", truncated,
			"duration_ms", time.Since(start).Milliseconds())
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// Compile-time checks
func (t *httpFetchTool) CheckConfirmation(raw string) (bool, string) {
	var a httpFetchArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return true, "HTTP request (unable to parse args). Allow?"
	}
	return true, fmt.Sprintf("HTTP GET %q. Allow?", a.URL)
}

// ConfirmationOptions implements Confirmable: binary confirmation.
func (t *httpFetchTool) ConfirmationOptions(_ string) []string { return nil }

// ConfirmArgs implements Confirmable: no args modification needed.
func (t *httpFetchTool) ConfirmArgs(original string, choice ConfirmChoice) string {
	if choice != ChoiceApprove {
		return original
	}
	return original
}

// SupportsSessionWhitelist implements Confirmable: supports allow-in-session.
func (t *httpFetchTool) SupportsSessionWhitelist() bool { return true }

// Compile-time checks
var _ Tool = (*httpFetchTool)(nil)
var _ Confirmable = (*httpFetchTool)(nil)
