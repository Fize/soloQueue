package todo

import (
	"context"
	"fmt"
	"strings"
)

// Service layers business logic on top of Store.
// It handles validation, cycle detection, and cascade completion rules.
type Service struct {
	store *Store
}

// NewService creates a new service wrapping the given store.
func NewService(store *Store) *Service {
	return &Service{store: store}
}

// Store returns the underlying store.
func (svc *Service) Store() *Store {
	return svc.store
}

// ─── Plan Operations ────────────────────────────────────────────────────────

// CreatePlan validates and creates a new plan.
func (svc *Service) CreatePlan(ctx context.Context, req CreatePlanRequest) (*Plan, error) {
	if err := validateTitle(req.Title); err != nil {
		return nil, err
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Content = strings.TrimSpace(req.Content)
	req.Tags = strings.TrimSpace(req.Tags)
	req.Creator = strings.TrimSpace(req.Creator)

	return svc.store.CreatePlan(ctx, req)
}

// GetPlan returns a plan by ID.
func (svc *Service) GetPlan(ctx context.Context, id string) (*Plan, error) {
	if id == "" {
		return nil, fmt.Errorf("plan ID is required")
	}
	return svc.store.GetPlan(ctx, id)
}

// ListPlans returns plans filtered by status.
func (svc *Service) ListPlans(ctx context.Context, status string) ([]Plan, error) {
	return svc.store.ListPlans(ctx, status)
}

// UpdatePlan validates and updates a plan.
func (svc *Service) UpdatePlan(ctx context.Context, id string, req UpdatePlanRequest) (*Plan, error) {
	if id == "" {
		return nil, fmt.Errorf("plan ID is required")
	}
	if req.Title != nil {
		t := strings.TrimSpace(*req.Title)
		if t == "" {
			return nil, fmt.Errorf("title cannot be empty")
		}
		req.Title = &t
	}
	if req.Content != nil {
		c := strings.TrimSpace(*req.Content)
		req.Content = &c
	}
	if req.Status != nil && !ValidPlanStatus(*req.Status) {
		return nil, fmt.Errorf("invalid status: %q (must be plan, running, or done)", *req.Status)
	}
	if req.Tags != nil {
		t := strings.TrimSpace(*req.Tags)
		req.Tags = &t
	}
	return svc.store.UpdatePlan(ctx, id, req)
}

// UpdatePlanStatus changes a plan's status.
func (svc *Service) UpdatePlanStatus(ctx context.Context, id string, status PlanStatus) (*Plan, error) {
	if id == "" {
		return nil, fmt.Errorf("plan ID is required")
	}
	if !ValidPlanStatus(string(status)) {
		return nil, fmt.Errorf("invalid status: %q", status)
	}
	return svc.store.UpdatePlanStatus(ctx, id, status)
}

// DeletePlan removes a plan.
func (svc *Service) DeletePlan(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	return svc.store.DeletePlan(ctx, id)
}

// ─── TodoItem Operations ────────────────────────────────────────────────────

// CreateTodoItem validates and creates a todo item.
func (svc *Service) CreateTodoItem(ctx context.Context, planID string, req CreateTodoRequest) (*TodoItemWithDeps, error) {
	if planID == "" {
		return nil, fmt.Errorf("plan ID is required")
	}
	if strings.TrimSpace(req.Content) == "" {
		return nil, fmt.Errorf("todo content cannot be empty")
	}
	req.Content = strings.TrimSpace(req.Content)

	return svc.store.CreateTodoItem(ctx, planID, req)
}

// ListTodoItems returns todo items for a plan.
func (svc *Service) ListTodoItems(ctx context.Context, planID string) ([]TodoItemWithDeps, error) {
	if planID == "" {
		return nil, fmt.Errorf("plan ID is required")
	}
	return svc.store.ListTodoItems(ctx, planID)
}

// UpdateTodoItem validates and updates a todo item.
func (svc *Service) UpdateTodoItem(ctx context.Context, id string, req UpdateTodoRequest) (*TodoItemWithDeps, error) {
	if id == "" {
		return nil, fmt.Errorf("todo ID is required")
	}
	if req.Content != nil {
		c := strings.TrimSpace(*req.Content)
		if c == "" {
			return nil, fmt.Errorf("todo content cannot be empty")
		}
		req.Content = &c
	}
	return svc.store.UpdateTodoItem(ctx, id, req)
}

// ToggleTodoItem flips a todo item's completed status, enforcing dependency rules.
// Returns an error if any dependency is not yet completed.
func (svc *Service) ToggleTodoItem(ctx context.Context, id string) (*TodoItemWithDeps, error) {
	if id == "" {
		return nil, fmt.Errorf("todo ID is required")
	}

	// Get current state
	current, err := svc.store.getTodoItemWithDeps(ctx, id)
	if err != nil {
		return nil, err
	}

	// If currently completed, allow un-completing freely (no dependency check needed)
	if current.Completed {
		return svc.store.ToggleTodoItem(ctx, id)
	}

	// Trying to complete: verify all dependencies are completed
	for _, depID := range current.DependsOn {
		dep, err := svc.store.getTodoItemWithDeps(ctx, depID)
		if err != nil {
			return nil, fmt.Errorf("dependency %q not found: %w", depID, err)
		}
		if !dep.Completed {
			return nil, fmt.Errorf("cannot complete: dependency %q (%s) is not yet completed", depID, dep.Content)
		}
	}

	return svc.store.ToggleTodoItem(ctx, id)
}

// DeleteTodoItem removes a todo item.
func (svc *Service) DeleteTodoItem(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	return svc.store.DeleteTodoItem(ctx, id)
}

// ReorderTodoItems reorders todo items.
func (svc *Service) ReorderTodoItems(ctx context.Context, planID string, ids []string) error {
	if planID == "" {
		return fmt.Errorf("plan ID is required")
	}
	if len(ids) == 0 {
		return nil
	}
	return svc.store.ReorderTodoItems(ctx, planID, ids)
}

// ─── Dependency Operations ──────────────────────────────────────────────────

// GetDependencies returns the dependency graph for a todo item.
func (svc *Service) GetDependencies(ctx context.Context, todoID string) (*DependenciesResponse, error) {
	if todoID == "" {
		return nil, fmt.Errorf("todo ID is required")
	}
	return svc.store.GetDependencies(ctx, todoID)
}

// SetDependencies validates and sets dependencies for a todo item.
// Detects and rejects cycles.
func (svc *Service) SetDependencies(ctx context.Context, todoID string, dependsOn []string) error {
	if todoID == "" {
		return fmt.Errorf("todo ID is required")
	}

	// Self-reference check
	for _, depID := range dependsOn {
		if depID == todoID {
			return fmt.Errorf("a todo item cannot depend on itself")
		}
	}

	// De-duplicate
	seen := make(map[string]bool)
	var unique []string
	for _, id := range dependsOn {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}

	if len(unique) > 0 {
		// Cycle detection via BFS: check if any dep transitively depends on todoID
		if err := svc.checkCycle(ctx, todoID, unique); err != nil {
			return err
		}
	}

	return svc.store.SetDependencies(ctx, todoID, unique)
}

// ─── Cycle Detection ───────────────────────────────────────────────────────

// checkCycle performs BFS to verify that none of the new dependencies
// transitively depends on todoID (which would create a cycle).
func (svc *Service) checkCycle(ctx context.Context, todoID string, newDeps []string) error {
	// Build the full dependency graph by walking all dependencies
	// Check if todoID is reachable from any of newDeps
	visited := make(map[string]bool)
	queue := make([]string, 0, len(newDeps))
	queue = append(queue, newDeps...)

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current == todoID {
			return fmt.Errorf("cycle detected: adding these dependencies would create a circular dependency")
		}

		if visited[current] {
			continue
		}
		visited[current] = true

		// Get dependencies of current node
		rows, err := svc.store.db.QueryContext(ctx,
			`SELECT depends_on FROM todo_dependencies WHERE todo_id = ?`, current)
		if err != nil {
			return fmt.Errorf("cycle check: query: %w", err)
		}
		var subDeps []string
		for rows.Next() {
			var d string
			if err := rows.Scan(&d); err != nil {
				rows.Close()
				return err
			}
			subDeps = append(subDeps, d)
		}
		rows.Close()

		queue = append(queue, subDeps...)
	}

	return nil
}

// ─── Validation Helpers ─────────────────────────────────────────────────────

func validateTitle(title string) error {
	t := strings.TrimSpace(title)
	if t == "" {
		return fmt.Errorf("title is required")
	}
	if len(t) > 500 {
		return fmt.Errorf("title too long (max 500 chars)")
	}
	return nil
}
