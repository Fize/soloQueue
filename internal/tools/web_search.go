package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

)

// webSearchTool 调用 Tavily 搜索 API
//
// Schema:
//
//	{
//	  "query":"golang errgroup",
//	  "max_results":5     // 默认 5；上限 20
//	}
//
// 仅当 Config.TavilyAPIKey != "" 时被 Build 注册；否则省略。
//
// Tavily wire（POST https://api.tavily.com/search）：
//
//	{"api_key":"...","query":"...","max_results":5,"search_depth":"basic"}
//
// 返回（面向 LLM）：
//
//	{"answer":"...","results":[{"title","url","content","score"}]}
type webSearchTool struct {
	cfg    Config
	client *http.Client
}

func newWebSearchTool(cfg Config) *webSearchTool {
	timeout := cfg.TavilyTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &webSearchTool{
		cfg: cfg,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (webSearchTool) Name() string { return "web_search" }

func (webSearchTool) Description() string {
	return "Search the web via Tavily. Returns {answer, results:[{title,url,content,score}]}."
}

func (webSearchTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "query":{"type":"string"},
    "max_results":{"type":"integer","minimum":1,"maximum":20,"default":5}
  },
  "required":["query"]
}`)
}

type webSearchArgs struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results,omitempty"`
}

type tavilyReqBody struct {
	APIKey      string `json:"api_key"`
	Query       string `json:"query"`
	MaxResults  int    `json:"max_results"`
	SearchDepth string `json:"search_depth,omitempty"`
}

type tavilyResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

type tavilyRespBody struct {
	Answer  string         `json:"answer,omitempty"`
	Results []tavilyResult `json:"results"`
}

type webSearchResult struct {
	Answer  string         `json:"answer,omitempty"`
	Results []tavilyResult `json:"results"`
}

func (t *webSearchTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}
	if t.cfg.TavilyAPIKey == "" {
		return "", ErrTavilyDisabled
	}

	var a webSearchArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("query", a.Query); err != nil {
		return "", err
	}

	maxR := a.MaxResults
	if maxR <= 0 {
		maxR = 5
	}
	if maxR > 20 {
		maxR = 20
	}

	body := tavilyReqBody{
		APIKey:      t.cfg.TavilyAPIKey,
		Query:       a.Query,
		MaxResults:  maxR,
		SearchDepth: "basic",
	}
	bodyJSON, _ := json.Marshal(body)

	endpoint := t.cfg.TavilyEndpoint
	if endpoint == "" {
		endpoint = "https://api.tavily.com/search"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyJSON))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if readErr != nil {
		return "", fmt.Errorf("read tavily body: %w", readErr)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("tavily %d: %s", resp.StatusCode, truncateString(string(data), 200))
	}

	var parsed tavilyRespBody
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", fmt.Errorf("parse tavily response: %w", err)
	}

	out := webSearchResult{
		Answer:  parsed.Answer,
		Results: parsed.Results,
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// Compile-time check
var _ Tool = (*webSearchTool)(nil)
