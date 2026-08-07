package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
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
    "explicit_user_request":{"type":"boolean","description":"True only when the user explicitly asked to remember this information."},
    "timestamp":{"type":"string","description":"Optional. The time this information is about, in YYYY-MM-DD HH:MM format. Use the actual time the event occurred or was discussed, not the current time. If omitted, defaults to now."}
  },
  "required":["content","memory_type"]
}`)
}

type rememberArgs struct {
	Content             string `json:"content"`
	MemoryType          string `json:"memory_type"`
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

	scopeType, scopeID := memoryScopeForWorkDir(t.cfg.WorkDir)
	sourceType := engine.SourceAgent
	if a.ExplicitUserRequest {
		sourceType = engine.SourceExplicit
	}
	result, err := t.cfg.MemoryEngine.Ingest(ctx, engine.MemoryCandidate{
		Content:             a.Content,
		MemoryType:          a.MemoryType,
		ScopeType:           scopeType,
		ScopeID:             scopeID,
		SourceType:          sourceType,
		SourceID:            t.cfg.WorkDir,
		Date:                date,
		EventTime:           eventTime,
		ExplicitUserRequest: a.ExplicitUserRequest,
	})
	if err != nil {
		return "", err
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "remember: evaluated",
			"hash", result.ContentHash, "action", result.Action, "reason", result.Reason)
	}

	b, _ := json.Marshal(rememberResult{
		ContentHash: result.ContentHash,
		Saved:       result.Action == "insert",
		IsNew:       result.IsNew,
		Action:      result.Action,
		Reason:      result.Reason,
	})
	return string(b), nil
}

func memoryScopeForWorkDir(workDir string) (string, string) {
	workDir = filepath.Clean(strings.TrimSpace(workDir))
	if workDir == "." || workDir == "" || strings.HasSuffix(workDir, string(filepath.Separator)+".soloqueue") {
		return engine.ScopeGlobal, ""
	}
	return engine.ScopeProject, workDir
}

var _ Tool = (*rememberTool)(nil)
