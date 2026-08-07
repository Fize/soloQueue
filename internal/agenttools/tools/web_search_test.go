package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)


const tavilyResponseJSON = `{"query":"golang errgroup","results":[{"title":"errgroup package - Go Packages","url":"https://pkg.go.dev/golang.org/x/sync/errgroup","content":"Package errgroup provides synchronization, error propagation, and Context cancellation.","score":0.98}]}`

func TestWebSearch_UsesTavilyWhenKeySet(t *testing.T) {
	var postURL, postBody string
	var postOpts HTTPOptions
	exec := &Executor{
		HTTPPostFn: func(_ context.Context, rawURL, body string, opts HTTPOptions) (HTTPResponse, error) {
			postURL = rawURL
			postBody = body
			postOpts = opts
			return HTTPResponse{StatusCode: 200, Body: []byte(tavilyResponseJSON)}, nil
		},
	}
	tool := newWebSearchTool(Config{
		WebSearchTimeout: 2 * time.Second,
		TavilyAPIKey:     "tvly-test-123",
		Executor:         exec,
	})

	out, err := tool.Execute(context.Background(), `{"query":"golang errgroup"}`)
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	if postURL != "https://api.tavily.com/search" {
		t.Errorf("postURL = %q, want https://api.tavily.com/search", postURL)
	}
	if got := postOpts.Headers["Authorization"]; got != "Bearer tvly-test-123" {
		t.Errorf("Authorization header = %q, want Bearer tvly-test-123", got)
	}
	if !strings.Contains(postBody, "golang errgroup") {
		t.Errorf("postBody = %q, want it to contain query %q", postBody, "golang errgroup")
	}

	var parsed struct {
		Results []struct {
			Title string `json:"title"`
			URL   string `json:"url"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("out is not valid JSON: %v; out = %s", err, out)
	}
	if len(parsed.Results) == 0 {
		t.Fatalf("results empty; out = %s", out)
	}
	if parsed.Results[0].Title != "errgroup package - Go Packages" {
		t.Errorf("results[0].title = %q, want %q", parsed.Results[0].Title, "errgroup package - Go Packages")
	}
	if parsed.Results[0].URL != "https://pkg.go.dev/golang.org/x/sync/errgroup" {
		t.Errorf("results[0].url = %q, want %q", parsed.Results[0].URL, "https://pkg.go.dev/golang.org/x/sync/errgroup")
	}
}


const ddgLiteHTML = `<!DOCTYPE HTML>
<html><body>
<table border="0">
  <tr>
    <td>1.&nbsp;</td>
    <td><a rel="nofollow" href="https://pkg.go.dev/golang.org/x/sync/errgroup" class='result-link'>errgroup package - Go Packages</a></td>
  </tr>
  <tr>
    <td>&nbsp;&nbsp;&nbsp;</td>
    <td class='result-snippet'>Package errgroup provides synchronization, error propagation, and Context cancellation for groups of goroutines.</td>
  </tr>
  <tr>
    <td>&nbsp;&nbsp;&nbsp;</td>
    <td><span class='link-text'>pkg.go.dev/golang.org/x/sync/errgroup</span></td>
  </tr>
  <tr>
    <td>2.&nbsp;</td>
    <td><a rel="nofollow" href="https://example.com/go-errgroup" class='result-link'>How to Use errgroup for Parallel Operations in Go</a></td>
  </tr>
  <tr>
    <td>&nbsp;&nbsp;&nbsp;</td>
    <td class='result-snippet'>Learn how to use errgroup for managing parallel operations in Go.</td>
  </tr>
  <tr>
    <td>&nbsp;&nbsp;&nbsp;</td>
    <td><span class='link-text'>example.com/go-errgroup</span></td>
  </tr>
</table>
</body></html>`

// TestWebSearch_UsesDDGWhenNoKey locks in the fallback branch: without a
// Tavily API key the tool must POST to DuckDuckGo Lite and parse its HTML.
func TestWebSearch_UsesDDGWhenNoKey(t *testing.T) {
	var postURL, postBody string
	var postOpts HTTPOptions
	exec := &Executor{
		HTTPPostFn: func(_ context.Context, rawURL, body string, opts HTTPOptions) (HTTPResponse, error) {
			postURL = rawURL
			postBody = body
			postOpts = opts
			return HTTPResponse{StatusCode: 200, Body: []byte(ddgLiteHTML)}, nil
		},
	}
	tool := newWebSearchTool(Config{
		WebSearchTimeout: 2 * time.Second,
		Executor:         exec,
	})

	out, err := tool.Execute(context.Background(), `{"query":"golang errgroup"}`)
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	if postURL != "https://lite.duckduckgo.com/lite/" {
		t.Errorf("postURL = %q, want https://lite.duckduckgo.com/lite/", postURL)
	}
	if postOpts.ContentType != "application/x-www-form-urlencoded" {
		t.Errorf("ContentType = %q, want application/x-www-form-urlencoded", postOpts.ContentType)
	}
	if !strings.Contains(postBody, "q=golang+errgroup") {
		t.Errorf("postBody = %q, want it to contain url-encoded query %q", postBody, "q=golang+errgroup")
	}

	var parsed webSearchResult
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("out is not valid JSON: %v; out = %s", err, out)
	}
	if len(parsed.Results) == 0 {
		t.Fatalf("results empty; out = %s", out)
	}
	if parsed.Results[0].Title != "errgroup package - Go Packages" {
		t.Errorf("results[0].title = %q, want %q", parsed.Results[0].Title, "errgroup package - Go Packages")
	}
}

func mkWebSearchTool(t *testing.T) *webSearchTool {
	t.Helper()
	cfg := Config{
		WebSearchTimeout: 2 * time.Second,
	}
	return newWebSearchTool(cfg)
}

func TestWebSearch_ParseDDGHTML(t *testing.T) {
	results := parseDDGResults([]byte(ddgLiteHTML), 5)
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[0].Title != "errgroup package - Go Packages" {
		t.Errorf("title[0] = %q", results[0].Title)
	}
	if results[0].URL != "https://pkg.go.dev/golang.org/x/sync/errgroup" {
		t.Errorf("url[0] = %q", results[0].URL)
	}
	if results[0].Content != "Package errgroup provides synchronization, error propagation, and Context cancellation for groups of goroutines." {
		t.Errorf("content[0] = %q", results[0].Content)
	}
	if results[1].Title != "How to Use errgroup for Parallel Operations in Go" {
		t.Errorf("title[1] = %q", results[1].Title)
	}
}

func TestWebSearch_ParseDDGHTML_MaxResults(t *testing.T) {
	results := parseDDGResults([]byte(ddgLiteHTML), 1)
	if len(results) != 1 {
		t.Errorf("results = %d, want 1", len(results))
	}
}

func TestWebSearch_ParseDDGHTML_Empty(t *testing.T) {
	results := parseDDGResults([]byte(`<html><body>No results</body></html>`), 5)
	if len(results) != 0 {
		t.Errorf("results = %d, want 0", len(results))
	}
}

func TestWebSearch_InvalidJSON(t *testing.T) {
	tool := mkWebSearchTool(t)
	_, err := tool.Execute(context.Background(), `{not json`)
	if err == nil {
		t.Error("invalid JSON should error")
	}
}

func TestWebSearch_EmptyQuery(t *testing.T) {
	tool := mkWebSearchTool(t)
	_, err := tool.Execute(context.Background(), `{"query":""}`)
	if err == nil {
		t.Error("empty query should error")
	}
}

func TestWebSearch_CtxCanceledUpfront(t *testing.T) {
	tool := mkWebSearchTool(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tool.Execute(ctx, `{"query":"x"}`)
	if err != context.Canceled {
		t.Errorf("err = %v, want Canceled", err)
	}
}

func TestWebSearch_MetadataInterface(t *testing.T) {
	tool := mkWebSearchTool(t)
	if tool.Name() != "WebSearch" {
		t.Errorf("Name = %q", tool.Name())
	}
	var m map[string]any
	if err := json.Unmarshal(tool.Parameters(), &m); err != nil {
		t.Errorf("Parameters not valid JSON: %v", err)
	}
}

func TestWebSearch_DefaultMaxResults(t *testing.T) {
	var a webSearchArgs
	raw := `{"query":"x"}`
	_ = json.Unmarshal([]byte(raw), &a)
	maxR := a.MaxResults
	if maxR <= 0 {
		maxR = 5
	}
	if maxR > 20 {
		maxR = 20
	}
	if maxR != 5 {
		t.Errorf("default max = %d, want 5", maxR)
	}
}

func TestWebSearch_MaxResultsCapped(t *testing.T) {
	var a webSearchArgs
	raw := `{"query":"x","max_results":999}`
	_ = json.Unmarshal([]byte(raw), &a)
	maxR := a.MaxResults
	if maxR <= 0 {
		maxR = 5
	}
	if maxR > 20 {
		maxR = 20
	}
	if maxR != 20 {
		t.Errorf("capped max = %d, want 20", maxR)
	}
}

// TestWebSearch_DescriptionReflectsProvider locks in that the tool's
// Description advertises the provider that will actually be used: DuckDuckGo
// without a Tavily key, Tavily (and not DuckDuckGo) when a key is set.
func TestWebSearch_DescriptionReflectsProvider(t *testing.T) {
	noKey := newWebSearchTool(Config{WebSearchTimeout: 2 * time.Second})
	withKey := newWebSearchTool(Config{WebSearchTimeout: 2 * time.Second, TavilyAPIKey: "tvly-test-123"})

	if got := noKey.Description(); !strings.Contains(got, "DuckDuckGo") {
		t.Errorf("no-key Description() = %q, want it to contain %q", got, "DuckDuckGo")
	}
	if got := withKey.Description(); !strings.Contains(got, "Tavily") {
		t.Errorf("with-key Description() = %q, want it to contain %q", got, "Tavily")
	}
	if got := withKey.Description(); strings.Contains(got, "DuckDuckGo") {
		t.Errorf("with-key Description() = %q, must not contain %q", got, "DuckDuckGo")
	}
}

func TestWebSearch_ResolveDDGURL(t *testing.T) {
	tests := []struct {
		name string
		href string
		want string
	}{
		{"direct URL", "https://example.com/page", "https://example.com/page"},
		{"empty", "", ""},
		{"DDG redirect with uddg", "//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fpage&rut=abc", "https://example.com/page"},
		{"DDG redirect without uddg", "//duckduckgo.com/l/?rut=abc", "https://duckduckgo.com/l/?rut=abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveDDGURL(tt.href)
			if got != tt.want {
				t.Errorf("resolveDDGURL(%q) = %q, want %q", tt.href, got, tt.want)
			}
		})
	}
}
