package cron

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
)

// Task is a scheduled/timer task persisted in SQLite.
type Task struct {
	ID             string     `json:"id"`
	Title          string     `json:"title"`
	TaskLevel      string     `json:"task_level"`
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
	// SourceChannel is the channel type that created this task ("qq", "wechat", or "" for web).
	SourceChannel string `json:"source_channel"`
	// SourceUserID is the user identifier in the source channel.
	SourceUserID string `json:"source_user_id"`
	// SourceConvID is the conversation identifier in the source channel.
	SourceConvID string `json:"source_conv_id"`
}

const (
	TaskLevelL0 = "L0"
	TaskLevelL1 = "L1"
	TaskLevelL2 = "L2"
	TaskLevelL3 = "L3"
	TaskLevelL4 = "L4"
)

// CreateTaskInput contains the required and optional fields for a new task.
type CreateTaskInput struct {
	Title       string
	TaskLevel   string
	Expression  string
	Instruction string
	TargetAgent string
	NextRunAt   time.Time
}

// ValidateTaskLevel validates the persisted task-level enum.
func ValidateTaskLevel(level string) error {
	switch level {
	case TaskLevelL0, TaskLevelL1, TaskLevelL2, TaskLevelL3, TaskLevelL4:
		return nil
	default:
		return fmt.Errorf("task_level must be one of L0, L1, L2, L3, or L4")
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

func validateTaskFields(title, taskLevel, expression, instruction string) error {
	if err := ValidateTaskTitle(title); err != nil {
		return err
	}
	if err := ValidateTaskLevel(taskLevel); err != nil {
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
		`SELECT id, title, task_level, expression, instruction, target_agent, status, last_run_at, next_run_at, created_at, updated_at, qq_source, qq_openid, qq_target_openid, qq_chat_id, source_channel, source_user_id, source_conv_id
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
		var srcChannel, srcUserID, srcConvID sql.NullString
		err := rows.Scan(&t.ID, &t.Title, &t.TaskLevel, &t.Expression, &t.Instruction, &t.TargetAgent, &t.Status, &lRun, &nRun, &cAt, &uAt, &qqSource, &qqOpenID, &qqTargetOpenID, &qqChatID, &srcChannel, &srcUserID, &srcConvID)
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
		if srcChannel.Valid {
			t.SourceChannel = srcChannel.String
		}
		if srcUserID.Valid {
			t.SourceUserID = srcUserID.String
		}
		if srcConvID.Valid {
			t.SourceConvID = srcConvID.String
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
	var srcChannel, srcUserID, srcConvID sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, title, task_level, expression, instruction, target_agent, status, last_run_at, next_run_at, created_at, updated_at, qq_source, qq_openid, qq_target_openid, qq_chat_id, source_channel, source_user_id, source_conv_id
		 FROM scheduled_tasks WHERE id = ?`, id).
		Scan(&t.ID, &t.Title, &t.TaskLevel, &t.Expression, &t.Instruction, &t.TargetAgent, &t.Status, &lRun, &nRun, &cAt, &uAt, &qqSource, &qqOpenID, &qqTargetOpenID, &qqChatID, &srcChannel, &srcUserID, &srcConvID)
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
	if srcChannel.Valid {
		t.SourceChannel = srcChannel.String
	}
	if srcUserID.Valid {
		t.SourceUserID = srcUserID.String
	}
	if srcConvID.Valid {
		t.SourceConvID = srcConvID.String
	}

	return &t, nil
}

// GetActiveTasks returns all tasks with 'active' status.
func (s *DBStore) GetActiveTasks(ctx context.Context) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, task_level, expression, instruction, target_agent, status, last_run_at, next_run_at, created_at, updated_at, qq_source, qq_openid, qq_target_openid, qq_chat_id, source_channel, source_user_id, source_conv_id
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
		var srcChannel, srcUserID, srcConvID sql.NullString
		err := rows.Scan(&t.ID, &t.Title, &t.TaskLevel, &t.Expression, &t.Instruction, &t.TargetAgent, &t.Status, &lRun, &nRun, &cAt, &uAt, &qqSource, &qqOpenID, &qqTargetOpenID, &qqChatID, &srcChannel, &srcUserID, &srcConvID)
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
		if srcChannel.Valid {
			t.SourceChannel = srcChannel.String
		}
		if srcUserID.Valid {
			t.SourceUserID = srcUserID.String
		}
		if srcConvID.Valid {
			t.SourceConvID = srcConvID.String
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

// getChannelMeta extracts generic channel source metadata from context.
// It tries the new ChatMeta key first, then falls back to legacy QQ context.
func getChannelMeta(ctx context.Context) (sourceChannel, userID, convID string) {
	// 1. Try new generic context key
	if meta, ok := channel.ChatMetaFromContext(ctx); ok {
		return meta.Channel, meta.UserID, meta.ConversationID
	}
	// 2. Fallback: legacy QQ context
	_, openID, targetOpenID, chatID, exists := getQQMessageMeta(ctx)
	if exists {
		conv := chatID
		if conv == "" {
			conv = targetOpenID
		}
		return "qq", openID, conv
	}
	return "", "", ""
}

// CreateTask inserts a new task.
func (s *DBStore) CreateTask(ctx context.Context, input CreateTaskInput) (*Task, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Expression = strings.TrimSpace(input.Expression)
	input.Instruction = strings.TrimSpace(input.Instruction)
	input.TargetAgent = strings.TrimSpace(input.TargetAgent)
	if err := validateTaskFields(input.Title, input.TaskLevel, input.Expression, input.Instruction); err != nil {
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

	qqSource, qqOpenID, qqTargetOpenID, qqChatID, _ := getQQMessageMeta(ctx)
	sourceChannel, sourceUserID, sourceConvID := getChannelMeta(ctx)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO scheduled_tasks (id, title, task_level, expression, instruction, target_agent, status, next_run_at, created_at, updated_at, qq_source, qq_openid, qq_target_openid, qq_chat_id, source_channel, source_user_id, source_conv_id)
		 VALUES (?, ?, ?, ?, ?, ?, 'active', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, input.Title, input.TaskLevel, input.Expression, input.Instruction, input.TargetAgent, nRun, now, now, qqSource, qqOpenID, qqTargetOpenID, qqChatID, sourceChannel, sourceUserID, sourceConvID)
	if err != nil {
		return nil, fmt.Errorf("cron store: create task: %w", err)
	}

	return &Task{
		ID:             id,
		Title:          input.Title,
		TaskLevel:      input.TaskLevel,
		Expression:     input.Expression,
		Instruction:    input.Instruction,
		TargetAgent:    input.TargetAgent,
		Status:         "active",
		NextRunAt:      input.NextRunAt,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		QQSource:       qqSource,
		QQOpenID:       qqOpenID,
		QQTargetOpenID: qqTargetOpenID,
		QQChatID:       qqChatID,
		SourceChannel:  sourceChannel,
		SourceUserID:   sourceUserID,
		SourceConvID:   sourceConvID,
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
	if err := validateTaskFields(t.Title, t.TaskLevel, t.Expression, t.Instruction); err != nil {
		return fmt.Errorf("cron store: invalid task: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Format(time.RFC3339)
	nRun := t.NextRunAt.Format(time.RFC3339)

	query := `UPDATE scheduled_tasks SET title = ?, task_level = ?, expression = ?, instruction = ?, target_agent = ?, status = ?, next_run_at = ?, updated_at = ?, qq_source = ?, qq_openid = ?, qq_target_openid = ?, qq_chat_id = ?, source_channel = ?, source_user_id = ?, source_conv_id = ? WHERE id = ?`
	args := []any{t.Title, t.TaskLevel, t.Expression, t.Instruction, t.TargetAgent, t.Status, nRun, now, t.QQSource, t.QQOpenID, t.QQTargetOpenID, t.QQChatID, t.SourceChannel, t.SourceUserID, t.SourceConvID, t.ID}
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
	return nil
}
