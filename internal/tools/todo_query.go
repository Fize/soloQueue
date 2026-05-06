package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/todo"
)

// ─── ListPlans ──────────────────────────────────────────────────────────────

type listPlansTool struct {
	cfg    Config
	logger *logger.Logger
}

func newListPlansTool(cfg Config) *listPlansTool {
	ensureExecutor(&cfg)
	return &listPlansTool{cfg: cfg, logger: cfg.Logger}
}

func (listPlansTool) Name() string { return "ListPlans" }

func (listPlansTool) Description() string {
	return "List all plans, optionally filtered by status. " +
		"Status can be: plan (draft), running (in progress), or done (completed). " +
		"Omit status to list all plans. Results are ordered by most recently updated first."
}

func (listPlansTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "status":{"type":"string","description":"Optional filter: plan, running, or done."}
  }
}`)
}

func (t *listPlansTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}
	if t.cfg.TodoStore == nil {
		return "", fmt.Errorf("todo system is not available")
	}

	var args struct {
		Status string `json:"status"`
	}
	json.Unmarshal([]byte(raw), &args) // optional, ignore errors

	svc := todo.NewService(t.cfg.TodoStore)
	plans, err := svc.ListPlans(ctx, strings.TrimSpace(args.Status))
	if err != nil {
		return "", err
	}

	if len(plans) == 0 {
		return "No plans found.", nil
	}

	// Build a compact summary for the LLM
	type summary struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Status    string `json:"status"`
		Tags      string `json:"tags,omitempty"`
		Creator   string `json:"creator,omitempty"`
		UpdatedAt string `json:"updated_at"`
	}
	var items []summary
	for _, p := range plans {
		items = append(items, summary{
			ID:        p.ID,
			Title:     p.Title,
			Status:    string(p.Status),
			Tags:      p.Tags,
			Creator:   p.Creator,
			UpdatedAt: p.UpdatedAt.Format("2006-01-02 15:04"),
		})
	}

	b, _ := json.Marshal(map[string]any{
		"total": len(items),
		"plans": items,
	})
	return string(b), nil
}

// ─── GetPlan ────────────────────────────────────────────────────────────────

type getPlanTool struct {
	cfg    Config
	logger *logger.Logger
}

func newGetPlanTool(cfg Config) *getPlanTool {
	ensureExecutor(&cfg)
	return &getPlanTool{cfg: cfg, logger: cfg.Logger}
}

func (getPlanTool) Name() string { return "GetPlan" }

func (getPlanTool) Description() string {
	return "Get full details of a plan including all its todo items with their " +
		"completion status, sort order, and dependency relationships. " +
		"Use this to review progress, check blockers, or analyze task structure."
}

func (getPlanTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "id":{"type":"string","description":"Plan ID to retrieve."}
  },
  "required":["id"]
}`)
}

func (t *getPlanTool) Execute(ctx context.Context, raw string) (string, error) {
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
	plan, err := svc.GetPlan(ctx, args.ID)
	if err != nil {
		return "", err
	}

	// Build detailed response with todo items and their dependencies
	type todoSummary struct {
		ID        string   `json:"id"`
		Content   string   `json:"content"`
		Completed bool     `json:"completed"`
		SortOrder int      `json:"sort_order"`
		DependsOn []string `json:"depends_on,omitempty"`
	}
	var todos []todoSummary
	for _, t := range plan.TodoItems {
		todos = append(todos, todoSummary{
			ID:        t.ID,
			Content:   t.Content,
			Completed: t.Completed,
			SortOrder: t.SortOrder,
			DependsOn: t.DependsOn,
		})
	}

	b, _ := json.Marshal(map[string]any{
		"id":         plan.ID,
		"title":      plan.Title,
		"content":    plan.Content,
		"status":     string(plan.Status),
		"tags":       plan.Tags,
		"creator":    plan.Creator,
		"created_at": plan.CreatedAt.Format("2006-01-02 15:04"),
		"updated_at": plan.UpdatedAt.Format("2006-01-02 15:04"),
		"todo_items": todos,
	})
	return string(b), nil
}
