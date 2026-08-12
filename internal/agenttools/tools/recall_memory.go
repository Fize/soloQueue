package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine"
)

// recallMemoryTool lets the LLM search long-term conversation.
type recallMemoryTool struct {
	cfg    Config
	logger *logger.Logger
}

func newRecallMemoryTool(cfg Config) *recallMemoryTool {
	ensureExecutor(&cfg)
	return &recallMemoryTool{cfg: cfg, logger: cfg.Logger}
}

func (recallMemoryTool) Name() string { return "RecallMemory" }

func (recallMemoryTool) Description() string {
	return "Search long-term memory using hybrid search (BM25 keyword matching + Knowledge Graph traversal). " +
		"Use this when the user refers to past conversations, asks about previously discussed topics, " +
		"or when you need historical context. Returns matching entries sorted by relevance."
}

func (recallMemoryTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "query":{"type":"string","description":"Search query. Both keyword and semantic-style queries work."},
    "entities":{"type":"array","items":{"type":"string"},"description":"Optional. Entity names to focus the knowledge graph search on."},
    "limit":{"type":"integer","description":"Max results. Default 10."}
  },
  "required":["query"]
}`)
}

type recallMemoryArgs struct {
	Query    string   `json:"query"`
	Entities []string `json:"entities,omitempty"`
	Limit    int      `json:"limit,omitempty"`
}

func (t *recallMemoryTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	if t.cfg.MemoryAccess == nil {
		return "", fmt.Errorf("memory_not_configured")
	}

	var a recallMemoryArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("query", a.Query); err != nil {
		return "", err
	}
	a.Query = strings.TrimSpace(a.Query)
	if len([]rune(a.Query)) > 2000 || len(a.Entities) > 20 {
		return "", fmt.Errorf("%w: memory query is too large", ErrInvalidArgs)
	}
	for _, entity := range a.Entities {
		if len([]rune(strings.TrimSpace(entity))) > 200 {
			return "", fmt.Errorf("%w: entity is too large", ErrInvalidArgs)
		}
	}

	limit := a.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 20 {
		limit = 20
	}

	result, err := t.cfg.MemoryAccess.Search(ctx, engine.SearchQuery{
		Text:                a.Query,
		Entities:            a.Entities,
		Limit:               limit,
		IncludeGraphContext: len(a.Entities) > 0,
	})
	if err != nil {
		return "", memoryToolError(ctx, err)
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "recall_memory: completed",
			"results", len(result.Results),
			"bm25", result.BM25Count,
			"kg", result.KGCount,
			"vector", result.VectorCount,
		)
	}

	type safeResult struct {
		Untrusted bool                  `json:"untrusted"`
		Results   []engine.SearchResult `json:"results"`
		Graph     []engine.GraphEdge    `json:"graph_edges,omitempty"`
	}
	b, err := json.Marshal(safeResult{Untrusted: true, Results: result.Results, Graph: result.GraphEdges})
	if err != nil {
		return "", fmt.Errorf("memory_unavailable")
	}
	return string(b), nil
}

var _ Tool = (*recallMemoryTool)(nil)
