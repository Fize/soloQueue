package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/todo"
)

// ─── CreatePlan ─────────────────────────────────────────────────────────────

type createPlanTool struct {
	cfg    Config
	logger *logger.Logger
}

func newCreatePlanTool(cfg Config) *createPlanTool {
	ensureExecutor(&cfg)
	return &createPlanTool{cfg: cfg, logger: cfg.Logger}
}

func (createPlanTool) Name() string { return "CreatePlan" }

func (createPlanTool) Description() string {
	return "Create a new task plan with optional embedded todo items and dependencies. " +
		"Plans have three states: plan (draft), running (in progress), done (completed). " +
		"Each plan can contain multiple todo items with dependency relationships. " +
		"Use this when starting a new task, project, or initiative that needs tracking."
}

func (createPlanTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "title":{"type":"string","description":"Plan title. Required, max 500 chars."},
    "content":{"type":"string","description":"Plan description/body. Markdown supported."},
    "status":{"type":"string","description":"Initial status: plan (default), running, or done."},
    "tags":{"type":"string","description":"Comma-separated tags, e.g. 'bug,frontend,urgent'."},
    "creator":{"type":"string","description":"Who created this plan."},
    "todo_items":{"type":"array","items":{"type":"object","properties":{"content":{"type":"string","description":"Todo item content."},"sort_order":{"type":"integer","description":"Sort order (0-based)."},"depends_on":{"type":"array","items":{"type":"string"},"description":"IDs of todo items this depends on."}},"required":["content"]},"description":"Initial todo items to add to the plan."}
  },
  "required":["title"]
}`)
}

func (t *createPlanTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}
	if t.cfg.TodoStore == nil {
		return "", fmt.Errorf("todo system is not available")
	}

	var req todo.CreatePlanRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}

	svc := todo.NewService(t.cfg.TodoStore)
	plan, err := svc.CreatePlan(ctx, req)
	if err != nil {
		return "", err
	}

	result := map[string]any{
		"id":         plan.ID,
		"title":      plan.Title,
		"status":     plan.Status,
		"tags":       plan.Tags,
		"created_at": plan.CreatedAt.Format("2006-01-02 15:04"),
	}
	b, _ := json.Marshal(result)
	return string(b), nil
}

// ─── UpdatePlan ─────────────────────────────────────────────────────────────

type updatePlanTool struct {
	cfg    Config
	logger *logger.Logger
}

func newUpdatePlanTool(cfg Config) *updatePlanTool {
	ensureExecutor(&cfg)
	return &updatePlanTool{cfg: cfg, logger: cfg.Logger}
}

func (updatePlanTool) Name() string { return "UpdatePlan" }

func (updatePlanTool) Description() string {
	return "Update a plan's title, content, status, or tags. " +
		"Only provide fields you want to change; omitted fields are left unchanged. " +
		"Status must be one of: plan, running, done."
}

func (updatePlanTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "id":{"type":"string","description":"Plan ID to update."},
    "title":{"type":"string","description":"New title (optional)."},
    "content":{"type":"string","description":"New content (optional)."},
    "status":{"type":"string","description":"New status: plan, running, or done (optional)."},
    "tags":{"type":"string","description":"New comma-separated tags (optional)."}
  },
  "required":["id"]
}`)
}

func (t *updatePlanTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}
	if t.cfg.TodoStore == nil {
		return "", fmt.Errorf("todo system is not available")
	}

	var args struct {
		ID      string  `json:"id"`
		Title   *string `json:"title"`
		Content *string `json:"content"`
		Status  *string `json:"status"`
		Tags    *string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}

	svc := todo.NewService(t.cfg.TodoStore)
	plan, err := svc.UpdatePlan(ctx, args.ID, todo.UpdatePlanRequest{
		Title:   args.Title,
		Content: args.Content,
		Status:  args.Status,
		Tags:    args.Tags,
	})
	if err != nil {
		return "", err
	}

	result := map[string]any{
		"id":         plan.ID,
		"title":      plan.Title,
		"status":     plan.Status,
		"updated_at": plan.UpdatedAt.Format("2006-01-02 15:04"),
	}
	b, _ := json.Marshal(result)
	return string(b), nil
}

// ─── DeletePlan ─────────────────────────────────────────────────────────────

type deletePlanTool struct {
	cfg    Config
	logger *logger.Logger
}

func newDeletePlanTool(cfg Config) *deletePlanTool {
	ensureExecutor(&cfg)
	return &deletePlanTool{cfg: cfg, logger: cfg.Logger}
}

func (deletePlanTool) Name() string { return "DeletePlan" }

func (deletePlanTool) Description() string {
	return "Delete a plan and all its todo items and dependencies. " +
		"This cannot be undone. Use with caution."
}

func (deletePlanTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "id":{"type":"string","description":"Plan ID to delete."}
  },
  "required":["id"]
}`)
}

func (t *deletePlanTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}
	if t.cfg.TodoStore == nil {
		return "", fmt.Errorf("todo system is not available")
	}

	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}

	svc := todo.NewService(t.cfg.TodoStore)
	if err := svc.DeletePlan(ctx, args.ID); err != nil {
		return "", err
	}

	return fmt.Sprintf(`{"deleted":true,"id":%q}`, args.ID), nil
}
