package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/cron"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

type scheduleTaskTool struct {
	cfg    Config
	logger *logger.Logger
}

func newScheduleTaskTool(cfg Config) *scheduleTaskTool {
	ensureSandbox(&cfg)
	return &scheduleTaskTool{cfg: cfg, logger: cfg.Logger}
}

func (scheduleTaskTool) Name() string { return "create_cron_job" }

func (scheduleTaskTool) Description() string {
	return "Creates a cron job that runs automatically in the future. " +
		"Supports recurring tasks (using standard 5-field cron expression) " +
		"and one-time tasks (using absolute local datetime string like 'YYYY-MM-DD HH:MM:SS' or 'YYYY-MM-DD ...'). " +
		"CRITICAL: You MUST derive the absolute datetime from the timestamp in the latest user message or retrieve the current time/date by executing a shell command (e.g., 'date' or 'Get-Date')."
}

func (t scheduleTaskTool) Parameters() json.RawMessage {
	targetAgent := ""
	if t.cfg.CronScope.IsGlobal() {
		targetAgent = `,
    "target_agent": {
      "type": "string",
      "description": "Optional. The execution agent or team. Defaults to L1."
    }`
	}
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "title": {
      "type": "string",
      "description": "A concise user-facing title that explains what this scheduled task does (maximum 100 characters)."
    },
    "task_type": {
      "type": "string",
      "description": "Required task type: general for ordinary work, engineering for implementation/debugging, or research for investigation and comparison.",
      "enum": ["general", "engineering", "research"]
    },
    "schedule": {
      "type": "string",
      "description": "CRITICAL: Standard 5-field cron expression (e.g. '0 8 * * *' for 8am daily, '0 12 * * 1' for Monday noon) OR a specific absolute local datetime string ('YYYY-MM-DD HH:MM:SS' or 'YYYY-MM-DD HH:MM') derived from the user message timestamp or via shell command execution. Do NOT pass relative terms."
    },
    "instruction": {
      "type": "string",
      "description": "The exact instruction prompt or reminder content to run when triggered."
    }` + targetAgent + `
  },
  "required": ["title", "task_type", "schedule", "instruction"]
}`)
}

type scheduleTaskArgs struct {
	Title       string `json:"title"`
	TaskType    string `json:"task_type"`
	Schedule    string `json:"schedule"`
	Instruction string `json:"instruction"`
	TargetAgent string `json:"target_agent"`
}

type scheduleTaskResult struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	TaskType  string `json:"task_type"`
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

	// Calculate next execution time using system local time
	nextRun, err := cron.NextTrigger(a.Schedule, time.Now())
	if err != nil {
		return "", fmt.Errorf("invalid schedule expression: %w", err)
	}

	// For one-time tasks, check if the target time has already passed by more than 1 minute
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
		Title: a.Title, TaskType: a.TaskType, Expression: a.Schedule,
		Instruction: a.Instruction, TargetAgent: targetAgent, NextRunAt: nextRun,
	})
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
		Title:     task.Title,
		TaskType:  task.TaskType,
		NextRunAt: task.NextRunAt.Format("2006-01-02 15:04:05"),
		Status:    task.Status,
	}
	b, _ := json.Marshal(res)
	return string(b), nil
}

var _ Tool = (*scheduleTaskTool)(nil)

// IsCronTool reports whether the given tool name belongs to the cron-job tool family.
func IsCronTool(name string) bool {
	return name == "create_cron_job" || name == "list_cron_jobs" || name == "update_cron_job" || name == "delete_cron_job"
}
