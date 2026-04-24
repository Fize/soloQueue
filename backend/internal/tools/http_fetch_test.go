package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

func mkHTTPTool(t *testing.T, cfgMut func(*Config)) *httpFetchTool {
	t.Helper()
	cfg := Config{
		HTTPMaxBody:      1 << 20,
		HTTPTimeout:      2 * time.Second,
		HTTPBlockPrivate: true,
	}
	if cfgMut != nil {
		cfgMut(&cfg)
	}
	// use a lookup that returns public IPs by default (so httptest URLs don't get blocked
	// when we turn off HTTPBlockPrivate in tests)
	tool := newHTTPFetchTool(cfg)
	return tool
}

func TestHTTP_Happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test", "yes")
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	tool := mkHTTPTool(t, func(c *Config) {
		c.HTTPBlockPrivate = false // httptest uses 127.0.0.1
	})
	raw, _ := json.Marshal(httpFetchArgs{URL: srv.URL})
	out, err := tool.Execute(context.Background(), string(raw))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var r httpFetchResult
	_ = json.Unmarshal([]byte(out), &r)
	if r.Status != 200 {
		t.Errorf("status = %d", r.Status)
	}
	if r.Body != "hello" {
		t.Errorf("body = %q", r.Body)
	}
	if r.Headers["X-Test"] != "yes" {
		t.Errorf("header = %+v", r.Headers)
	}
}

func TestHTTP_404Passes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	tool := mkHTTPTool(t, func(c *Config) { c.HTTPBlockPrivate = false })
	raw, _ := json.Marshal(httpFetchArgs{URL: srv.URL})
	out, err := tool.Execute(context.Background(), string(raw))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var r httpFetchResult
	_ = json.Unmarshal([]byte(out), &r)
	if r.Status != 404 {
		t.Errorf("status = %d", r.Status)
	}
}

func TestHTTP_SSRF_Loopback(t *testing.T) {
	tool := mkHTTPTool(t, nil) // BlockPrivate = true
	raw, _ := json.Marshal(httpFetchArgs{URL: "http://127.0.0.1/"})
	_, err := tool.Execute(context.Background(), string(raw))
	if err == nil || !strings.Contains(err.Error(), "private address") {
		t.Errorf("err = %v, want private address", err)
	}
}

func TestHTTP_SSRF_DNSPrivate(t *testing.T) {
	tool := mkHTTPTool(t, nil)
	// inject lookup returning RFC1918 IP
	tool.lookup = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.0.0.1")}, nil
	}
	raw, _ := json.Marshal(httpFetchArgs{URL: "http://internal.example.com/"})
	_, err := tool.Execute(context.Background(), string(raw))
	if err == nil || !strings.Contains(err.Error(), "private address") {
		t.Errorf("err = %v, want private address", err)
	}
}

func TestHTTP_SchemeRejected(t *testing.T) {
	tool := mkHTTPTool(t, nil)
	for _, u := range []string{"ftp://example.com", "file:///etc/passwd"} {
		raw, _ := json.Marshal(httpFetchArgs{URL: u})
		_, err := tool.Execute(context.Background(), string(raw))
		if err == nil || !strings.Contains(err.Error(), "scheme not allowed") {
			t.Errorf("%s: err = %v, want scheme not allowed", u, err)
		}
	}
}

func TestHTTP_UserinfoRejected(t *testing.T) {
	tool := mkHTTPTool(t, nil)
	raw, _ := json.Marshal(httpFetchArgs{URL: "http://user:pass@example.com/"})
	_, err := tool.Execute(context.Background(), string(raw))
	if err == nil || !strings.Contains(err.Error(), "userinfo") {
		t.Errorf("err = %v, want userinfo", err)
	}
}

func TestHTTP_HostNotAllowed(t *testing.T) {
	tool := mkHTTPTool(t, func(c *Config) {
		c.HTTPBlockPrivate = false
		c.HTTPAllowedHosts = []string{"example.com"}
	})
	raw, _ := json.Marshal(httpFetchArgs{URL: "http://evil.com/"})
	_, err := tool.Execute(context.Background(), string(raw))
	if err == nil || !strings.Contains(err.Error(), "host not allowed") {
		t.Errorf("err = %v, want host not allowed", err)
	}
}

func TestHTTP_BodyTruncation(t *testing.T) {
	// 1kb body
	body := strings.Repeat("x", 1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	tool := mkHTTPTool(t, func(c *Config) {
		c.HTTPBlockPrivate = false
		c.HTTPMaxBody = 100
	})
	raw, _ := json.Marshal(httpFetchArgs{URL: srv.URL})
	out, err := tool.Execute(context.Background(), string(raw))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var r httpFetchResult
	_ = json.Unmarshal([]byte(out), &r)
	if len(r.Body) != 100 {
		t.Errorf("body len = %d, want 100", len(r.Body))
	}
	if !r.Truncated {
		t.Error("truncated should be true")
	}
}

func TestHTTP_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()
	tool := mkHTTPTool(t, func(c *Config) {
		c.HTTPBlockPrivate = false
		c.HTTPTimeout = 50 * time.Millisecond
	})
	raw, _ := json.Marshal(httpFetchArgs{URL: srv.URL})
	_, err := tool.Execute(context.Background(), string(raw))
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestHTTP_CustomHeaders(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Custom")
	}))
	defer srv.Close()

	tool := mkHTTPTool(t, func(c *Config) { c.HTTPBlockPrivate = false })
	raw, _ := json.Marshal(httpFetchArgs{
		URL: srv.URL, Headers: map[string]string{"X-Custom": "hello"},
	})
	_, err := tool.Execute(context.Background(), string(raw))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotHeader != "hello" {
		t.Errorf("X-Custom = %q", gotHeader)
	}
}

