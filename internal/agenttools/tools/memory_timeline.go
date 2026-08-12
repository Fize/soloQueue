package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine"
)

// memoryTimelineTool lists memories chronologically.
type memoryTimelineTool struct {
	cfg    Config
	logger *logger.Logger
}

func newMemoryTimelineTool(cfg Config) *memoryTimelineTool {
	ensureExecutor(&cfg)
	return &memoryTimelineTool{cfg: cfg, logger: cfg.Logger}
}

func (memoryTimelineTool) Name() string { return "MemoryTimeline" }

func (memoryTimelineTool) Description() string {
	return "List memories chronologically within a date range. " +
		"Use this to review what happened during a specific time period."
}

func (memoryTimelineTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "from":{"type":"string","description":"Start date YYYY-MM-DD. Optional."},
    "to":{"type":"string","description":"End date YYYY-MM-DD. Optional."},
    "limit":{"type":"integer","description":"Maximum entries. Default 50."}
  },
  "required":[]
}`)
}

type memoryTimelineArgs struct {
	From  string `json:"from,omitempty"`
	To    string `json:"to,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

func (t *memoryTimelineTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	if t.cfg.MemoryAccess == nil {
		return "", fmt.Errorf("memory_not_configured")
	}

	var a memoryTimelineArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}

	if a.Limit <= 0 {
		a.Limit = 50
	}
	if a.Limit > 100 {
		a.Limit = 100
	}
	for _, value := range []string{a.From, a.To} {
		if value != "" {
			if _, err := time.Parse("2006-01-02", value); err != nil {
				return "", fmt.Errorf("%w: dates must use YYYY-MM-DD", ErrInvalidArgs)
			}
		}
	}
	if a.From != "" && a.To != "" && a.From > a.To {
		return "", fmt.Errorf("%w: from must not be after to", ErrInvalidArgs)
	}

	entries, err := t.cfg.MemoryAccess.Timeline(ctx, a.From, a.To, a.Limit)
	if err != nil {
		return "", memoryToolError(ctx, err)
	}

	result, err := json.Marshal(struct {
		Untrusted bool                 `json:"untrusted"`
		Entries   []engine.MemoryEntry `json:"entries"`
	}{Untrusted: true, Entries: entries})
	if err != nil {
		return "", fmt.Errorf("memory_unavailable")
	}
	return string(result), nil
}

var _ Tool = (*memoryTimelineTool)(nil)
