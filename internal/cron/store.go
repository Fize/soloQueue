package cron

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
)

// Task is a scheduled/timer task persisted in SQLite.
type Task struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	TaskType    string     `json:"task_type"`
	Expression  string     `json:"expression"`
	Instruction string     `json:"instruction"`
	TargetAgent string     `json:"target_agent"`
	Status      string     `json:"status"` // 'active' | 'paused' | 'completed'
	LastRunAt   *time.Time `json:"last_run_at,omitempty"`
	NextRunAt   time.Time  `json:"next_run_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

const (
	TaskTypeGeneral     = "general"
	TaskTypeEngineering = "engineering"
	TaskTypeResearch    = "research"
)

// CreateTaskInput contains the required and optional fields for a new task.
type CreateTaskInput struct {
	Title       string
	TaskType    string
	Expression  string
	Instruction string
	TargetAgent string
	NextRunAt   time.Time
}

// ValidateTaskType validates the persisted task-type enum.
func ValidateTaskType(taskType string) error {
	switch taskType {
	case TaskTypeGeneral, TaskTypeEngineering, TaskTypeResearch:
		return nil
	default:
		return fmt.Errorf("task_type must be one of general, engineering, or research")
	}
}

// ValidateTaskTitle validates the required user-facing title.
func ValidateTaskTitle(title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("title is required")
	}
	if len([]rune(title)) > 100 {
		return fmt.Errorf("title must be at most 100 characters")
	}
	return nil
}

func validateTaskFields(title, taskType, expression, instruction string) error {
	if err := ValidateTaskTitle(title); err != nil {
		return err
	}
	if err := ValidateTaskType(taskType); err != nil {
		return err
	}
	if strings.TrimSpace(expression) == "" {
		return fmt.Errorf("expression is required")
	}
	if strings.TrimSpace(instruction) == "" {
		return fmt.Errorf("instruction is required")
	}
	return nil
}

// IsOneTime returns true if the expression represents a specific datetime.
func (t *Task) IsOneTime() bool {
	return IsOneTimeExpression(t.Expression)
}

// ExecutionRecord is a single execution history entry for a scheduled task.
// The full execution trace is stored in a timeline file at TimelineDir.
type ExecutionRecord struct {
	ID            string    `json:"id"`
	TaskID        string    `json:"task_id"`
	ExecutedAt    time.Time `json:"executed_at"`
	CompletedAt   time.Time `json:"completed_at"`
	DurationMs    int64     `json:"duration_ms"`
	Status        string    `json:"status"` // 'success' | 'failed' | 'panic'
	ResultSummary string    `json:"result_summary"`
	ErrorMessage  string    `json:"error_message"`
	TaskType      string    `json:"task_type"`
	TargetAgent   string    `json:"target_agent"`
	ModelID       string    `json:"model_id"`
	ProviderID    string    `json:"provider_id"`
	TimelineDir   string    `json:"timeline_dir"`
}

const maxSummaryLen = 500

// DBStore manages persistent scheduled tasks in the shared SQLite database.
type DBStore struct {
	db      *sql.DB
	mu      *sync.Mutex
	workDir string // base directory for cron log cleanup on task deletion
	logf    func(format string, args ...any)
}

// NewDBStore creates a DBStore from a shared DB reference.
func NewDBStore(db *sqlitedb.DB) *DBStore {
	return &DBStore{
		db: db.DB,
		mu: &db.WMu,
	}
}

// SetWorkDir configures the base directory for cron log cleanup on task deletion.
func (s *DBStore) SetWorkDir(dir string) {
	s.workDir = dir
}

// SetLogf configures a log callback for warnings (e.g. cleanup failures).
func (s *DBStore) SetLogf(fn func(format string, args ...any)) {
	s.logf = fn
}

// ListTasks returns all scheduled tasks.
func (s *DBStore) ListTasks(ctx context.Context) ([]Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, task_type, expression, instruction, target_agent, status, last_run_at, next_run_at, created_at, updated_at
		 FROM scheduled_tasks ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("cron store: list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		var lRun sql.NullString
		var nRun, cAt, uAt string
		err := rows.Scan(&t.ID, &t.Title, &t.TaskType, &t.Expression, &t.Instruction, &t.TargetAgent, &t.Status, &lRun, &nRun, &cAt, &uAt)
		if err != nil {
			return nil, fmt.Errorf("cron store: scan task: %w", err)
		}

		if lRun.Valid && lRun.String != "" {
			parsed, _ := time.ParseInLocation(time.RFC3339, lRun.String, time.Local)
			t.LastRunAt = &parsed
		}
		t.NextRunAt, _ = time.ParseInLocation(time.RFC3339, nRun, time.Local)
		t.CreatedAt, _ = time.ParseInLocation(time.RFC3339, cAt, time.Local)
		t.UpdatedAt, _ = time.ParseInLocation(time.RFC3339, uAt, time.Local)

		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// GetTask retrieves a single scheduled task.
func (s *DBStore) GetTask(ctx context.Context, id string) (*Task, error) {
	var t Task
	var lRun sql.NullString
	var nRun, cAt, uAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, title, task_type, expression, instruction, target_agent, status, last_run_at, next_run_at, created_at, updated_at
		 FROM scheduled_tasks WHERE id = ?`, id).
		Scan(&t.ID, &t.Title, &t.TaskType, &t.Expression, &t.Instruction, &t.TargetAgent, &t.Status, &lRun, &nRun, &cAt, &uAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("cron store: task %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("cron store: get task: %w", err)
	}

	if lRun.Valid && lRun.String != "" {
		parsed, _ := time.ParseInLocation(time.RFC3339, lRun.String, time.Local)
		t.LastRunAt = &parsed
	}
	t.NextRunAt, _ = time.ParseInLocation(time.RFC3339, nRun, time.Local)
	t.CreatedAt, _ = time.ParseInLocation(time.RFC3339, cAt, time.Local)
	t.UpdatedAt, _ = time.ParseInLocation(time.RFC3339, uAt, time.Local)

	return &t, nil
}

