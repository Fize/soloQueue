package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// rememberTool lets the LLM save important information to permanent memory.
type rememberTool struct {
	cfg    Config
	logger *logger.Logger
}

func newRememberTool(cfg Config) *rememberTool {
	ensureExecutor(&cfg)
	return &rememberTool{cfg: cfg, logger: cfg.Logger}
}

func (rememberTool) Name() string { return "Remember" }

func (rememberTool) Description() string {
	return "Save important information to permanent long-term memory. " +
		"Use this when the user explicitly asks you to remember something, " +
		"or when you encounter information likely to be useful in future conversations. " +
		"Requires an enabled embedding configuration."
}

func (rememberTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "content":{"type":"string","description":"The information to save. Be concise but include all key details."},
    "timestamp":{"type":"string","description":"Optional. The time this information is about, in YYYY-MM-DD HH:MM format. Use the actual time the event occurred or was discussed, not the current time. If omitted, defaults to now."}
  },
  "required":["content"]
}`)
}

type rememberArgs struct {
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
}

type rememberResult struct {
	ID    string `json:"id"`
	Saved bool   `json:"saved"`
}

func (t *rememberTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	if t.cfg.PermanentManager == nil {
		return "", fmt.Errorf("permanent memory is not configured; enable embedding in settings.toml")
	}

	var a rememberArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("content", a.Content); err != nil {
		return "", err
	}

	var at time.Time
	if a.Timestamp != "" {
		var err error
		at, err = time.Parse("2006-01-02 15:04", a.Timestamp)
		if err != nil {
			return "", fmt.Errorf("invalid timestamp format, expected YYYY-MM-DD HH:MM: %w", err)
		}
	}

	if err := t.cfg.PermanentManager.Remember(ctx, a.Content, at); err != nil {
		return "", err
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "remember: saved")
	}

	b, _ := json.Marshal(rememberResult{Saved: true})
	return string(b), nil
}

var _ Tool = (*rememberTool)(nil)
