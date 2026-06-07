package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// memoryTimelineTool lists memories chronologically.
type memoryTimelineTool struct {
	cfg    Config
	logger *logger.Logger
}

func newMemoryTimelineTool(cfg Config) *memoryTimelineTool {
	ensureSandbox(&cfg)
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

	if t.cfg.MemoryEngine == nil {
		return "", fmt.Errorf("memory engine is not configured; check your settings")
	}

	var a memoryTimelineArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}

	if a.Limit <= 0 {
		a.Limit = 50
	}

	entries, err := t.cfg.MemoryEngine.Timeline(ctx, a.From, a.To, a.Limit)
	if err != nil {
		return "", err
	}

	if len(entries) == 0 {
		return "No memories found in this date range.", nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Timeline (%d entries):\n\n", len(entries)))
	for i, e := range entries {
		b.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, e.Date, e.EventTime))
		content := e.Content
		if len(content) > 200 {
			content = content[:200] + "..."
		}
		b.WriteString(fmt.Sprintf("   %s\n\n", content))
	}
	return b.String(), nil
}

var _ Tool = (*memoryTimelineTool)(nil)