// GetActiveTasks returns all tasks with 'active' status.
func (s *DBStore) GetActiveTasks(ctx context.Context) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, task_type, expression, instruction, target_agent, status, last_run_at, next_run_at, created_at, updated_at

		 FROM scheduled_tasks WHERE status = 'active'`)
	if err != nil {
		return nil, fmt.Errorf("cron store: get active tasks: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		var lRun sql.NullString
		var nRun, cAt, uAt string
		err := rows.Scan(&t.ID, &t.Title, &t.TaskType, &t.Expression, &t.Instruction, &t.TargetAgent, &t.Status, &lRun, &nRun, &cAt, &uAt)
		if err != nil {
			return nil, fmt.Errorf("cron store: scan active task: %w", err)
		}

		if lRun.Valid && lRun.String != "" {
			parsed, _ := time.ParseInLocation(time.RFC3339, lRun.String, time.Local)
			t.LastRunAt = &parsed
		}
		t.NextRunAt, _ = time.ParseInLocation(time.RFC3339, nRun, time.Local)
		t.CreatedAt, _ = time.ParseInLocation(time.RFC3339, cAt, time.Local)
		t.UpdatedAt, _ = time.ParseInLocation(time.RFC3339, uAt, time.Local)

		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// CreateTask inserts a new task.
func (s *DBStore) CreateTask(ctx context.Context, input CreateTaskInput) (*Task, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Expression = strings.TrimSpace(input.Expression)
	input.Instruction = strings.TrimSpace(input.Instruction)
	input.TargetAgent = strings.TrimSpace(input.TargetAgent)
	if err := validateTaskFields(input.Title, input.TaskType, input.Expression, input.Instruction); err != nil {
		return nil, fmt.Errorf("cron store: invalid task: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	id := uuid.New().String()
	now := time.Now().Format(time.RFC3339)
	nRun := input.NextRunAt.Format(time.RFC3339)

	if input.TargetAgent == "" {
		input.TargetAgent = "L1"
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO scheduled_tasks (id, title, task_type, expression, instruction, target_agent, status, next_run_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, 'active', ?, ?, ?)`,
		id, input.Title, input.TaskType, input.Expression, input.Instruction, input.TargetAgent, nRun, now, now)
	if err != nil {
		return nil, fmt.Errorf("cron store: create task: %w", err)
	}

	return &Task{
		ID:          id,
		Title:       input.Title,
		TaskType:    input.TaskType,
		Expression:  input.Expression,
		Instruction: input.Instruction,
		TargetAgent: input.TargetAgent,
		Status:      "active",
		NextRunAt:   input.NextRunAt,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}, nil
}

// UpdateTask updates all editable task fields.
func (s *DBStore) UpdateTask(ctx context.Context, t *Task) error {
	return s.updateTask(ctx, t, "")
}

// UpdateTaskForTarget updates a task only if it still belongs to targetAgent.
// This keeps team-scoped agent authorization race-safe at the database boundary.
func (s *DBStore) UpdateTaskForTarget(ctx context.Context, t *Task, targetAgent string) error {
	if strings.TrimSpace(targetAgent) == "" {
		return fmt.Errorf("cron store: target agent is required")
	}
	return s.updateTask(ctx, t, targetAgent)
}