func TestHTTP_InvalidURL(t *testing.T) {
	tool := mkHTTPTool(t, nil)
	raw, _ := json.Marshal(httpFetchArgs{URL: "::not a url::"})
	_, err := tool.Execute(context.Background(), string(raw))
	if err == nil {
		t.Error("invalid URL should error")
	}
}

func TestHTTP_EmptyURL(t *testing.T) {
	tool := mkHTTPTool(t, nil)
	_, err := tool.Execute(context.Background(), `{"url":""}`)
	if err == nil {
		t.Error("empty URL should error")
	}
}

func TestHTTP_InvalidJSON(t *testing.T) {
	tool := mkHTTPTool(t, nil)
	_, err := tool.Execute(context.Background(), `{not json`)
	if err == nil {
		t.Error("invalid JSON should error")
	}
}

func TestHTTP_CtxCanceledUpfront(t *testing.T) {
	tool := mkHTTPTool(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw, _ := json.Marshal(httpFetchArgs{URL: "https://example.com/"})
	_, err := tool.Execute(ctx, string(raw))
	if err != context.Canceled {
		t.Errorf("err = %v, want Canceled", err)
	}
}

func TestIsPrivateIP(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"172.32.0.0", false},
		{"192.168.1.1", true},
		{"169.254.1.1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"::1", true},
		{"fe80::1", true},
		{"fc00::1", true},
		{"fd00::1", true},
		{"2001:4860:4860::8888", false},
		{"100.64.0.1", true},
		{"0.0.0.0", true},
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		got := isPrivateIP(ip)
		if got != c.want {
			t.Errorf("isPrivateIP(%s) = %v, want %v", c.ip, got, c.want)
		}
	}
}

func TestHTTP_MetadataInterface(t *testing.T) {
	tool := mkHTTPTool(t, nil)
	if tool.Name() != "http_fetch" {
		t.Errorf("Name = %q", tool.Name())
	}
	var m map[string]any
	if err := json.Unmarshal(tool.Parameters(), &m); err != nil {
		t.Errorf("Parameters not valid JSON: %v", err)
	}
}

// ─── Confirmable 接口测试 ────────────────────────────────────────────────────

func TestHTTPFetch_CheckConfirmation_AlwaysNeedsConfirm(t *testing.T) {
	tool := mkHTTPTool(t, nil)
	raw, _ := json.Marshal(httpFetchArgs{URL: "https://example.com/api/data"})
	needs, prompt := tool.CheckConfirmation(string(raw))
	if !needs {
		t.Error("http_fetch should always need confirmation")
	}
	if prompt == "" {
		t.Error("expected non-empty prompt")
	}
	if !strings.Contains(prompt, "example.com") {
		t.Errorf("prompt should contain URL, got: %s", prompt)
	}
}

func TestHTTPFetch_CheckConfirmation_InvalidJSON(t *testing.T) {
	tool := mkHTTPTool(t, nil)
	needs, prompt := tool.CheckConfirmation(`{not json`)
	if !needs {
		t.Error("should still need confirm even with invalid JSON")
	}
	if prompt == "" {
		t.Error("expected non-empty fallback prompt")
	}
}

func TestHTTPFetch_ConfirmationOptions_Binary(t *testing.T) {
	tool := mkHTTPTool(t, nil)
	raw, _ := json.Marshal(httpFetchArgs{URL: "https://example.com/"})
	if opts := tool.ConfirmationOptions(string(raw)); opts != nil {
		t.Errorf("expected nil for binary confirm, got %v", opts)
	}
}

func TestHTTPFetch_ConfirmArgs_PreservesOriginal(t *testing.T) {
	tool := mkHTTPTool(t, nil)
	original := `{"url":"https://example.com/"}`
	for _, choice := range []agent.ConfirmChoice{agent.ChoiceApprove, agent.ChoiceDeny, agent.ChoiceAllowInSession} {
		got := tool.ConfirmArgs(original, choice)
		if got != original {
			t.Errorf("choice=%v: expected original preserved, got %s", choice, got)
		}
	}
}

func TestHTTPFetch_SupportsSessionWhitelist(t *testing.T) {
	tool := mkHTTPTool(t, nil)
	if !tool.SupportsSessionWhitelist() {
		t.Error("should support session whitelist")
	}
}

// shut up unused import if needed (fmt kept for future use)
var _ = fmt.Sprintf
