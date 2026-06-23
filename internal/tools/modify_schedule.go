package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/cron"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

type modifyScheduledTaskTool struct {
	cfg    Config
	logger *logger.Logger
}

func newModifyScheduledTaskTool(cfg Config) *modifyScheduledTaskTool {
	ensureSandbox(&cfg)
	return &modifyScheduledTaskTool{cfg: cfg, logger: cfg.Logger}
}

func (modifyScheduledTaskTool) Name() string { return "modify_scheduled_task" }

func (modifyScheduledTaskTool) Description() string {
	return "Modifies an existing scheduled task. You can update the schedule expression, instruction, target agent, or status (active/paused). " +
		"At least one of expression, instruction, target_agent, or status must be provided. " +
		"The task_id is required — you can obtain it from a previously created task (returned by schedule_task) or by asking the user."
}

func (modifyScheduledTaskTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "task_id": {
      "type": "string",
      "description": "The unique ID of the scheduled task to modify (returned by schedule_task when the task was created)."
    },
    "expression": {
      "type": "string",
      "description": "Optional. New schedule expression: a standard 5-field cron expression (e.g. '0 8 * * *' for 8am daily) or an absolute local datetime string ('YYYY-MM-DD HH:MM:SS' or 'YYYY-MM-DD HH:MM'). Leave empty to keep the current expression."
    },
    "instruction": {
      "type": "string",
      "description": "Optional. New instruction/reminder content for the task. Leave empty to keep the current instruction."
    },
    "target_agent": {
      "type": "string",
      "description": "Optional. New target agent level (e.g. 'L1'). Leave empty to keep the current target."
    },
    "status": {
      "type": "string",
      "description": "Optional. New status: 'active' to enable the task or 'paused' to temporarily disable it. Leave empty to keep the current status.",
      "enum": ["active", "paused"]
    }
  },
  "required": ["task_id"]
}`)
}

type modifyScheduledTaskArgs struct {
	TaskID      string `json:"task_id"`
	Expression  string `json:"expression"`
	Instruction string `json:"instruction"`
	TargetAgent string `json:"target_agent"`
	Status      string `json:"status"`
}

func (t *modifyScheduledTaskTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	if t.cfg.CronStore == nil || t.cfg.CronScheduler == nil {
		return "", fmt.Errorf("scheduled tasks system is not configured/available")
	}

	var a modifyScheduledTaskArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("task_id", a.TaskID); err != nil {
		return "", err
	}

	// At least one modifiable field must be provided.
	if a.Expression == "" && a.Instruction == "" && a.TargetAgent == "" && a.Status == "" {
		return "", fmt.Errorf("%w: at least one of expression, instruction, target_agent, or status must be provided", ErrInvalidArgs)
	}

	// Validate status if provided.
	if a.Status != "" && a.Status != "active" && a.Status != "paused" {
		return "", fmt.Errorf("%w: status must be 'active' or 'paused'", ErrInvalidArgs)
	}

	// Load existing task.
	task, err := t.cfg.CronStore.GetTask(ctx, a.TaskID)
	if err != nil {
		return "", fmt.Errorf("failed to find task: %w", err)
	}

	// Detect changes.
	changed := false
	statusChanged := false

	if a.Expression != "" && a.Expression != task.Expression {
		// Validate the new expression.
		nextRun, err := cron.NextTrigger(a.Expression, time.Now())
		if err != nil {
			return "", fmt.Errorf("invalid schedule expression: %w", err)
		}
		task.Expression = a.Expression
		task.NextRunAt = nextRun
		changed = true
	}
	if a.Instruction != "" && a.Instruction != task.Instruction {
		task.Instruction = a.Instruction
		changed = true
	}
	if a.TargetAgent != "" && a.TargetAgent != task.TargetAgent {
		task.TargetAgent = a.TargetAgent
		changed = true
	}
	if a.Status != "" && a.Status != task.Status {
		task.Status = a.Status
		statusChanged = true
	}

	// Recalculate next run if status changed back to active and expression didn't change.
	if statusChanged && task.Status == "active" && !changed {
		nextRun, err := cron.NextTrigger(task.Expression, time.Now())
		if err != nil {
			return "", fmt.Errorf("invalid schedule expression: %w", err)
		}
		task.NextRunAt = nextRun
	}

	// Update database.
	if err := t.cfg.CronStore.UpdateTask(ctx, task); err != nil {
		return "", fmt.Errorf("failed to update task: %w", err)
	}

	// Dynamically update scheduler.
	if task.Status == "active" {
		t.cfg.CronScheduler.Schedule(*task)
	} else {
		t.cfg.CronScheduler.Unschedule(task.ID)
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "cron: task modified via tool", "task_id", task.ID, "status", task.Status)
	}

	type modifyResult struct {
		ID        string `json:"id"`
		NextRunAt string `json:"next_run_at"`
		Status    string `json:"status"`
	}
	res := modifyResult{
		ID:        task.ID,
		NextRunAt: task.NextRunAt.Format("2006-01-02 15:04:05"),
		Status:    task.Status,
	}
	b, _ := json.Marshal(res)
	return string(b), nil
}

var _ Tool = (*modifyScheduledTaskTool)(nil)
