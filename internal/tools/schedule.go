package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/cron"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

type scheduleTaskTool struct {
	cfg    Config
	logger *logger.Logger
}

func newScheduleTaskTool(cfg Config) *scheduleTaskTool {
	ensureSandbox(&cfg)
	return &scheduleTaskTool{cfg: cfg, logger: cfg.Logger}
}

func (scheduleTaskTool) Name() string { return "schedule_task" }

func (scheduleTaskTool) Description() string {
	return "Schedules a task to run automatically in the future. " +
		"Supports recurring tasks (using standard 5-field cron expression) " +
		"and one-time tasks (using absolute local datetime string like 'YYYY-MM-DD HH:MM:SS' or 'YYYY-MM-DD ...'). " +
		"CRITICAL: You MUST derive the absolute datetime from the timestamp in the latest user message or retrieve the current time/date by executing a shell command (e.g., 'date' or 'Get-Date')."
}

func (scheduleTaskTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "expression": {
      "type": "string",
      "description": "CRITICAL: Standard 5-field cron expression (e.g. '0 8 * * *' for 8am daily, '0 12 * * 1' for Monday noon) OR a specific absolute local datetime string ('YYYY-MM-DD HH:MM:SS' or 'YYYY-MM-DD HH:MM') derived from the user message timestamp or via shell command execution. Do NOT pass relative terms."
    },
    "instruction": {
      "type": "string",
      "description": "The exact instruction prompt or reminder content to run when triggered."
    },
    "target_agent": {
      "type": "string",
      "description": "Optional. The target agent level to handle this instruction (e.g. 'L1'). Default is 'L1'."
    }
  },
  "required": ["expression", "instruction"]
}`)
}

type scheduleTaskArgs struct {
	Expression  string `json:"expression"`
	Instruction string `json:"instruction"`
	TargetAgent string `json:"target_agent"`
}

type scheduleTaskResult struct {
	ID        string `json:"id"`
	NextRunAt string `json:"next_run_at"`
	Status    string `json:"status"`
}

func (t *scheduleTaskTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	if t.cfg.CronStore == nil || t.cfg.CronScheduler == nil {
		return "", fmt.Errorf("scheduled tasks system is not configured/available")
	}

	var a scheduleTaskArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("expression", a.Expression); err != nil {
		return "", err
	}
	if err := validateNotZeroLen("instruction", a.Instruction); err != nil {
		return "", err
	}

	// Calculate next execution time using system local time
	nextRun, err := cron.NextTrigger(a.Expression, time.Now())
	if err != nil {
		return "", fmt.Errorf("invalid schedule expression: %w", err)
	}

	// For one-time tasks, check if the target time has already passed by more than 1 minute
	if cron.IsOneTimeExpression(a.Expression) && nextRun.Before(time.Now().Add(-1*time.Minute)) {
		return "", fmt.Errorf("the scheduled time %s has already passed (current time: %s)",
			nextRun.Format("2006-01-02 15:04:05"),
			time.Now().Format("2006-01-02 15:04:05"))
	}

	task, err := t.cfg.CronStore.CreateTask(ctx, a.Expression, a.Instruction, a.TargetAgent, nextRun)
	if err != nil {
		return "", fmt.Errorf("failed to save task: %w", err)
	}

	// Dynamically register in the background cron scheduler
	t.cfg.CronScheduler.Schedule(*task)

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "cron: task dynamically scheduled via tool", "task_id", task.ID, "next_run", nextRun.Format(time.RFC3339))
	}

	res := scheduleTaskResult{
		ID:        task.ID,
		NextRunAt: task.NextRunAt.Format("2006-01-02 15:04:05"),
		Status:    task.Status,
	}
	b, _ := json.Marshal(res)
	return string(b), nil
}

var _ Tool = (*scheduleTaskTool)(nil)