func (s *DBStore) updateTask(ctx context.Context, t *Task, requiredTarget string) error {
	t.Title = strings.TrimSpace(t.Title)
	t.Expression = strings.TrimSpace(t.Expression)
	t.Instruction = strings.TrimSpace(t.Instruction)
	t.TargetAgent = strings.TrimSpace(t.TargetAgent)
	if t.TargetAgent == "" {
		t.TargetAgent = "L1"
	}
	if err := validateTaskFields(t.Title, t.TaskType, t.Expression, t.Instruction); err != nil {
		return fmt.Errorf("cron store: invalid task: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Format(time.RFC3339)
	nRun := t.NextRunAt.Format(time.RFC3339)

	query := `UPDATE scheduled_tasks SET title = ?, task_type = ?, expression = ?, instruction = ?, target_agent = ?, status = ?, next_run_at = ?, updated_at = ? WHERE id = ?`
	args := []any{t.Title, t.TaskType, t.Expression, t.Instruction, t.TargetAgent, t.Status, nRun, now, t.ID}
	if requiredTarget != "" {
		query += ` AND lower(target_agent) = lower(?)`
		args = append(args, requiredTarget)
	}
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("cron store: update task: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("cron store: task %q not found in target scope", t.ID)
	}
	t.UpdatedAt = time.Now()
	return nil
}

// UpdateTaskStatus changes status ('active', 'paused', 'completed').
func (s *DBStore) UpdateTaskStatus(ctx context.Context, id, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE scheduled_tasks SET status = ?, updated_at = ? WHERE id = ?`,
		status, now, id)
	if err != nil {
		return fmt.Errorf("cron store: update status: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("cron store: task %q not found", id)
	}
	return nil
}

// UpdateNextRun updates timestamps after execution.
func (s *DBStore) UpdateNextRun(ctx context.Context, id string, lastRun time.Time, nextRun time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Format(time.RFC3339)
	lRun := lastRun.Format(time.RFC3339)
	nRun := nextRun.Format(time.RFC3339)

	_, err := s.db.ExecContext(ctx,
		`UPDATE scheduled_tasks SET status = 'active', last_run_at = ?, next_run_at = ?, updated_at = ? WHERE id = ?`,
		lRun, nRun, now, id)
	if err != nil {
		return fmt.Errorf("cron store: update next run: %w", err)
	}
	return nil
}

// ClaimTask atomically claims a task for execution by transitioning it from
// 'active' to 'running'. Returns true if the claim succeeded, false if another
// instance already claimed it (rows affected == 0).
func (s *DBStore) ClaimTask(ctx context.Context, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE scheduled_tasks SET status = 'running', updated_at = ? WHERE status = 'active' AND id = ?`,
		now, id)
	if err != nil {
		return false, fmt.Errorf("cron store: claim task: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ResetStaleRunning resets any tasks stuck in 'running' status that were last
// updated before the given time back to 'active'. This handles crash recovery:
// if the process crashes while executing a task, the task remains in 'running'
// state and gets reset on the next Start().
func (s *DBStore) ResetStaleRunning(ctx context.Context, beforeTime time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Format(time.RFC3339)
	before := beforeTime.Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE scheduled_tasks SET status = 'active', updated_at = ? WHERE status = 'running' AND updated_at < ?`,
		now, before)
	if err != nil {
		return 0, fmt.Errorf("cron store: reset stale running: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// MarkCompleted sets status of one-time tasks to completed.
func (s *DBStore) MarkCompleted(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Format(time.RFC3339)
	lRun := time.Now().Format(time.RFC3339)

	_, err := s.db.ExecContext(ctx,
		`UPDATE scheduled_tasks SET status = 'completed', last_run_at = ?, updated_at = ? WHERE id = ?`,
		lRun, now, id)
	if err != nil {
		return fmt.Errorf("cron store: mark completed: %w", err)
	}
	return nil
}

// DeleteTask removes task from DB.
func (s *DBStore) DeleteTask(ctx context.Context, id string) error {
	return s.deleteTask(ctx, id, "")
}

// DeleteTaskForTarget deletes a task only if it belongs to targetAgent.
func (s *DBStore) DeleteTaskForTarget(ctx context.Context, id, targetAgent string) error {
	if strings.TrimSpace(targetAgent) == "" {
		return fmt.Errorf("cron store: target agent is required")
	}
	return s.deleteTask(ctx, id, targetAgent)
}

func (s *DBStore) deleteTask(ctx context.Context, id, requiredTarget string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `DELETE FROM scheduled_tasks WHERE id = ?`
	args := []any{id}
	if requiredTarget != "" {
		query += ` AND lower(target_agent) = lower(?)`
		args = append(args, requiredTarget)
	}
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("cron store: delete task: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("cron store: task %q not found", id)
	}

	// Cascade-delete execution history.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM cron_execution_history WHERE task_id = ?`, id); err != nil {
		return fmt.Errorf("cron store: delete task history: %w", err)
	}

	// Clean up cron log files on disk.
	if s.workDir != "" {
		dir := filepath.Join(s.workDir, "logs", "cron", id)
		if err := os.RemoveAll(dir); err != nil && s.logf != nil {
			s.logf("cron store: failed to remove task log dir %s: %v", dir, err)
		}
	}

	return nil
}

// ─── Execution History CRUD ──────────────────────────────────────────────────

// RecordExecution inserts a new execution history entry.
func (s *DBStore) RecordExecution(ctx context.Context, rec ExecutionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	eAt := rec.ExecutedAt.Format(time.RFC3339)
	cAt := rec.CompletedAt.Format(time.RFC3339)
	summary := rec.ResultSummary
	if len(summary) > maxSummaryLen {
		summary = summary[:maxSummaryLen]
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO cron_execution_history (id, task_id, executed_at, completed_at, duration_ms, status, result_summary, error_message, task_type, target_agent, model_id, provider_id, timeline_dir)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.TaskID, eAt, cAt, rec.DurationMs, rec.Status, summary,
		rec.ErrorMessage, rec.TaskType, rec.TargetAgent, rec.ModelID, rec.ProviderID, rec.TimelineDir)
	if err != nil {
		return fmt.Errorf("cron store: record execution: %w", err)
	}
	return nil
}

// ListExecutionHistory returns execution history for a task, newest first.
func (s *DBStore) ListExecutionHistory(ctx context.Context, taskID string, limit, offset int) ([]ExecutionRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, task_id, executed_at, completed_at, duration_ms, status, result_summary, error_message, task_type, target_agent, model_id, provider_id, timeline_dir
		 FROM cron_execution_history WHERE task_id = ?
		 ORDER BY executed_at DESC LIMIT ? OFFSET ?`, taskID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("cron store: list execution history: %w", err)
	}
	defer rows.Close()

	var records []ExecutionRecord
	for rows.Next() {
		var r ExecutionRecord
		var eAt, cAt string
		err := rows.Scan(&r.ID, &r.TaskID, &eAt, &cAt, &r.DurationMs, &r.Status, &r.ResultSummary, &r.ErrorMessage, &r.TaskType, &r.TargetAgent, &r.ModelID, &r.ProviderID, &r.TimelineDir)
		if err != nil {
			return nil, fmt.Errorf("cron store: scan execution record: %w", err)
		}
		r.ExecutedAt, _ = time.ParseInLocation(time.RFC3339, eAt, time.Local)
		r.CompletedAt, _ = time.ParseInLocation(time.RFC3339, cAt, time.Local)
		records = append(records, r)
	}
	return records, rows.Err()
}

// GetExecutionHistory returns a single execution record by ID (scoped to taskID).
func (s *DBStore) GetExecutionHistory(ctx context.Context, taskID, execID string) (*ExecutionRecord, error) {
	var r ExecutionRecord
	var eAt, cAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, task_id, executed_at, completed_at, duration_ms, status, result_summary, error_message, task_type, target_agent, model_id, provider_id, timeline_dir
		 FROM cron_execution_history WHERE task_id = ? AND id = ?`, taskID, execID).
		Scan(&r.ID, &r.TaskID, &eAt, &cAt, &r.DurationMs, &r.Status, &r.ResultSummary, &r.ErrorMessage, &r.TaskType, &r.TargetAgent, &r.ModelID, &r.ProviderID, &r.TimelineDir)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("cron store: execution record %q not found", execID)
	}
	if err != nil {
		return nil, fmt.Errorf("cron store: get execution record: %w", err)
	}
	r.ExecutedAt, _ = time.ParseInLocation(time.RFC3339, eAt, time.Local)
	r.CompletedAt, _ = time.ParseInLocation(time.RFC3339, cAt, time.Local)
	return &r, nil
}

// DeleteExecutionHistory deletes all execution history for a task.
func (s *DBStore) DeleteExecutionHistory(ctx context.Context, taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx, `DELETE FROM cron_execution_history WHERE task_id = ?`, taskID)
	if err != nil {
		return fmt.Errorf("cron store: delete execution history: %w", err)
	}
	return nil
}

// PruneExecutionHistory keeps only the most recent keepN records for a task.
func (s *DBStore) PruneExecutionHistory(ctx context.Context, taskID string, keepN int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if keepN <= 0 {
		return 0, nil
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM cron_execution_history WHERE task_id = ? AND id NOT IN (
			 SELECT id FROM cron_execution_history WHERE task_id = ?
			 ORDER BY executed_at DESC LIMIT ?)`, taskID, taskID, keepN)
	if err != nil {
		return 0, fmt.Errorf("cron store: prune execution history: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
