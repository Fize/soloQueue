package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/memoryengine"
)

// rememberTool lets the LLM save important information to permanent memory.
type rememberTool struct {
	cfg    Config
	logger *logger.Logger
}

func newRememberTool(cfg Config) *rememberTool {
	ensureSandbox(&cfg)
	return &rememberTool{cfg: cfg, logger: cfg.Logger}
}

func (rememberTool) Name() string { return "Remember" }

func (rememberTool) Description() string {
	return "Save important information to long-term memory. " +
		"Use this when the user explicitly asks you to remember something, " +
		"or when you encounter information likely to be useful in future conversations. " +
		"Optionally include extracted entities and their relationships to build the knowledge graph."
}

func (rememberTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "content":{"type":"string","description":"The information to save. Be concise but include all key details."},
    "timestamp":{"type":"string","description":"Optional. The time this information is about, in YYYY-MM-DD HH:MM format. Use the actual time the event occurred or was discussed, not the current time. If omitted, defaults to now."},
    "entities":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"type":{"type":"string"},"relations":{"type":"array","items":{"type":"object","properties":{"target_name":{"type":"string"},"rel_type":{"type":"string"},"weight":{"type":"number"}}}}}},"description":"Optional. Extracted entities and their relationships to index in the knowledge graph."}
  },
  "required":["content"]
}`)
}

type rememberArgs struct {
	Content   string              `json:"content"`
	Timestamp string              `json:"timestamp"`
	Entities  []memoryengine.EntityExtraction `json:"entities,omitempty"`
}

type rememberResult struct {
	ContentHash string `json:"content_hash"`
	Saved       bool   `json:"saved"`
	IsNew       bool   `json:"is_new"`
}

func (t *rememberTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	if t.cfg.MemoryEngine == nil {
		return "", fmt.Errorf("memory engine is not configured; check your settings")
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
	} else {
		at = time.Now()
	}

	date := at.Format("2006-01-02")
	eventTime := at.Format(time.RFC3339)

	var hash string
	var isNew bool
	var err error

	if len(a.Entities) > 0 {
		hash, isNew, err = t.cfg.MemoryEngine.SaveWithEntities(ctx, a.Content, date, "", eventTime, a.Entities)
	} else {
		hash, isNew, err = t.cfg.MemoryEngine.Save(ctx, a.Content, date, "", eventTime)
	}
	if err != nil {
		return "", err
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "remember: saved", "hash", hash, "is_new", isNew)
	}

	b, _ := json.Marshal(rememberResult{ContentHash: hash, Saved: true, IsNew: isNew})
	return string(b), nil
}

var _ Tool = (*rememberTool)(nil)
