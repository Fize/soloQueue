package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// recallMemoryTool lets the LLM search permanent long-term memory.
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
	return "Search permanent long-term memory for information related to a query. " +
		"Use this when the user refers to past conversations, asks about previously " +
		"discussed topics, or when you need historical context to answer a question. " +
		"Returns matching entries with dates and summaries. " +
		"Requires an enabled embedding configuration."
}

func (recallMemoryTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "query":{"type":"string","description":"Natural language search query to find relevant memories."}
  },
  "required":["query"]
}`)
}

type recallMemoryArgs struct {
	Query string `json:"query"`
}

func (t *recallMemoryTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	if t.cfg.PermanentManager == nil {
		return "", fmt.Errorf("permanent memory is not configured; enable embedding in settings.toml")
	}

	var a recallMemoryArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("query", a.Query); err != nil {
		return "", err
	}

	result, err := t.cfg.PermanentManager.QueryForPrompt(ctx, a.Query)
	if err != nil {
		return "", err
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "recall_memory: completed",
			"has_results", result != "")
	}

	if result == "" {
		return "No relevant memories found.", nil
	}
	return result, nil
}

var _ Tool = (*recallMemoryTool)(nil)
