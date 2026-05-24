package cron

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
)

// Task is a scheduled/timer task persisted in SQLite.
type Task struct {
	ID          string     `json:"id"`
	Expression  string     `json:"expression"`
	Instruction string     `json:"instruction"`
	TargetAgent string     `json:"target_agent"`
	Status      string     `json:"status"` // 'active' | 'paused' | 'completed'
	LastRunAt   *time.Time `json:"last_run_at,omitempty"`
	NextRunAt   time.Time  `json:"next_run_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// IsOneTime returns true if the expression represents a specific datetime.
func (t *Task) IsOneTime() bool {
	return IsOneTimeExpression(t.Expression)
}

// DBStore manages persistent scheduled tasks in the shared SQLite database.
type DBStore struct {
	db *sql.DB
	mu *sync.Mutex
}

// NewDBStore creates a DBStore from a shared DB reference.
func NewDBStore(db *sqlitedb.DB) *DBStore {
	return &DBStore{
		db: db.DB,
		mu: &db.WMu,
	}
}

// ListTasks returns all scheduled tasks.
func (s *DBStore) ListTasks(ctx context.Context) ([]Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, expression, instruction, target_agent, status, last_run_at, next_run_at, created_at, updated_at
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
		err := rows.Scan(&t.ID, &t.Expression, &t.Instruction, &t.TargetAgent, &t.Status, &lRun, &nRun, &cAt, &uAt)
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
		`SELECT id, expression, instruction, target_agent, status, last_run_at, next_run_at, created_at, updated_at
		 FROM scheduled_tasks WHERE id = ?`, id).
		Scan(&t.ID, &t.Expression, &t.Instruction, &t.TargetAgent, &t.Status, &lRun, &nRun, &cAt, &uAt)
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
		`SELECT id, expression, instruction, target_agent, status, last_run_at, next_run_at, created_at, updated_at
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
		err := rows.Scan(&t.ID, &t.Expression, &t.Instruction, &t.TargetAgent, &t.Status, &lRun, &nRun, &cAt, &uAt)
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
func (s *DBStore) CreateTask(ctx context.Context, expression, instruction, targetAgent string, nextRun time.Time) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := uuid.New().String()
	now := time.Now().Format(time.RFC3339)
	nRun := nextRun.Format(time.RFC3339)

	if targetAgent == "" {
		targetAgent = "L1"
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO scheduled_tasks (id, expression, instruction, target_agent, status, next_run_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'active', ?, ?, ?)`,
		id, expression, instruction, targetAgent, nRun, now, now)
	if err != nil {
		return nil, fmt.Errorf("cron store: create task: %w", err)
	}

	return &Task{
		ID:          id,
		Expression:  expression,
		Instruction: instruction,
		TargetAgent: targetAgent,
		Status:      "active",
		NextRunAt:   nextRun,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}, nil
}

// UpdateTask updates editable fields of a task (expression, instruction, target_agent, status, next_run_at).
func (s *DBStore) UpdateTask(ctx context.Context, t *Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Format(time.RFC3339)
	nRun := t.NextRunAt.Format(time.RFC3339)

	_, err := s.db.ExecContext(ctx,
		`UPDATE scheduled_tasks SET expression = ?, instruction = ?, target_agent = ?, status = ?, next_run_at = ?, updated_at = ? WHERE id = ?`,
		t.Expression, t.Instruction, t.TargetAgent, t.Status, nRun, now, t.ID)
	if err != nil {
		return fmt.Errorf("cron store: update task: %w", err)
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
		`UPDATE scheduled_tasks SET last_run_at = ?, next_run_at = ?, updated_at = ? WHERE id = ?`,
		lRun, nRun, now, id)
	if err != nil {
		return fmt.Errorf("cron store: update next run: %w", err)
	}
	return nil
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
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.ExecContext(ctx, `DELETE FROM scheduled_tasks WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("cron store: delete task: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("cron store: task %q not found", id)
	}
	return nil
}
