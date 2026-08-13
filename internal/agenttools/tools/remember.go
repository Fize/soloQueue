package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine"
)

// rememberTool lets the LLM save important information to permanent conversation.
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
	return "Save important information to long-term conversation. " +
		"Use this when the user explicitly asks you to remember something, " +
		"or when you encounter information likely to be useful in future conversations. " +
		"Save only durable preferences, decisions, stable facts, or reusable solutions. " +
		"Do not save routine task completion reports, transient output, or duplicates."
}

func (rememberTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "content":{"type":"string","description":"The information to save. Be concise but include all key details."},
	    "memory_type":{"type":"string","enum":["preference","decision","stable_fact","reusable_solution"],"description":"Why this information remains useful across future conversations."},
	    "subject_key":{"type":"string","description":"Optional stable dotted key for a mutable preference, decision, or fact."},
	    "replaces_content_hash":{"type":"string","description":"Optional content hash of the active memory explicitly replaced by this one. Requires subject_key."},
	    "explicit_user_request":{"type":"boolean","description":"True only when the user explicitly asked to remember this information."},
    "timestamp":{"type":"string","description":"Optional. The time this information is about, in YYYY-MM-DD HH:MM format. Use the actual time the event occurred or was discussed, not the current time. If omitted, defaults to now."}
  },
  "required":["content","memory_type"]
}`)
}

type rememberArgs struct {
	Content             string `json:"content"`
	MemoryType          string `json:"memory_type"`
	SubjectKey          string `json:"subject_key,omitempty"`
	ReplacesContentHash string `json:"replaces_content_hash,omitempty"`
	ExplicitUserRequest bool   `json:"explicit_user_request,omitempty"`
	Timestamp           string `json:"timestamp"`
}

type rememberResult struct {
	ContentHash string `json:"content_hash"`
	Saved       bool   `json:"saved"`
	IsNew       bool   `json:"is_new"`
	Action      string `json:"action"`
	Reason      string `json:"reason,omitempty"`
}

func (t *rememberTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	if t.cfg.MemoryAccess == nil {
		return "", fmt.Errorf("memory_not_configured")
	}

	var a rememberArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("content", a.Content); err != nil {
		return "", err
	}
	a.Content = strings.TrimSpace(a.Content)
	if len([]rune(a.Content)) > 8000 {
		return "", fmt.Errorf("%w: content exceeds 8000 characters", ErrInvalidArgs)
	}
	switch a.MemoryType {
	case engine.MemoryTypePreference, engine.MemoryTypeDecision, engine.MemoryTypeStableFact, engine.MemoryTypeReusableSolution:
	default:
		return "", fmt.Errorf("%w: unsupported memory_type", ErrInvalidArgs)
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

	sourceType := engine.SourceAgent
	if a.ExplicitUserRequest {
		sourceType = engine.SourceExplicit
	}
	result, err := t.cfg.MemoryAccess.Ingest(ctx, engine.MemoryCandidate{
		Content:             a.Content,
		SubjectKey:          a.SubjectKey,
		ReplacesContentHash: a.ReplacesContentHash,
		MemoryType:          a.MemoryType,
		SourceType:          sourceType,
		SourceID:            t.cfg.WorkDir,
		Date:                date,
		EventTime:           eventTime,
		ExplicitUserRequest: a.ExplicitUserRequest,
	})
	if err != nil {
		return "", memoryToolError(ctx, err)
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "remember: evaluated",
			"action", result.Action, "reason", result.Reason)
	}

	b, _ := json.Marshal(rememberResult{
		ContentHash: result.ContentHash,
		Saved:       result.Action == "insert" || result.Action == "replace",
		IsNew:       result.IsNew,
		Action:      result.Action,
		Reason:      result.Reason,
	})
	return string(b), nil
}

var _ Tool = (*rememberTool)(nil)
