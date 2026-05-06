package todo

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Store manages plan/todo persistence in a SQLite database.
// Writes are serialized via mutex; reads are concurrent.
// The store reuses the existing entries.db or creates it on first access.
type Store struct {
	db *sql.DB
	mu sync.Mutex // serializes writes (SQLite single-writer)
}

// NewStore opens or creates a SQLite-backed todo store at the given path.
// The database file is shared with the permanent memory vector store.
func NewStore(path string) (*Store, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("todo store: create dir: %w", err)
	}

	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_foreign_keys=ON")
	if err != nil {
		return nil, fmt.Errorf("todo store: open db: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("todo store: migrate: %w", err)
	}

	return s, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying *sql.DB. Used by friends in the server package
// to share the connection for HTTP handlers.
func (s *Store) DB() *sql.DB {
	return s.db
}

// ─── Migration ──────────────────────────────────────────────────────────────

func (s *Store) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS plans (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		content TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'plan' CHECK(status IN ('plan','running','done')),
		tags TEXT NOT NULL DEFAULT '',
		creator TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS todo_items (
		id TEXT PRIMARY KEY,
		plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
		content TEXT NOT NULL,
		completed INTEGER NOT NULL DEFAULT 0,
		sort_order INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS todo_dependencies (
		todo_id TEXT NOT NULL REFERENCES todo_items(id) ON DELETE CASCADE,
		depends_on TEXT NOT NULL REFERENCES todo_items(id) ON DELETE CASCADE,
		PRIMARY KEY (todo_id, depends_on)
	);

	CREATE INDEX IF NOT EXISTS idx_todo_plan ON todo_items(plan_id);
	CREATE INDEX IF NOT EXISTS idx_plans_status ON plans(status);
	CREATE INDEX IF NOT EXISTS idx_todo_deps_todo ON todo_dependencies(todo_id);
	CREATE INDEX IF NOT EXISTS idx_todo_deps_depends ON todo_dependencies(depends_on);
	`
	_, err := s.db.Exec(schema)
	return err
}

// ─── Plan CRUD ──────────────────────────────────────────────────────────────

// ListPlans returns plans, optionally filtered by status.
func (s *Store) ListPlans(ctx context.Context, status string) ([]Plan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var rows *sql.Rows
	var err error

	if ValidPlanStatus(status) {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, title, content, status, tags, creator, created_at, updated_at
			 FROM plans WHERE status = ? ORDER BY updated_at DESC`, status)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, title, content, status, tags, creator, created_at, updated_at
			 FROM plans ORDER BY updated_at DESC`)
	}
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	defer rows.Close()

	var plans []Plan
	for rows.Next() {
		var p Plan
		var cAt, uAt string
		if err := rows.Scan(&p.ID, &p.Title, &p.Content, &p.Status, &p.Tags, &p.Creator, &cAt, &uAt); err != nil {
			return nil, fmt.Errorf("scan plan: %w", err)
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339, cAt)
		p.UpdatedAt, _ = time.Parse(time.RFC3339, uAt)
		plans = append(plans, p)
	}
	return plans, rows.Err()
}

// GetPlan returns a single plan with its todo items and dependency graph.
func (s *Store) GetPlan(ctx context.Context, id string) (*Plan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var p Plan
	var cAt, uAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, title, content, status, tags, creator, created_at, updated_at
		 FROM plans WHERE id = ?`, id).
		Scan(&p.ID, &p.Title, &p.Content, &p.Status, &p.Tags, &p.Creator, &cAt, &uAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("plan %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get plan: %w", err)
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, cAt)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, uAt)

	// Load todo items with dependencies
	todos, err := s.listTodoItemsWithDeps(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get plan todos: %w", err)
	}
	p.TodoItems = todos

	return &p, nil
}

