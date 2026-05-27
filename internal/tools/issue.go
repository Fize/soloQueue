package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/todo"
)

type manageIssueTool struct {
	cfg    Config
	logger *logger.Logger
}

func newManageIssueTool(cfg Config) *manageIssueTool {
	ensureSandbox(&cfg)
	return &manageIssueTool{cfg: cfg, logger: cfg.Logger}
}

func (manageIssueTool) Name() string { return "ManageIssue" }

func (manageIssueTool) Description() string {
	return "Unified tool for managing Kanban board issues, their execution plan, checklists, and comments. " +
		"Supports actions: 'create' (new issue), 'update' (edit details/plan/status), 'delete', 'get' (view details, checklist, comments), " +
		"'list' (by status: backlog, todo, running, done), 'add_comment' (agents must specify their agent name in 'author'), " +
		"'add_task' (add checklist item), 'toggle_task' (mark check/uncheck), and 'set_dependencies' (set checklist dependencies)."
}

func (manageIssueTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "action":{"type":"string","enum":["create","update","delete","get","list","add_comment","add_task","toggle_task","set_dependencies"],"description":"Action to perform."},
    "id":{"type":"string","description":"Issue ID. Required for update, delete, get, add_comment, add_task."},
    "title":{"type":"string","description":"Issue title. Required for create; optional for update."},
    "description":{"type":"string","description":"Brief description of the issue. Optional for create/update."},
    "plan":{"type":"string","description":"Markdown formatted plan / design doc. Optional for create/update."},
    "status":{"type":"string","enum":["backlog","todo","running","done"],"description":"Issue status: backlog, todo, running, or done. Optional for list/update."},
    "tags":{"type":"string","description":"Comma-separated tags (e.g. 'bug,frontend'). Optional for create/update."},
    "comment_content":{"type":"string","description":"Content of the comment. Required for add_comment."},
    "author":{"type":"string","description":"Author / creator of the issue or comment. Optional for create (defaults to agent name); required for add_comment."},
    "task_id":{"type":"string","description":"Task/checklist item ID. Required for toggle_task, set_dependencies."},
    "task_content":{"type":"string","description":"Text content of the checklist item. Required for add_task."},
    "depends_on":{"type":"array","items":{"type":"string"},"description":"Task IDs that this checklist item depends on. Optional for add_task, required for set_dependencies."}
  },
  "required":["action"]
}`)
}

func (t *manageIssueTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}
	if t.cfg.TodoStore == nil {
		return "", fmt.Errorf("kanban issue system is not available")
	}

	var args struct {
		Action         string   `json:"action"`
		ID             string   `json:"id"`
		Title          string   `json:"title"`
		Description    string   `json:"description"`
		Plan           string   `json:"plan"`
		Status         string   `json:"status"`
		Tags           string   `json:"tags"`
		Creator        string   `json:"creator"`
		CommentContent string   `json:"comment_content"`
		Author         string   `json:"author"`
		TaskID         string   `json:"task_id"`
		TaskContent    string   `json:"task_content"`
		DependsOn      []string `json:"depends_on"`
	}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}

	svc := todo.NewService(t.cfg.TodoStore)

	switch args.Action {
	case "create":
		if args.Title == "" {
			return "", fmt.Errorf("title is required for create")
		}
		author := args.Author
		if author == "" {
			author = args.Creator
		}
		if author == "" {
			author = iface.AgentNameFromContext(ctx)
		}
		p, err := svc.CreatePlan(ctx, todo.CreatePlanRequest{
			Title:       args.Title,
			Description: args.Description,
			Plan:        args.Plan,
			Status:      args.Status,
			Tags:        args.Tags,
			Author:      author,
		})
		if err != nil {
			return "", err
		}
		return serialize(p)

	case "update":
		if args.ID == "" {
			return "", fmt.Errorf("id is required for update")
		}
		req := todo.UpdatePlanRequest{}
		if args.Title != "" {
			req.Title = &args.Title
		}
		if args.Description != "" {
			req.Description = &args.Description
		}
		if args.Plan != "" {
			req.Plan = &args.Plan
		}
		if args.Status != "" {
			req.Status = &args.Status
		}
		if args.Tags != "" {
			req.Tags = &args.Tags
		}
		p, err := svc.UpdatePlan(ctx, args.ID, req)
		if err != nil {
			return "", err
		}
		return serialize(p)

	case "delete":
		if args.ID == "" {
			return "", fmt.Errorf("id is required for delete")
		}
		if err := svc.DeletePlan(ctx, args.ID); err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"deleted":true,"id":%q}`, args.ID), nil

	case "get":
		if args.ID == "" {
			return "", fmt.Errorf("id is required for get")
		}
		p, err := svc.GetPlan(ctx, args.ID)
		if err != nil {
			return "", err
		}
		return serialize(p)

	case "list":
		plans, err := svc.ListPlans(ctx, args.Status)
		if err != nil {
			return "", err
		}
		return serialize(map[string]any{
			"total":  len(plans),
			"issues": plans,
		})

	case "add_comment":
		if args.ID == "" {
			return "", fmt.Errorf("id (issue_id) is required for add_comment")
		}
		if args.CommentContent == "" {
			return "", fmt.Errorf("comment_content is required for add_comment")
		}
		if args.Author == "" {
			return "", fmt.Errorf("author is required for add_comment")
		}
		c, err := svc.AddComment(ctx, args.ID, args.Author, args.CommentContent)
		if err != nil {
			return "", err
		}
		return serialize(c)

	case "add_task":
		if args.ID == "" {
			return "", fmt.Errorf("id (issue_id) is required for add_task")
		}
		if args.TaskContent == "" {
			return "", fmt.Errorf("task_content is required for add_task")
		}
		t, err := svc.CreateTodoItem(ctx, args.ID, todo.CreateTodoRequest{
			Content:   args.TaskContent,
			DependsOn: args.DependsOn,
		})
		if err != nil {
			return "", err
		}
		return serialize(t)

	case "toggle_task":
		if args.TaskID == "" {
			return "", fmt.Errorf("task_id is required for toggle_task")
		}
		t, err := svc.ToggleTodoItem(ctx, args.TaskID)
		if err != nil {
			return "", err
		}
		return serialize(t)

	case "set_dependencies":
		if args.TaskID == "" {
			return "", fmt.Errorf("task_id is required for set_dependencies")
		}
		if err := svc.SetDependencies(ctx, args.TaskID, args.DependsOn); err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"updated":true,"task_id":%q,"depends_on":%v}`, args.TaskID, args.DependsOn), nil

	default:
		return "", fmt.Errorf("unsupported action: %q", args.Action)
	}
}

func serialize(val any) (string, error) {
	b, err := json.Marshal(val)
	if err != nil {
		return "", fmt.Errorf("serialize: %w", err)
	}
	return string(b), nil
}
