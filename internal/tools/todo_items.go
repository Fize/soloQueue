package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/todo"
)

// ─── AddTodoItems ───────────────────────────────────────────────────────────

type addTodoItemsTool struct {
	cfg    Config
	logger *logger.Logger
}

func newAddTodoItemsTool(cfg Config) *addTodoItemsTool {
	ensureExecutor(&cfg)
	return &addTodoItemsTool{cfg: cfg, logger: cfg.Logger}
}

func (addTodoItemsTool) Name() string { return "AddTodoItems" }

func (addTodoItemsTool) Description() string {
	return "Add one or more todo items to an existing plan. " +
		"Each item can optionally specify dependencies on other todo items. " +
		"Returns the created items with their IDs."
}

func (addTodoItemsTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "plan_id":{"type":"string","description":"The plan ID to add items to."},
    "items":{"type":"array","items":{"type":"object","properties":{"content":{"type":"string","description":"Todo item content."},"sort_order":{"type":"integer","description":"Sort order (0-based)."},"depends_on":{"type":"array","items":{"type":"string"},"description":"IDs of todo items this depends on."}},"required":["content"]},"description":"The todo items to add."}
  },
  "required":["plan_id","items"]
}`)
}

func (t *addTodoItemsTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}
	if t.cfg.TodoStore == nil {
		return "", fmt.Errorf("todo system is not available")
	}

	var args struct {
		PlanID string                   `json:"plan_id"`
		Items  []todo.CreateTodoRequest `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}

	if len(args.Items) == 0 {
		return "", fmt.Errorf("%w: items array is empty", ErrInvalidArgs)
	}

	svc := todo.NewService(t.cfg.TodoStore)
	var created []map[string]any
	for _, item := range args.Items {
		result, err := svc.CreateTodoItem(ctx, args.PlanID, item)
		if err != nil {
			return "", err
		}
		created = append(created, map[string]any{
			"id":      result.ID,
			"content": result.Content,
			"deps":    result.DependsOn,
		})
	}

	b, _ := json.Marshal(map[string]any{"created": created})
	return string(b), nil
}

// ─── DeleteTodoItems ────────────────────────────────────────────────────────

type deleteTodoItemsTool struct {
	cfg    Config
	logger *logger.Logger
}

func newDeleteTodoItemsTool(cfg Config) *deleteTodoItemsTool {
	ensureExecutor(&cfg)
	return &deleteTodoItemsTool{cfg: cfg, logger: cfg.Logger}
}

func (deleteTodoItemsTool) Name() string { return "DeleteTodoItems" }

func (deleteTodoItemsTool) Description() string {
	return "Delete one or more todo items by ID. Dependencies are cleaned up automatically."
}

func (deleteTodoItemsTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "ids":{"type":"array","items":{"type":"string"},"description":"IDs of todo items to delete."}
  },
  "required":["ids"]
}`)
}

func (t *deleteTodoItemsTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}
	if t.cfg.TodoStore == nil {
		return "", fmt.Errorf("todo system is not available")
	}

	var args struct {
		IDs []string `json:"ids"`
	}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}

	svc := todo.NewService(t.cfg.TodoStore)
	for _, id := range args.IDs {
		if err := svc.DeleteTodoItem(ctx, id); err != nil {
			return "", err
		}
	}

	b, _ := json.Marshal(map[string]any{"deleted": len(args.IDs)})
	return string(b), nil
}

// ─── ToggleTodo ─────────────────────────────────────────────────────────────

type toggleTodoTool struct {
	cfg    Config
	logger *logger.Logger
}

func newToggleTodoTool(cfg Config) *toggleTodoTool {
	ensureExecutor(&cfg)
	return &toggleTodoTool{cfg: cfg, logger: cfg.Logger}
}

func (toggleTodoTool) Name() string { return "ToggleTodo" }

func (toggleTodoTool) Description() string {
	return "Toggle a todo item's completion status. " +
		"When marking as complete, all dependencies must already be completed. " +
		"When un-completing, there is no restriction. " +
		"Returns the new completion status."
}

func (toggleTodoTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "id":{"type":"string","description":"Todo item ID to toggle."}
  },
  "required":["id"]
}`)
}

func (t *toggleTodoTool) Execute(ctx context.Context, raw string) (string, error) {
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
	item, err := svc.ToggleTodoItem(ctx, args.ID)
	if err != nil {
		return "", err
	}

	b, _ := json.Marshal(map[string]any{
		"id":        item.ID,
		"content":   item.Content,
		"completed": item.Completed,
	})
	return string(b), nil
}

// ─── SetTodoDependencies ────────────────────────────────────────────────────

type setTodoDependenciesTool struct {
	cfg    Config
	logger *logger.Logger
}

func newSetTodoDependenciesTool(cfg Config) *setTodoDependenciesTool {
	ensureExecutor(&cfg)
	return &setTodoDependenciesTool{cfg: cfg, logger: cfg.Logger}
}

func (setTodoDependenciesTool) Name() string { return "SetTodoDependencies" }

func (setTodoDependenciesTool) Description() string {
	return "Set or replace the dependencies for a todo item. " +
		"Pass a complete list of IDs that this item depends on. " +
		"Previous dependencies are removed and replaced. " +
		"Cyclic dependencies are detected and rejected. " +
		"A todo item cannot depend on itself."
}

func (setTodoDependenciesTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "id":{"type":"string","description":"Todo item ID."},
    "depends_on":{"type":"array","items":{"type":"string"},"description":"Complete list of IDs this item depends on. Pass empty array to clear all dependencies."}
  },
  "required":["id","depends_on"]
}`)
}

func (t *setTodoDependenciesTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}
	if t.cfg.TodoStore == nil {
		return "", fmt.Errorf("todo system is not available")
	}

	var args struct {
		ID        string   `json:"id"`
		DependsOn []string `json:"depends_on"`
	}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}

	svc := todo.NewService(t.cfg.TodoStore)
	if err := svc.SetDependencies(ctx, args.ID, args.DependsOn); err != nil {
		return "", err
	}

	b, _ := json.Marshal(map[string]any{
		"id":         args.ID,
		"depends_on": args.DependsOn,
	})
	return string(b), nil
}
