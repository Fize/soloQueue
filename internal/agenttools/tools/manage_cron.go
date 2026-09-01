package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/cron"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

// manageCronTool provides a unified interface for managing scheduled tasks (create, list, update, delete).
type manageCronTool struct {
	cfg    Config
	logger *logger.Logger
}

func newManageCronTool(cfg Config) *manageCronTool {
	ensureExecutor(&cfg)
	return &manageCronTool{cfg: cfg, logger: cfg.Logger}
}

func (manageCronTool) Name() string { return "manage_cron" }

func (manageCronTool) Description() string {
	return "Manage scheduled cron tasks (create, list, update, delete). " +
		"Supports recurring tasks (using standard 5-field cron expression) " +
		"and one-time tasks (using absolute local datetime string like 'YYYY-MM-DD HH:MM:SS')."
}

func (t manageCronTool) Parameters() json.RawMessage {
	targetAgent := ""
	if t.cfg.CronScope.IsGlobal() {
		targetAgent = `,
    "target_agent": {
      "type": "string",
      "description": "Optional (Global scope only). Target execution agent or team."
    }`
	}
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "action": {
      "type": "string",
      "description": "Action to perform: 'create', 'list', 'update', or 'delete'.",
      "enum": ["create", "list", "update", "delete"]
    },
    "task_id": {
      "type": "string",
      "description": "The unique ID of the cron job (required for 'update' and 'delete')."
    },
    "title": {
      "type": "string",
      "description": "Concise task title (required for 'create', optional for 'update')."
    },
    "task_type": {
      "type": "string",
      "description": "Task type (required for 'create', optional for 'update').",
      "enum": ["general", "engineering", "research"]
    },
    "schedule": {
      "type": "string",
      "description": "Schedule expression: standard 5-field cron or 'YYYY-MM-DD HH:MM:SS' (required for 'create', optional for 'update')."
    },
    "instruction": {
      "type": "string",
      "description": "Instruction/reminder content to run when triggered (required for 'create', optional for 'update')."
    },
    "status": {
      "type": "string",
      "description": "Task status filter for 'list', or new status ('active'/'paused') for 'update'.",
      "enum": ["active", "paused", "running", "completed"]
    }` + targetAgent + `
  },
  "required": ["action"]
}`)
}

type manageCronArgs struct {
	Action      string `json:"action"`
	TaskID      string `json:"task_id"`
	Title       string `json:"title"`
	TaskType    string `json:"task_type"`
	Schedule    string `json:"schedule"`
	Instruction string `json:"instruction"`
	TargetAgent string `json:"target_agent"`
	Status      string `json:"status"`
}

func (t *manageCronTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	if t.cfg.CronStore == nil || t.cfg.CronScheduler == nil {
		return "", fmt.Errorf("scheduled tasks system is not configured/available")
	}

	var a manageCronArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}

	switch strings.ToLower(a.Action) {
	case "create":
		return t.executeCreate(ctx, a)
	case "list":
		return t.executeList(ctx, a)
	case "update":
		return t.executeUpdate(ctx, a)
	case "delete":
		return t.executeDelete(ctx, a)
	default:
		return "", fmt.Errorf("%w: invalid action %q (must be create, list, update, or delete)", ErrInvalidArgs, a.Action)
	}
}

func (t *manageCronTool) executeCreate(ctx context.Context, a manageCronArgs) (string, error) {
	if err := validateNotZeroLen("title", a.Title); err != nil {
		return "", err
	}
	if err := cron.ValidateTaskTitle(a.Title); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("task_type", a.TaskType); err != nil {
		return "", err
	}
	if err := cron.ValidateTaskType(a.TaskType); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("schedule", a.Schedule); err != nil {
		return "", err
	}
	if err := validateNotZeroLen("instruction", a.Instruction); err != nil {
		return "", err
	}

	nextRun, err := cron.NextTrigger(a.Schedule, time.Now())
	if err != nil {
		return "", fmt.Errorf("invalid schedule expression: %w", err)
	}

	if cron.IsOneTimeExpression(a.Schedule) && nextRun.Before(time.Now().Add(-1*time.Minute)) {
		return "", fmt.Errorf("the scheduled time %s has already passed (current time: %s)",
			nextRun.Format("2006-01-02 15:04:05"),
			time.Now().Format("2006-01-02 15:04:05"))
	}

	targetAgent := a.TargetAgent
	if t.cfg.CronScope.IsTeam() {
		targetAgent = t.cfg.CronScope.Owner
	}

	task, err := t.cfg.CronStore.CreateTask(ctx, cron.CreateTaskInput{
		Title:       a.Title,
		TaskType:    a.TaskType,
		Expression:  a.Schedule,
		Instruction: a.Instruction,
		TargetAgent: targetAgent,
		NextRunAt:   nextRun,
	})
	if err != nil {
		return "", fmt.Errorf("failed to save task: %w", err)
	}

	t.cfg.CronScheduler.Schedule(*task)

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "cron: task dynamically scheduled via tool", "task_id", task.ID, "next_run", nextRun.Format(time.RFC3339))
	}

	res := struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		TaskType  string `json:"task_type"`
		NextRunAt string `json:"next_run_at"`
		Status    string `json:"status"`
	}{
		ID:        task.ID,
		Title:     task.Title,
		TaskType:  task.TaskType,
		NextRunAt: task.NextRunAt.Format("2006-01-02 15:04:05"),
		Status:    task.Status,
	}
	b, _ := json.Marshal(res)
	return string(b), nil
}

type cronJobSummary struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	TaskType    string `json:"task_type"`
	Schedule    string `json:"schedule"`
	Instruction string `json:"instruction"`
	TargetAgent string `json:"target_agent"`
	Status      string `json:"status"`
	NextRunAt   string `json:"next_run_at"`
}

func (t *manageCronTool) executeList(ctx context.Context, a manageCronArgs) (string, error) {
	if a.Status != "" && a.Status != "active" && a.Status != "paused" && a.Status != "running" && a.Status != "completed" {
		return "", fmt.Errorf("%w: invalid status %q", ErrInvalidArgs, a.Status)
	}

	tasks, err := t.cfg.CronStore.ListTasks(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to list cron jobs: %w", err)
	}
	jobs := make([]cronJobSummary, 0, len(tasks))
	for i := range tasks {
		task := &tasks[i]
		if authorizeCronTask(t.cfg.CronScope, task) != nil {
			continue
		}
		if a.Status != "" && task.Status != a.Status {
			continue
		}
		if t.cfg.CronScope.IsGlobal() && a.TargetAgent != "" && !strings.EqualFold(task.TargetAgent, a.TargetAgent) {
			continue
		}
		jobs = append(jobs, cronJobSummary{
			ID:          task.ID,
			Title:       task.Title,
			TaskType:    task.TaskType,
			Schedule:    task.Expression,
			Instruction: task.Instruction,
			TargetAgent: task.TargetAgent,
			Status:      task.Status,
			NextRunAt:   task.NextRunAt.Format(time.DateTime),
		})
	}
	b, _ := json.Marshal(struct {
		Jobs []cronJobSummary `json:"jobs"`
	}{Jobs: jobs})
	return string(b), nil
}

func (t *manageCronTool) executeUpdate(ctx context.Context, a manageCronArgs) (string, error) {
	if err := validateNotZeroLen("task_id", a.TaskID); err != nil {
		return "", err
	}
	if a.Title == "" && a.TaskType == "" && a.Schedule == "" && a.Instruction == "" && a.TargetAgent == "" && a.Status == "" {
		return "", fmt.Errorf("%w: at least one modifiable field must be provided", ErrInvalidArgs)
	}
	if a.TaskType != "" {
		if err := cron.ValidateTaskType(a.TaskType); err != nil {
			return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
		}
	}
	if a.Status != "" && a.Status != "active" && a.Status != "paused" {
		return "", fmt.Errorf("%w: status must be 'active' or 'paused'", ErrInvalidArgs)
	}

	task, err := t.cfg.CronStore.GetTask(ctx, a.TaskID)
	if err != nil {
		return "", fmt.Errorf("failed to find task: %w", err)
	}
	if err := authorizeCronTask(t.cfg.CronScope, task); err != nil {
		return "", err
	}

	changed := false
	statusChanged := false
	if a.Title != "" && a.Title != task.Title {
		task.Title = a.Title
		changed = true
	}
	if a.TaskType != "" && a.TaskType != task.TaskType {
		task.TaskType = a.TaskType
		changed = true
	}
	if a.Schedule != "" && a.Schedule != task.Expression {
		nextRun, err := cron.NextTrigger(a.Schedule, time.Now())
		if err != nil {
			return "", fmt.Errorf("invalid schedule expression: %w", err)
		}
		task.Expression = a.Schedule
		task.NextRunAt = nextRun
		changed = true
	}
	if a.Instruction != "" && a.Instruction != task.Instruction {
		task.Instruction = a.Instruction
		changed = true
	}
	if t.cfg.CronScope.IsGlobal() && a.TargetAgent != "" && a.TargetAgent != task.TargetAgent {
		task.TargetAgent = a.TargetAgent
		changed = true
	}
	if t.cfg.CronScope.IsTeam() {
		task.TargetAgent = t.cfg.CronScope.Owner
	}
	if a.Status != "" && a.Status != task.Status {
		task.Status = a.Status
		statusChanged = true
	}

	if statusChanged && task.Status == "active" && !changed {
		nextRun, err := cron.NextTrigger(task.Expression, time.Now())
		if err != nil {
			return "", fmt.Errorf("invalid schedule expression: %w", err)
		}
		task.NextRunAt = nextRun
	}

	var updateErr error
	if t.cfg.CronScope.IsTeam() {
		updateErr = t.cfg.CronStore.UpdateTaskForTarget(ctx, task, t.cfg.CronScope.Owner)
	} else {
		updateErr = t.cfg.CronStore.UpdateTask(ctx, task)
	}
	if updateErr != nil {
		return "", fmt.Errorf("failed to update task: %w", updateErr)
	}

	if task.Status == "active" {
		t.cfg.CronScheduler.Schedule(*task)
	} else {
		t.cfg.CronScheduler.Unschedule(task.ID)
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "cron: task modified via tool", "task_id", task.ID, "status", task.Status)
	}

	res := struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		TaskType  string `json:"task_type"`
		NextRunAt string `json:"next_run_at"`
		Status    string `json:"status"`
	}{
		ID:        task.ID,
		Title:     task.Title,
		TaskType:  task.TaskType,
		NextRunAt: task.NextRunAt.Format("2006-01-02 15:04:05"),
		Status:    task.Status,
	}
	b, _ := json.Marshal(res)
	return string(b), nil
}

func (t *manageCronTool) executeDelete(ctx context.Context, a manageCronArgs) (string, error) {
	if err := validateNotZeroLen("task_id", a.TaskID); err != nil {
		return "", err
	}

	task, err := t.cfg.CronStore.GetTask(ctx, a.TaskID)
	if err != nil {
		return "", fmt.Errorf("failed to find task: %w", err)
	}
	if err := authorizeCronTask(t.cfg.CronScope, task); err != nil {
		return "", err
	}

	var deleteErr error
	if t.cfg.CronScope.IsTeam() {
		deleteErr = t.cfg.CronStore.DeleteTaskForTarget(ctx, a.TaskID, t.cfg.CronScope.Owner)
	} else {
		deleteErr = t.cfg.CronStore.DeleteTask(ctx, a.TaskID)
	}
	if deleteErr != nil {
		return "", fmt.Errorf("failed to delete task: %w", deleteErr)
	}

	t.cfg.CronScheduler.Unschedule(a.TaskID)

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "cron: task deleted via tool", "task_id", a.TaskID)
	}

	res := struct {
		Deleted string `json:"deleted"`
	}{Deleted: a.TaskID}
	b, _ := json.Marshal(res)
	return string(b), nil
}

var _ Tool = (*manageCronTool)(nil)

// IsCronTool reports whether the given tool name belongs to the cron-job tool family.
func IsCronTool(name string) bool {
	return name == "manage_cron" || name == "create_cron_job" || name == "list_cron_jobs" || name == "update_cron_job" || name == "delete_cron_job"
}
