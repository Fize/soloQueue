package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/memoryengine"
)

// recallMemoryTool lets the LLM search long-term memory.
type recallMemoryTool struct {
	cfg    Config
	logger *logger.Logger
}

func newRecallMemoryTool(cfg Config) *recallMemoryTool {
	ensureSandbox(&cfg)
	return &recallMemoryTool{cfg: cfg, logger: cfg.Logger}
}

func (recallMemoryTool) Name() string { return "RecallMemory" }

func (recallMemoryTool) Description() string {
	desc := "Search long-term memory using hybrid search (BM25 keyword matching + Knowledge Graph traversal). "
	if desc != "" {
		desc += "Use this when the user refers to past conversations, asks about previously discussed topics, " +
			"or when you need historical context. Returns matching entries sorted by relevance."
	}
	return desc
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

	if t.cfg.MemoryEngine == nil {
		return "", fmt.Errorf("memory engine is not configured; check your settings")
	}

	var a recallMemoryArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("query", a.Query); err != nil {
		return "", err
	}

	limit := a.Limit
	if limit <= 0 {
		limit = 10
	}

	result, err := t.cfg.MemoryEngine.Search(ctx, memoryengine.SearchQuery{
		Text:               strings.TrimSpace(a.Query),
		Entities:           a.Entities,
		Limit:              limit,
		IncludeGraphContext: len(a.Entities) > 0,
	})
	if err != nil {
		return "", err
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "recall_memory: completed",
			"results", len(result.Results),
			"bm25", result.BM25Count,
			"kg", result.KGCount,
			"vector", result.VectorCount,
		)
	}

	if len(result.Results) == 0 {
		return "No relevant memories found.", nil
	}

	// Format results for LLM consumption
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d relevant memories:\n\n", len(result.Results)))
	for i, r := range result.Results {
		b.WriteString(fmt.Sprintf("%d. [%s] (score: %.2f, source: %s)\n", i+1, r.Date, r.Score, r.Source))
		if r.EventTime != "" && r.EventTime != r.Date {
			b.WriteString(fmt.Sprintf("   Event time: %s\n", r.EventTime))
		}
		b.WriteString(fmt.Sprintf("   %s\n\n", r.Content))
	}

	// Append graph context if available
	if len(result.GraphEdges) > 0 {
		b.WriteString("\n--- Knowledge Graph Context ---\n")
		for _, e := range result.GraphEdges {
			b.WriteString(fmt.Sprintf("- %s --[%s]--> %s (weight: %.2f)\n",
				e.SourceName, e.RelType, e.TargetName, e.Weight))
		}
	}

	return b.String(), nil
}

var _ Tool = (*recallMemoryTool)(nil)
