package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// rememberTool lets the LLM save important information to permanent memory.
type rememberTool struct {
	cfg    Config
	logger *logger.Logger
}

func newRememberTool(cfg Config) *rememberTool { ensureExecutor(&cfg); return &rememberTool{cfg: cfg, logger: cfg.Logger} }

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
    "content":{"type":"string","description":"The information to save. Be concise but include all key details."}
  },
  "required":["content"]
}`)
}

type rememberArgs struct {
	Content string `json:"content"`
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

	if err := t.cfg.PermanentManager.Remember(ctx, a.Content); err != nil {
		return "", err
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "remember: saved")
	}

	b, _ := json.Marshal(rememberResult{Saved: true})
	return string(b), nil
}

var _ Tool = (*rememberTool)(nil)
