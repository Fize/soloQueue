// Package todo provides a Trello-style plan/task system backed by SQLite.
//
// Plans have three states (plan → running → done), contain a list of todo items
// with dependency relationships, and support LLM tool-based manipulation plus
// a minimal HTTP API for read/update/delete operations.
//
// Architecture:
//
//	store.go   — SQLite CRUD (plans, todo_items, todo_dependencies tables)
//	service.go — Business logic (validation, cycle detection, cascade rules)
//	types.go   — Domain types and request/response DTOs
package todo

import "time"

// ─── Domain Types ──────────────────────────────────────────────────────────

// PlanStatus represents the lifecycle state of a plan.
type PlanStatus string

const (
	StatusPlan    PlanStatus = "plan"
	StatusRunning PlanStatus = "running"
	StatusDone    PlanStatus = "done"
)

// ValidPlanStatus checks whether a status string is a valid PlanStatus.
func ValidPlanStatus(s string) bool {
	switch PlanStatus(s) {
	case StatusPlan, StatusRunning, StatusDone:
		return true
	}
	return false
}

// Plan is a task plan with embedded todo items.
type Plan struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Content   string     `json:"content"`
	Status    PlanStatus `json:"status"`
	Tags      string     `json:"tags"` // comma-separated, e.g. "bug,frontend"
	Creator   string     `json:"creator"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`

	// Populated on detail queries
	TodoItems []TodoItemWithDeps `json:"todo_items,omitempty"`
}

// TodoItem is a single checkable item within a plan.
type TodoItem struct {
	ID        string    `json:"id"`
	PlanID    string    `json:"plan_id"`
	Content   string    `json:"content"`
	Completed bool      `json:"completed"`
	SortOrder int       `json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
}

// TodoItemWithDeps extends TodoItem with its dependency information.
type TodoItemWithDeps struct {
	TodoItem
	DependsOn []string `json:"depends_on"` // IDs this item depends on
	Blockers  []string `json:"blockers"`   // IDs that depend on this item (reverse edges)
}

// ─── Request DTOs ──────────────────────────────────────────────────────────

// CreatePlanRequest is the input for creating a new plan.
type CreatePlanRequest struct {
	Title     string              `json:"title"`
	Content   string              `json:"content,omitempty"`
	Status    string              `json:"status,omitempty"`
	Tags      string              `json:"tags,omitempty"`
	Creator   string              `json:"creator,omitempty"`
	TodoItems []CreateTodoRequest `json:"todo_items,omitempty"`
}

// UpdatePlanRequest is the input for updating an existing plan.
type UpdatePlanRequest struct {
	Title   *string `json:"title,omitempty"`
	Content *string `json:"content,omitempty"`
	Status  *string `json:"status,omitempty"`
	Tags    *string `json:"tags,omitempty"`
}

// CreateTodoRequest is the input for creating todo items.
type CreateTodoRequest struct {
	Content   string   `json:"content"`
	SortOrder int      `json:"sort_order,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"` // IDs of items this depends on
}

// UpdateTodoRequest is the input for updating a todo item.
type UpdateTodoRequest struct {
	Content   *string `json:"content,omitempty"`
	Completed *bool   `json:"completed,omitempty"`
	SortOrder *int    `json:"sort_order,omitempty"`
}

// ReorderTodosRequest is the input for reordering todo items.
type ReorderTodosRequest struct {
	IDs []string `json:"ids"`
}

// SetDependenciesRequest is the input for setting dependencies on a todo item.
type SetDependenciesRequest struct {
	DependsOn []string `json:"depends_on"`
}

// ─── Response DTOs ─────────────────────────────────────────────────────────

// PlanListResponse is returned for list queries.
type PlanListResponse struct {
	Plans []Plan `json:"plans"`
	Total int    `json:"total"`
}

// TodoListResponse is returned for todo item list queries.
type TodoListResponse struct {
	Todos []TodoItemWithDeps `json:"todos"`
	Total int                `json:"total"`
}

// DependenciesResponse is returned for dependency queries.
type DependenciesResponse struct {
	TodoID    string   `json:"todo_id"`
	DependsOn []string `json:"depends_on"`
	Blockers  []string `json:"blockers"`
}
