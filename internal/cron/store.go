package cron

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
)

// Task is a scheduled/timer task persisted in SQLite.
type Task struct {
	ID             string     `json:"id"`
	Expression     string     `json:"expression"`
	Instruction    string     `json:"instruction"`
	TargetAgent    string     `json:"target_agent"`
	Status         string     `json:"status"` // 'active' | 'paused' | 'completed'
	LastRunAt      *time.Time `json:"last_run_at,omitempty"`
	NextRunAt      time.Time  `json:"next_run_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	QQSource       int        `json:"qq_source"`
	QQOpenID       string     `json:"qq_openid"`
	QQTargetOpenID string     `json:"qq_target_openid"`
	QQChatID       string     `json:"qq_chat_id"`
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
		`SELECT id, expression, instruction, target_agent, status, last_run_at, next_run_at, created_at, updated_at, qq_source, qq_openid, qq_target_openid, qq_chat_id
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
		var qqSource sql.NullInt64
		var qqOpenID, qqTargetOpenID, qqChatID sql.NullString
		err := rows.Scan(&t.ID, &t.Expression, &t.Instruction, &t.TargetAgent, &t.Status, &lRun, &nRun, &cAt, &uAt, &qqSource, &qqOpenID, &qqTargetOpenID, &qqChatID)
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
		
		if qqSource.Valid {
			t.QQSource = int(qqSource.Int64)
		} else {
			t.QQSource = -1
		}
		if qqOpenID.Valid {
			t.QQOpenID = qqOpenID.String
		}
		if qqTargetOpenID.Valid {
			t.QQTargetOpenID = qqTargetOpenID.String
		}
		if qqChatID.Valid {
			t.QQChatID = qqChatID.String
		}

		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// GetTask retrieves a single scheduled task.
func (s *DBStore) GetTask(ctx context.Context, id string) (*Task, error) {
	var t Task
	var lRun sql.NullString
	var nRun, cAt, uAt string
	var qqSource sql.NullInt64
	var qqOpenID, qqTargetOpenID, qqChatID sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, expression, instruction, target_agent, status, last_run_at, next_run_at, created_at, updated_at, qq_source, qq_openid, qq_target_openid, qq_chat_id
		 FROM scheduled_tasks WHERE id = ?`, id).
		Scan(&t.ID, &t.Expression, &t.Instruction, &t.TargetAgent, &t.Status, &lRun, &nRun, &cAt, &uAt, &qqSource, &qqOpenID, &qqTargetOpenID, &qqChatID)
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

	if qqSource.Valid {
		t.QQSource = int(qqSource.Int64)
	} else {
		t.QQSource = -1
	}
	if qqOpenID.Valid {
		t.QQOpenID = qqOpenID.String
	}
	if qqTargetOpenID.Valid {
		t.QQTargetOpenID = qqTargetOpenID.String
	}
	if qqChatID.Valid {
		t.QQChatID = qqChatID.String
	}

	return &t, nil
}

// GetActiveTasks returns all tasks with 'active' status.
func (s *DBStore) GetActiveTasks(ctx context.Context) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, expression, instruction, target_agent, status, last_run_at, next_run_at, created_at, updated_at, qq_source, qq_openid, qq_target_openid, qq_chat_id
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
		var qqSource sql.NullInt64
		var qqOpenID, qqTargetOpenID, qqChatID sql.NullString
		err := rows.Scan(&t.ID, &t.Expression, &t.Instruction, &t.TargetAgent, &t.Status, &lRun, &nRun, &cAt, &uAt, &qqSource, &qqOpenID, &qqTargetOpenID, &qqChatID)
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

		if qqSource.Valid {
			t.QQSource = int(qqSource.Int64)
		} else {
			t.QQSource = -1
		}
		if qqOpenID.Valid {
			t.QQOpenID = qqOpenID.String
		}
		if qqTargetOpenID.Valid {
			t.QQTargetOpenID = qqTargetOpenID.String
		}
		if qqChatID.Valid {
			t.QQChatID = qqChatID.String
		}

		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func getQQMessageMeta(ctx context.Context) (source int, openID, targetOpenID, chatID string, exists bool) {
	val := ctx.Value("qq_message")
	if val == nil {
		return -1, "", "", "", false
	}
	v := reflect.ValueOf(val)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return -1, "", "", "", false
	}

	fSource := v.FieldByName("Source")
	fOpenID := v.FieldByName("OpenID")
	fTargetOpenID := v.FieldByName("TargetOpenID")
	fChatID := v.FieldByName("ChatID")

	source = -1
	if fSource.IsValid() && (fSource.Kind() == reflect.Int || fSource.Kind() == reflect.Int64 || fSource.Kind() == reflect.Int32) {
		source = int(fSource.Int())
	}
	if fOpenID.IsValid() && fOpenID.Kind() == reflect.String {
		openID = fOpenID.String()
	}
	if fTargetOpenID.IsValid() && fTargetOpenID.Kind() == reflect.String {
		targetOpenID = fTargetOpenID.String()
	}
	if fChatID.IsValid() && fChatID.Kind() == reflect.String {
		chatID = fChatID.String()
	}
	return source, openID, targetOpenID, chatID, true
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

	qqSource, qqOpenID, qqTargetOpenID, qqChatID, _ := getQQMessageMeta(ctx)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO scheduled_tasks (id, expression, instruction, target_agent, status, next_run_at, created_at, updated_at, qq_source, qq_openid, qq_target_openid, qq_chat_id)
		 VALUES (?, ?, ?, ?, 'active', ?, ?, ?, ?, ?, ?, ?)`,
		id, expression, instruction, targetAgent, nRun, now, now, qqSource, qqOpenID, qqTargetOpenID, qqChatID)
	if err != nil {
		return nil, fmt.Errorf("cron store: create task: %w", err)
	}

	return &Task{
		ID:             id,
		Expression:     expression,
		Instruction:    instruction,
		TargetAgent:    targetAgent,
		Status:         "active",
		NextRunAt:      nextRun,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		QQSource:       qqSource,
		QQOpenID:       qqOpenID,
		QQTargetOpenID: qqTargetOpenID,
		QQChatID:       qqChatID,
	}, nil
}

// UpdateTask updates editable fields of a task (expression, instruction, target_agent, status, next_run_at).
func (s *DBStore) UpdateTask(ctx context.Context, t *Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Format(time.RFC3339)
	nRun := t.NextRunAt.Format(time.RFC3339)

	_, err := s.db.ExecContext(ctx,
		`UPDATE scheduled_tasks SET expression = ?, instruction = ?, target_agent = ?, status = ?, next_run_at = ?, updated_at = ?, qq_source = ?, qq_openid = ?, qq_target_openid = ?, qq_chat_id = ? WHERE id = ?`,
		t.Expression, t.Instruction, t.TargetAgent, t.Status, nRun, now, t.QQSource, t.QQOpenID, t.QQTargetOpenID, t.QQChatID, t.ID)
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

// MarkFailed sets status of a task to 'failed'.
func (s *DBStore) MarkFailed(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE scheduled_tasks SET status = 'failed', last_run_at = ?, updated_at = ? WHERE id = ?`,
		now, now, id)
	if err != nil {
		return fmt.Errorf("cron store: mark failed: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("cron store: task %q not found", id)
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
