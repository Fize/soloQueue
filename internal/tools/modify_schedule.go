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

func (modifyScheduledTaskTool) Name() string { return "update_cron_job" }

func (modifyScheduledTaskTool) Description() string {
	return "Updates an existing cron job. You can update the title, task level, schedule, instruction, target agent, or status (active/paused). " +
		"At least one modifiable field must be provided. " +
		"Use list_cron_jobs first when the job ID is unknown."
}

func (t modifyScheduledTaskTool) Parameters() json.RawMessage {
	targetAgent := ""
	if t.cfg.CronScope.IsGlobal() {
		targetAgent = `,
    "target_agent": {
      "type": "string",
      "description": "Optional. New execution agent or team."
    }`
	}
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "task_id": {
      "type": "string",
      "description": "The unique ID of the cron job to update."
    },
    "title": {
      "type": "string",
      "description": "Optional. New concise user-facing task title (maximum 100 characters)."
    },
    "task_level": {
      "type": "string",
      "description": "Optional. New task complexity level.",
      "enum": ["L0", "L1", "L2", "L3", "L4"]
    },
    "schedule": {
      "type": "string",
      "description": "Optional. New schedule expression: a standard 5-field cron expression (e.g. '0 8 * * *' for 8am daily) or an absolute local datetime string ('YYYY-MM-DD HH:MM:SS' or 'YYYY-MM-DD HH:MM'). Leave empty to keep the current expression."
    },
    "instruction": {
      "type": "string",
      "description": "Optional. New instruction/reminder content for the task. Leave empty to keep the current instruction."
    }` + targetAgent + `,
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
	Title       string `json:"title"`
	TaskLevel   string `json:"task_level"`
	Schedule    string `json:"schedule"`
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
	if a.Title == "" && a.TaskLevel == "" && a.Schedule == "" && a.Instruction == "" && a.TargetAgent == "" && a.Status == "" {
		return "", fmt.Errorf("%w: at least one modifiable field must be provided", ErrInvalidArgs)
	}
	if a.TaskLevel != "" {
		if err := cron.ValidateTaskLevel(a.TaskLevel); err != nil {
			return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
		}
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
	if err := authorizeCronTask(t.cfg.CronScope, task); err != nil {
		return "", err
	}

	// Detect changes.
	changed := false
	statusChanged := false
	if a.Title != "" && a.Title != task.Title {
		task.Title = a.Title
		changed = true
	}
	if a.TaskLevel != "" && a.TaskLevel != task.TaskLevel {
		task.TaskLevel = a.TaskLevel
		changed = true
	}

	if a.Schedule != "" && a.Schedule != task.Expression {
		// Validate the new expression.
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

	// Recalculate next run if status changed back to active and expression didn't change.
	if statusChanged && task.Status == "active" && !changed {
		nextRun, err := cron.NextTrigger(task.Expression, time.Now())
		if err != nil {
			return "", fmt.Errorf("invalid schedule expression: %w", err)
		}
		task.NextRunAt = nextRun
	}

	// Update database.
	var updateErr error
	if t.cfg.CronScope.IsTeam() {
		updateErr = t.cfg.CronStore.UpdateTaskForTarget(ctx, task, t.cfg.CronScope.Owner)
	} else {
		updateErr = t.cfg.CronStore.UpdateTask(ctx, task)
	}
	if updateErr != nil {
		return "", fmt.Errorf("failed to update task: %w", updateErr)
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
		Title     string `json:"title"`
		TaskLevel string `json:"task_level"`
		NextRunAt string `json:"next_run_at"`
		Status    string `json:"status"`
	}
	res := modifyResult{
		ID:        task.ID,
		Title:     task.Title,
		TaskLevel: task.TaskLevel,
		NextRunAt: task.NextRunAt.Format("2006-01-02 15:04:05"),
		Status:    task.Status,
	}
	b, _ := json.Marshal(res)
	return string(b), nil
}

var _ Tool = (*modifyScheduledTaskTool)(nil)