// CreatePlan inserts a new plan. Returns the created plan.
func (s *Store) CreatePlan(ctx context.Context, req CreatePlanRequest) (*Plan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	id := newID()
	now := time.Now().UTC().Format(time.RFC3339)
	status := StatusPlan
	if ValidPlanStatus(req.Status) {
		status = PlanStatus(req.Status)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("create plan: begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		`INSERT INTO plans (id, title, content, status, tags, creator, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, req.Title, req.Content, status, req.Tags, req.Creator, now, now)
	if err != nil {
		return nil, fmt.Errorf("create plan: insert: %w", err)
	}

	// Insert initial todo items if provided
	for i := range req.TodoItems {
		if err := s.insertTodoItemTx(ctx, tx, id, req.TodoItems[i], now); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("create plan: commit: %w", err)
	}

	return &Plan{
		ID: id, Title: req.Title, Content: req.Content,
		Status: status, Tags: req.Tags, Creator: req.Creator,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}, nil
}

// UpdatePlan updates plan fields. Nil fields are left unchanged.
func (s *Store) UpdatePlan(ctx context.Context, id string, req UpdatePlanRequest) (*Plan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify existence
	existing, err := s.getPlanInTx(ctx, id)
	if err != nil {
		return nil, err
	}

	// Build dynamic UPDATE
	sets := []string{}
	args := []any{}

	if req.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *req.Title)
	}
	if req.Content != nil {
		sets = append(sets, "content = ?")
		args = append(args, *req.Content)
	}
	if req.Status != nil && ValidPlanStatus(*req.Status) {
		sets = append(sets, "status = ?")
		args = append(args, *req.Status)
	}
	if req.Tags != nil {
		sets = append(sets, "tags = ?")
		args = append(args, *req.Tags)
	}

	if len(sets) == 0 {
		return existing, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	sets = append(sets, "updated_at = ?")
	args = append(args, now, id)

	_, err = s.db.ExecContext(ctx,
		`UPDATE plans SET `+strings.Join(sets, ", ")+` WHERE id = ?`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("update plan: %w", err)
	}

	// Reload
	return s.getPlanInTx(ctx, id)
}

// UpdatePlanStatus changes a plan's status.
func (s *Store) UpdatePlanStatus(ctx context.Context, id string, status PlanStatus) (*Plan, error) {
	return s.UpdatePlan(ctx, id, UpdatePlanRequest{Status: (*string)(&status)})
}

// DeletePlan removes a plan and all its todo items (CASCADE).
func (s *Store) DeletePlan(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.ExecContext(ctx, `DELETE FROM plans WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete plan: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("plan %q not found", id)
	}
	return nil
}

// ─── TodoItem CRUD ──────────────────────────────────────────────────────────

// ListTodoItems returns all todo items for a plan with dependency info.
func (s *Store) ListTodoItems(ctx context.Context, planID string) ([]TodoItemWithDeps, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.listTodoItemsWithDeps(ctx, planID)
}

// CreateTodoItem adds a todo item to a plan.
func (s *Store) CreateTodoItem(ctx context.Context, planID string, req CreateTodoRequest) (*TodoItemWithDeps, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify plan exists
	if _, err := s.getPlanInTx(ctx, planID); err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("create todo: begin tx: %w", err)
	}
	defer tx.Rollback()

	itemID := newID()
	_, err = tx.ExecContext(ctx,
		`INSERT INTO todo_items (id, plan_id, content, completed, sort_order, created_at)
		 VALUES (?, ?, ?, 0, ?, ?)`,
		itemID, planID, req.Content, req.SortOrder, now)
	if err != nil {
		return nil, fmt.Errorf("create todo: insert: %w", err)
	}

	// Create dependency edges
	for _, depID := range req.DependsOn {
		_, err = tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO todo_dependencies (todo_id, depends_on) VALUES (?, ?)`,
			itemID, depID)
		if err != nil {
			return nil, fmt.Errorf("create todo: dependency: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("create todo: commit: %w", err)
	}

	// Reload with deps
	return s.getTodoItemWithDeps(ctx, itemID)
}

// UpdateTodoItem updates a todo item's fields.
func (s *Store) UpdateTodoItem(ctx context.Context, id string, req UpdateTodoRequest) (*TodoItemWithDeps, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	sets := []string{}
	args := []any{}

	if req.Content != nil {
		sets = append(sets, "content = ?")
		args = append(args, *req.Content)
	}
	if req.SortOrder != nil {
		sets = append(sets, "sort_order = ?")
		args = append(args, *req.SortOrder)
	}
	if req.Completed != nil {
		sets = append(sets, "completed = ?")
		if *req.Completed {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}

	if len(sets) == 0 {
		return s.getTodoItemWithDeps(ctx, id)
	}

	args = append(args, id)
	_, err := s.db.ExecContext(ctx,
		`UPDATE todo_items SET `+strings.Join(sets, ", ")+` WHERE id = ?`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("update todo: %w", err)
	}

	return s.getTodoItemWithDeps(ctx, id)
}

// ToggleTodoItem flips the completed status of a todo item.
func (s *Store) ToggleTodoItem(ctx context.Context, id string) (*TodoItemWithDeps, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx,
		`UPDATE todo_items SET completed = CASE WHEN completed = 0 THEN 1 ELSE 0 END WHERE id = ?`,
		id)
	if err != nil {
		return nil, fmt.Errorf("toggle todo: %w", err)
	}

	return s.getTodoItemWithDeps(ctx, id)
}

// DeleteTodoItem removes a todo item (dependencies are cleaned via CASCADE).
func (s *Store) DeleteTodoItem(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.ExecContext(ctx, `DELETE FROM todo_items WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete todo: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("todo item %q not found", id)
	}
	return nil
}

// ReorderTodoItems updates sort_order for a batch of todo items.
func (s *Store) ReorderTodoItems(ctx context.Context, planID string, ids []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("reorder todos: begin tx: %w", err)
	}
	defer tx.Rollback()

	for i, id := range ids {
		_, err = tx.ExecContext(ctx,
			`UPDATE todo_items SET sort_order = ? WHERE id = ? AND plan_id = ?`,
			i, id, planID)
		if err != nil {
			return fmt.Errorf("reorder todos: update %q: %w", id, err)
		}
	}

	return tx.Commit()
}

// ─── Dependencies ───────────────────────────────────────────────────────────

// GetDependencies returns the dependency graph for a todo item.
func (s *Store) GetDependencies(ctx context.Context, todoID string) (*DependenciesResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// depends_on — items this todo depends on
	rows, err := s.db.QueryContext(ctx,
		`SELECT depends_on FROM todo_dependencies WHERE todo_id = ?`, todoID)
	if err != nil {
		return nil, fmt.Errorf("get dependencies: %w", err)
	}
	var deps []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			rows.Close()
			return nil, err
		}
		deps = append(deps, d)
	}
	rows.Close()

	// blockers — items that depend on this todo
	rows2, err := s.db.QueryContext(ctx,
		`SELECT todo_id FROM todo_dependencies WHERE depends_on = ?`, todoID)
	if err != nil {
		return nil, fmt.Errorf("get blockers: %w", err)
	}
	var blockers []string
	for rows2.Next() {
		var b string
		if err := rows2.Scan(&b); err != nil {
			rows2.Close()
			return nil, err
		}
		blockers = append(blockers, b)
	}
	rows2.Close()

	return &DependenciesResponse{
		TodoID:    todoID,
		DependsOn: deps,
		Blockers:  blockers,
	}, nil
}

// SetDependencies replaces all dependency edges for a todo item.
func (s *Store) SetDependencies(ctx context.Context, todoID string, dependsOn []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("set dependencies: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Delete existing
	_, err = tx.ExecContext(ctx, `DELETE FROM todo_dependencies WHERE todo_id = ?`, todoID)
	if err != nil {
		return fmt.Errorf("set dependencies: delete old: %w", err)
	}

	// Insert new
	for _, depID := range dependsOn {
		_, err = tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO todo_dependencies (todo_id, depends_on) VALUES (?, ?)`,
			todoID, depID)
		if err != nil {
			return fmt.Errorf("set dependencies: insert: %w", err)
		}
	}

	return tx.Commit()
}

// ─── Internal Helpers ──────────────────────────────────────────────────────

func (s *Store) getPlanInTx(ctx context.Context, id string) (*Plan, error) {
	var p Plan
	var cAt, uAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, title, content, status, tags, creator, created_at, updated_at
		 FROM plans WHERE id = ?`, id).
		Scan(&p.ID, &p.Title, &p.Content, &p.Status, &p.Tags, &p.Creator, &cAt, &uAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("plan %q not found", id)
	}
	if err != nil {
		return nil, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, cAt)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, uAt)
	return &p, nil
}

func (s *Store) listTodoItemsWithDeps(ctx context.Context, planID string) ([]TodoItemWithDeps, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, plan_id, content, completed, sort_order, created_at
		 FROM todo_items WHERE plan_id = ?
		 ORDER BY sort_order ASC, created_at ASC`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var todos []TodoItemWithDeps
	for rows.Next() {
		var t TodoItemWithDeps
		var comp int
		var cAt string
		if err := rows.Scan(&t.ID, &t.PlanID, &t.Content, &comp, &t.SortOrder, &cAt); err != nil {
			return nil, err
		}
		t.Completed = comp != 0
		t.CreatedAt, _ = time.Parse(time.RFC3339, cAt)

		// Load dependencies for this item
		t.DependsOn, t.Blockers, err = s.getDepsForItem(ctx, t.ID)
		if err != nil {
			return nil, err
		}
		todos = append(todos, t)
	}
	return todos, rows.Err()
}

func (s *Store) getTodoItemWithDeps(ctx context.Context, id string) (*TodoItemWithDeps, error) {
	var t TodoItemWithDeps
	var comp int
	var cAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, plan_id, content, completed, sort_order, created_at
		 FROM todo_items WHERE id = ?`, id).
		Scan(&t.ID, &t.PlanID, &t.Content, &comp, &t.SortOrder, &cAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("todo item %q not found", id)
	}
	if err != nil {
		return nil, err
	}
	t.Completed = comp != 0
	t.CreatedAt, _ = time.Parse(time.RFC3339, cAt)

	t.DependsOn, t.Blockers, err = s.getDepsForItem(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) getDepsForItem(ctx context.Context, todoID string) (dependsOn, blockers []string, err error) {
	// depends_on
	rows, err := s.db.QueryContext(ctx,
		`SELECT depends_on FROM todo_dependencies WHERE todo_id = ?`, todoID)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			rows.Close()
			return nil, nil, err
		}
		dependsOn = append(dependsOn, d)
	}
	rows.Close()

	// blockers
	rows2, err := s.db.QueryContext(ctx,
		`SELECT todo_id FROM todo_dependencies WHERE depends_on = ?`, todoID)
	if err != nil {
		return nil, nil, err
	}
	for rows2.Next() {
		var b string
		if err := rows2.Scan(&b); err != nil {
			rows2.Close()
			return nil, nil, err
		}
		blockers = append(blockers, b)
	}
	rows2.Close()

	return dependsOn, blockers, nil
}

func (s *Store) insertTodoItemTx(ctx context.Context, tx *sql.Tx, planID string, req CreateTodoRequest, now string) error {
	itemID := newID()
	_, err := tx.ExecContext(ctx,
		`INSERT INTO todo_items (id, plan_id, content, completed, sort_order, created_at)
		 VALUES (?, ?, ?, 0, ?, ?)`,
		itemID, planID, req.Content, req.SortOrder, now)
	if err != nil {
		return fmt.Errorf("insert todo item: %w", err)
	}

	for _, depID := range req.DependsOn {
		_, err = tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO todo_dependencies (todo_id, depends_on) VALUES (?, ?)`,
			itemID, depID)
		if err != nil {
			return fmt.Errorf("insert todo dep: %w", err)
		}
	}
	return nil
}

// ─── ID Generation ─────────────────────────────────────────────────────────

// newID generates a simple unique ID. The project uses github.com/google/uuid
// in other places, but for simplicity and test determinism we use a
// timestamp-based approach. Importers can swap via build tags if needed.
var newID = func() string {
	// Use a simple time + counter approach for uniqueness.
	// In production, this is replaced when the package is initialized
	// to use proper UUID generation. Tests may override this.
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
