package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

type deleteScheduledTaskTool struct {
	cfg    Config
	logger *logger.Logger
}

func newDeleteScheduledTaskTool(cfg Config) *deleteScheduledTaskTool {
	ensureSandbox(&cfg)
	return &deleteScheduledTaskTool{cfg: cfg, logger: cfg.Logger}
}

func (deleteScheduledTaskTool) Name() string { return "delete_scheduled_task" }

func (deleteScheduledTaskTool) Description() string {
	return "Deletes an existing scheduled task permanently. " +
		"The task will be unscheduled immediately and removed from the database. " +
		"The task_id is required — you can obtain it from a previously created task (returned by schedule_task) or by asking the user. " +
		"This action cannot be undone."
}

func (deleteScheduledTaskTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "task_id": {
      "type": "string",
      "description": "The unique ID of the scheduled task to delete (returned by schedule_task when the task was created)."
    }
  },
  "required": ["task_id"]
}`)
}

type deleteScheduledTaskArgs struct {
	TaskID string `json:"task_id"`
}

func (t *deleteScheduledTaskTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	if t.cfg.CronStore == nil || t.cfg.CronScheduler == nil {
		return "", fmt.Errorf("scheduled tasks system is not configured/available")
	}

	var a deleteScheduledTaskArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("task_id", a.TaskID); err != nil {
		return "", err
	}

	// Unschedule first so the task won't fire during deletion.
	t.cfg.CronScheduler.Unschedule(a.TaskID)

	// Delete from database.
	if err := t.cfg.CronStore.DeleteTask(ctx, a.TaskID); err != nil {
		return "", fmt.Errorf("failed to delete task: %w", err)
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "cron: task deleted via tool", "task_id", a.TaskID)
	}

	type deleteResult struct {
		Deleted string `json:"deleted"`
	}
	res := deleteResult{Deleted: a.TaskID}
	b, _ := json.Marshal(res)
	return string(b), nil
}

var _ Tool = (*deleteScheduledTaskTool)(nil)
