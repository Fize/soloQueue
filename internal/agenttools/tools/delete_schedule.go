package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

type deleteScheduledTaskTool struct {
	cfg    Config
	logger *logger.Logger
}

func newDeleteScheduledTaskTool(cfg Config) *deleteScheduledTaskTool {
	ensureSandbox(&cfg)
	return &deleteScheduledTaskTool{cfg: cfg, logger: cfg.Logger}
}

func (deleteScheduledTaskTool) Name() string { return "delete_cron_job" }

func (deleteScheduledTaskTool) Description() string {
	return "Deletes an existing cron job permanently. " +
		"The task will be unscheduled immediately and removed from the database. " +
		"Use list_cron_jobs first when the job ID is unknown. " +
		"This action cannot be undone."
}

func (deleteScheduledTaskTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "task_id": {
      "type": "string",
		"description": "The unique ID of the cron job to delete."
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

	// Remove the in-memory entry only after the scoped database delete succeeds.
	t.cfg.CronScheduler.Unschedule(a.TaskID)

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
