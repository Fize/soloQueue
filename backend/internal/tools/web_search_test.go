package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func mkWebSearchTool(t *testing.T, endpoint, apiKey string) *webSearchTool {
	t.Helper()
	cfg := Config{
		TavilyAPIKey:   apiKey,
		TavilyEndpoint: endpoint,
		TavilyTimeout:  2 * time.Second,
	}
	return newWebSearchTool(cfg)
}

func TestWebSearch_Happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// parse request body to verify fields
		var body tavilyReqBody
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.APIKey != "tvly-test" {
			t.Errorf("api_key = %q", body.APIKey)
		}
		if body.Query != "golang errgroup" {
			t.Errorf("query = %q", body.Query)
		}
		if body.MaxResults != 3 {
			t.Errorf("max_results = %d", body.MaxResults)
		}
		// return minimal Tavily response
		resp := tavilyRespBody{
			Answer: "errgroup is useful",
			Results: []tavilyResult{
				{Title: "pkg.go.dev", URL: "https://pkg.go.dev/golang.org/x/sync/errgroup", Score: 0.99},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	tool := mkWebSearchTool(t, srv.URL, "tvly-test")
	raw, _ := json.Marshal(webSearchArgs{Query: "golang errgroup", MaxResults: 3})
	out, err := tool.Execute(context.Background(), string(raw))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var r webSearchResult
	_ = json.Unmarshal([]byte(out), &r)
	if r.Answer != "errgroup is useful" {
		t.Errorf("answer = %q", r.Answer)
	}
	if len(r.Results) != 1 {
		t.Errorf("results = %d", len(r.Results))
	}
}

func TestWebSearch_APIKeyEmpty(t *testing.T) {
	tool := mkWebSearchTool(t, "http://example.com", "")
	_, err := tool.Execute(context.Background(), `{"query":"x"}`)
	if !errors.Is(err, ErrTavilyDisabled) {
		t.Errorf("err = %v, want ErrTavilyDisabled", err)
	}
}

func TestWebSearch_Build_OmitsWhenAPIKeyEmpty(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AllowedDirs = []string{t.TempDir()}
	cfg.TavilyAPIKey = ""
	list := Build(cfg)
	for _, tool := range list {
		if tool.Name() == "web_search" {
			t.Error("web_search should not be registered when APIKey empty")
		}
	}
}

func TestWebSearch_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	tool := mkWebSearchTool(t, srv.URL, "tvly-test")
	_, err := tool.Execute(context.Background(), `{"query":"x"}`)
	if err == nil || !strings.Contains(err.Error(), "tavily 500") {
		t.Errorf("err = %v, want tavily 500", err)
	}
}

func TestWebSearch_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	tool := mkWebSearchTool(t, srv.URL, "tvly-wrong")
	_, err := tool.Execute(context.Background(), `{"query":"x"}`)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("err = %v, want 401", err)
	}
}

func TestWebSearch_InvalidResponseJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()
	tool := mkWebSearchTool(t, srv.URL, "tvly-test")
	_, err := tool.Execute(context.Background(), `{"query":"x"}`)
	if err == nil || !strings.Contains(err.Error(), "parse tavily") {
		t.Errorf("err = %v, want parse error", err)
	}
}

func TestWebSearch_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()
	cfg := Config{
		TavilyAPIKey:   "tvly-test",
		TavilyEndpoint: srv.URL,
		TavilyTimeout:  50 * time.Millisecond,
	}
	tool := newWebSearchTool(cfg)
	_, err := tool.Execute(context.Background(), `{"query":"x"}`)
	if err == nil {
		t.Error("expected timeout")
	}
}

func TestWebSearch_EmptyQuery(t *testing.T) {
	tool := mkWebSearchTool(t, "http://example.com", "tvly-test")
	_, err := tool.Execute(context.Background(), `{"query":""}`)
	if err == nil {
		t.Error("empty query should error")
	}
}

func TestWebSearch_MaxResultsCapped(t *testing.T) {
	var gotMax int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body tavilyReqBody
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotMax = body.MaxResults
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	tool := mkWebSearchTool(t, srv.URL, "tvly-test")
	raw, _ := json.Marshal(webSearchArgs{Query: "x", MaxResults: 999})
	_, _ = tool.Execute(context.Background(), string(raw))
	if gotMax != 20 {
		t.Errorf("capped max = %d, want 20", gotMax)
	}
}

func TestWebSearch_DefaultMaxResults(t *testing.T) {
	var gotMax int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body tavilyReqBody
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotMax = body.MaxResults
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	tool := mkWebSearchTool(t, srv.URL, "tvly-test")
	_, _ = tool.Execute(context.Background(), `{"query":"x"}`)
	if gotMax != 5 {
		t.Errorf("default max = %d, want 5", gotMax)
	}
}

func TestWebSearch_InvalidJSON(t *testing.T) {
	tool := mkWebSearchTool(t, "http://example.com", "tvly-test")
	_, err := tool.Execute(context.Background(), `{not json`)
	if err == nil {
		t.Error("invalid JSON should error")
	}
}

func TestWebSearch_CtxCanceledUpfront(t *testing.T) {
	tool := mkWebSearchTool(t, "http://example.com", "tvly-test")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tool.Execute(ctx, `{"query":"x"}`)
	if err != context.Canceled {
		t.Errorf("err = %v, want Canceled", err)
	}
}

func TestWebSearch_MetadataInterface(t *testing.T) {
	tool := mkWebSearchTool(t, "http://example.com", "tvly-test")
	if tool.Name() != "web_search" {
		t.Errorf("Name = %q", tool.Name())
	}
	var m map[string]any
	if err := json.Unmarshal(tool.Parameters(), &m); err != nil {
		t.Errorf("Parameters not valid JSON: %v", err)
	}
}
