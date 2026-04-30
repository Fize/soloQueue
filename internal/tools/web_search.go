package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// webSearchTool 通过 DuckDuckGo Lite 搜索网页
//
// Schema:
//
//	{
//	  "query":"golang errgroup",
//	  "max_results":5     // 默认 5；上限 20
//	}
//
// 无需 API Key，始终注册。
// 请求 POST https://lite.duckduckgo.com/lite/，解析 HTML 提取结果。
//
// 返回（面向 LLM）：
//
//	{"results":[{"title","url","content"}]}
type webSearchTool struct {
	cfg    Config
	logger *logger.Logger
	client *http.Client
}

func newWebSearchTool(cfg Config) *webSearchTool {
	timeout := cfg.WebSearchTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &webSearchTool{
		cfg:    cfg,
		logger: cfg.Logger,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (webSearchTool) Name() string { return "WebSearch" }

func (webSearchTool) Description() string {
	return "Search the web via DuckDuckGo. Returns {results:[{title,url,content}]}."
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

type ddgResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

type webSearchResult struct {
	Results []ddgResult `json:"results"`
}

func (t *webSearchTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	var a webSearchArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("query", a.Query); err != nil {
		return "", err
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "web_search: starting",
			"query", a.Query)
	}
	start := time.Now()

	maxR := a.MaxResults
	if maxR <= 0 {
		maxR = 5
	}
	if maxR > 20 {
		maxR = 20
	}

	form := url.Values{}
	form.Set("q", a.Query)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://lite.duckduckgo.com/lite/", strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; SoloQueue/1.0)")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if readErr != nil {
		return "", fmt.Errorf("read WebSearch body: %w", readErr)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("WebSearch %d: %s", resp.StatusCode, truncateString(string(data), 200))
	}

	results := parseDDGResults(data, maxR)

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "web_search: completed",
			"query", a.Query, "results", len(results),
			"duration_ms", time.Since(start).Milliseconds())
	}

	out := webSearchResult{Results: results}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// parseDDGResults 从 DuckDuckGo Lite HTML 中提取搜索结果
func parseDDGResults(htmlData []byte, maxResults int) []ddgResult {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(htmlData)))
	if err != nil {
		return nil
	}

	var results []ddgResult
	doc.Find("a.result-link").Each(func(i int, s *goquery.Selection) {
		if len(results) >= maxResults {
			return
		}

		title := strings.TrimSpace(s.Text())
		href, _ := s.Attr("href")

		// DDG Lite 的 href 可能是直接 URL 或跳转链接
		realURL := resolveDDGURL(href)
		if realURL == "" {
			realURL = href
		}

		// 摘要在下一个 <tr> 的 td.result-snippet 中
		snippet := ""
		row := s.Closest("tr")
		if row.Length() > 0 {
			nextRow := row.Next()
			if nextRow.Length() > 0 {
				snippetTD := nextRow.Find("td.result-snippet")
				if snippetTD.Length() > 0 {
					snippet = strings.TrimSpace(snippetTD.Text())
				}
			}
		}

		if title != "" && realURL != "" {
			results = append(results, ddgResult{
				Title:   title,
				URL:     realURL,
				Content: snippet,
			})
		}
	})

	return results
}

// resolveDDGURL 从 DDG 跳转链接中提取真实 URL
// DDG Lite 格式: //duckduckgo.com/l/?uddg=<encoded_url>&rut=...
// 或直接是 https://... URL
func resolveDDGURL(href string) string {
	if href == "" {
		return ""
	}

	// 补全协议
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}

	u, err := url.Parse(href)
	if err != nil {
		return href
	}

	// 如果是 DDG 跳转链接，提取 uddg 参数
	if strings.Contains(u.Host, "duckduckgo.com") && u.Path == "/l/" {
		if uddg := u.Query().Get("uddg"); uddg != "" {
			decoded, err := url.QueryUnescape(uddg)
			if err == nil {
				return decoded
			}
			return uddg
		}
	}

	return href
}

// Compile-time check
var _ Tool = (*webSearchTool)(nil)
