package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type listCronJobsTool struct {
	cfg Config
}

func newListCronJobsTool(cfg Config) *listCronJobsTool {
	ensureSandbox(&cfg)
	return &listCronJobsTool{cfg: cfg}
}

func (listCronJobsTool) Name() string { return "list_cron_jobs" }

func (listCronJobsTool) Description() string {
	return "Lists cron jobs visible to this agent. Use it to find job IDs before updating or deleting jobs."
}

func (t listCronJobsTool) Parameters() json.RawMessage {
	targetAgent := ""
	if t.cfg.CronScope.IsGlobal() {
		targetAgent = `,
    "target_agent": {
      "type": "string",
      "description": "Optional. Return only jobs assigned to this agent or team."
    }`
	}
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "status": {
      "type": "string",
      "description": "Optional job status filter.",
      "enum": ["active", "paused", "running", "completed"]
    }` + targetAgent + `
  }
}`)
}

type listCronJobsArgs struct {
	Status      string `json:"status"`
	TargetAgent string `json:"target_agent"`
}

type cronJobSummary struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	TaskLevel   string `json:"task_level"`
	Schedule    string `json:"schedule"`
	Instruction string `json:"instruction"`
	TargetAgent string `json:"target_agent"`
	Status      string `json:"status"`
	NextRunAt   string `json:"next_run_at"`
}

func (t *listCronJobsTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}
	if t.cfg.CronStore == nil {
		return "", fmt.Errorf("cron jobs system is not configured/available")
	}

	var a listCronJobsArgs
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &a); err != nil {
			return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
		}
	}
	if a.Status != "" && a.Status != "active" && a.Status != "paused" && a.Status != "running" && a.Status != "completed" {
		return "", fmt.Errorf("%w: invalid status", ErrInvalidArgs)
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
			ID: task.ID, Title: task.Title, TaskLevel: task.TaskLevel,
			Schedule: task.Expression, Instruction: task.Instruction,
			TargetAgent: task.TargetAgent, Status: task.Status,
			NextRunAt: task.NextRunAt.Format(time.DateTime),
		})
	}
	b, _ := json.Marshal(struct {
		Jobs []cronJobSummary `json:"jobs"`
	}{Jobs: jobs})
	return string(b), nil
}

var _ Tool = (*listCronJobsTool)(nil)
