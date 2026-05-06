package todo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func tempDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "entries.db")
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(tempDB(t))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	return NewService(newTestStore(t))
}

func TestStore_NewStore_CreatesTables(t *testing.T) {
	store := newTestStore(t)

	// Verify tables exist by querying them
	_, err := store.db.Exec(`SELECT 1 FROM plans LIMIT 0`)
	if err != nil {
		t.Errorf("plans table missing: %v", err)
	}
	_, err = store.db.Exec(`SELECT 1 FROM todo_items LIMIT 0`)
	if err != nil {
		t.Errorf("todo_items table missing: %v", err)
	}
	_, err = store.db.Exec(`SELECT 1 FROM todo_dependencies LIMIT 0`)
	if err != nil {
		t.Errorf("todo_dependencies table missing: %v", err)
	}
}

func TestStore_NewStore_ReusesExistingDB(t *testing.T) {
	path := tempDB(t)
	store1, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	store1.Close()

	store2, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore (reopen): %v", err)
	}
	defer store2.Close()

	// Should be empty
	plans, err := store2.ListPlans(context.Background(), "")
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	if len(plans) != 0 {
		t.Errorf("expected 0 plans, got %d", len(plans))
	}
}

// ─── Plan CRUD ────────────────────────────────────────────────────────────

func TestStore_CreatePlan(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	plan, err := svc.CreatePlan(ctx, CreatePlanRequest{
		Title:   "Test Plan",
		Content: "Test content",
		Tags:    "tag1,tag2",
		Creator: "tester",
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if plan.ID == "" {
		t.Error("expected non-empty ID")
	}
	if plan.Title != "Test Plan" {
		t.Errorf("title = %q, want %q", plan.Title, "Test Plan")
	}
	if plan.Status != StatusPlan {
		t.Errorf("status = %q, want %q", plan.Status, StatusPlan)
	}
}

func TestStore_CreatePlan_WithTodoItems(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	plan, err := svc.CreatePlan(ctx, CreatePlanRequest{
		Title: "Plan with todos",
		TodoItems: []CreateTodoRequest{
			{Content: "Task 1"},
			{Content: "Task 2", SortOrder: 1},
		},
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	full, err := svc.GetPlan(ctx, plan.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if len(full.TodoItems) != 2 {
		t.Errorf("expected 2 todo items, got %d", len(full.TodoItems))
	}
}

func TestStore_ListPlans(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	_, _ = svc.CreatePlan(ctx, CreatePlanRequest{Title: "Plan A"})
	_, _ = svc.CreatePlan(ctx, CreatePlanRequest{Title: "Plan B", Status: "running"})
	_, _ = svc.CreatePlan(ctx, CreatePlanRequest{Title: "Plan C", Status: "done"})

	all, err := svc.ListPlans(ctx, "")
	if err != nil {
		t.Fatalf("ListPlans(all): %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 plans, got %d", len(all))
	}

	running, err := svc.ListPlans(ctx, "running")
	if err != nil {
		t.Fatalf("ListPlans(running): %v", err)
	}
	if len(running) != 1 || running[0].Title != "Plan B" {
		t.Errorf("expected 1 running plan, got %d", len(running))
	}
}

func TestStore_UpdatePlan(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	plan, _ := svc.CreatePlan(ctx, CreatePlanRequest{Title: "Original"})

	newTitle := "Updated Title"
	plan, err := svc.UpdatePlan(ctx, plan.ID, UpdatePlanRequest{Title: &newTitle})
	if err != nil {
		t.Fatalf("UpdatePlan: %v", err)
	}
	if plan.Title != "Updated Title" {
		t.Errorf("title = %q, want %q", plan.Title, "Updated Title")
	}
}

func TestStore_UpdatePlanStatus(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	plan, _ := svc.CreatePlan(ctx, CreatePlanRequest{Title: "Status Test"})

	plan, err := svc.UpdatePlanStatus(ctx, plan.ID, StatusRunning)
	if err != nil {
		t.Fatalf("UpdatePlanStatus: %v", err)
	}
	if plan.Status != StatusRunning {
		t.Errorf("status = %q", plan.Status)
	}
}

func TestStore_DeletePlan(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	plan, _ := svc.CreatePlan(ctx, CreatePlanRequest{Title: "To Delete"})

	err := svc.DeletePlan(ctx, plan.ID)
	if err != nil {
		t.Fatalf("DeletePlan: %v", err)
	}

	_, err = svc.GetPlan(ctx, plan.ID)
	if err == nil {
		t.Error("expected error getting deleted plan")
	}
}

func TestStore_DeletePlan_NotFound(t *testing.T) {
	svc := newTestService(t)
	err := svc.DeletePlan(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent plan")
	}
}

// ─── TodoItem CRUD ────────────────────────────────────────────────────────

func TestStore_CreateTodoItem(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	plan, _ := svc.CreatePlan(ctx, CreatePlanRequest{Title: "Plan"})

	item, err := svc.CreateTodoItem(ctx, plan.ID, CreateTodoRequest{
		Content:   "Buy milk",
		SortOrder: 5,
	})
	if err != nil {
		t.Fatalf("CreateTodoItem: %v", err)
	}
	if item.ID == "" {
		t.Error("expected non-empty ID")
	}
	if item.Content != "Buy milk" {
		t.Errorf("content = %q", item.Content)
	}
	if item.SortOrder != 5 {
		t.Errorf("sort_order = %d", item.SortOrder)
	}
	if item.Completed {
		t.Error("expected not completed")
	}
}

func TestStore_UpdateTodoItem(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	plan, _ := svc.CreatePlan(ctx, CreatePlanRequest{Title: "Plan"})
	item, _ := svc.CreateTodoItem(ctx, plan.ID, CreateTodoRequest{Content: "Original"})

	newContent := "Updated"
	newSort := 10
	updated, err := svc.UpdateTodoItem(ctx, item.ID, UpdateTodoRequest{
		Content:   &newContent,
		SortOrder: &newSort,
	})
	if err != nil {
		t.Fatalf("UpdateTodoItem: %v", err)
	}
	if updated.Content != "Updated" {
		t.Errorf("content = %q", updated.Content)
	}
	if updated.SortOrder != 10 {
		t.Errorf("sort_order = %d", updated.SortOrder)
	}
}

func TestStore_ToggleTodoItem(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	plan, _ := svc.CreatePlan(ctx, CreatePlanRequest{Title: "Plan"})
	item, _ := svc.CreateTodoItem(ctx, plan.ID, CreateTodoRequest{Content: "Toggle me"})

	// Toggle to complete
	toggled, err := svc.ToggleTodoItem(ctx, item.ID)
	if err != nil {
		t.Fatalf("ToggleTodoItem: %v", err)
	}
	if !toggled.Completed {
		t.Error("expected completed after toggle")
	}

	// Toggle back
	toggled, err = svc.ToggleTodoItem(ctx, item.ID)
	if err != nil {
		t.Fatalf("ToggleTodoItem (back): %v", err)
	}
	if toggled.Completed {
		t.Error("expected not completed after second toggle")
	}
}

func TestStore_ToggleTodoItem_BlockedByDependency(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	plan, _ := svc.CreatePlan(ctx, CreatePlanRequest{Title: "Plan"})
	dep, _ := svc.CreateTodoItem(ctx, plan.ID, CreateTodoRequest{Content: "Dep"})
	blocked, _ := svc.CreateTodoItem(ctx, plan.ID, CreateTodoRequest{
		Content:   "Blocked",
		DependsOn: []string{dep.ID},
	})

	// Try to complete blocked item — should fail because dep is not completed
	_, err := svc.ToggleTodoItem(ctx, blocked.ID)
	if err == nil {
		t.Error("expected error: dependency not completed")
	}

	// Complete the dependency first
	_, _ = svc.ToggleTodoItem(ctx, dep.ID)

	// Now should succeed
	toggled, err := svc.ToggleTodoItem(ctx, blocked.ID)
	if err != nil {
		t.Fatalf("ToggleTodoItem after dep done: %v", err)
	}
	if !toggled.Completed {
		t.Error("expected completed")
	}
}

func TestStore_DeleteTodoItem(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	plan, _ := svc.CreatePlan(ctx, CreatePlanRequest{Title: "Plan"})
	item, _ := svc.CreateTodoItem(ctx, plan.ID, CreateTodoRequest{Content: "Delete me"})

	err := svc.DeleteTodoItem(ctx, item.ID)
	if err != nil {
		t.Fatalf("DeleteTodoItem: %v", err)
	}

	todos, _ := svc.ListTodoItems(ctx, plan.ID)
	if len(todos) != 0 {
		t.Errorf("expected 0 todos, got %d", len(todos))
	}
}

func TestStore_ReorderTodoItems(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	plan, _ := svc.CreatePlan(ctx, CreatePlanRequest{Title: "Plan"})
	a, _ := svc.CreateTodoItem(ctx, plan.ID, CreateTodoRequest{Content: "A", SortOrder: 0})
	b, _ := svc.CreateTodoItem(ctx, plan.ID, CreateTodoRequest{Content: "B", SortOrder: 1})
	c, _ := svc.CreateTodoItem(ctx, plan.ID, CreateTodoRequest{Content: "C", SortOrder: 2})

	// Reverse order
	err := svc.ReorderTodoItems(ctx, plan.ID, []string{c.ID, b.ID, a.ID})
	if err != nil {
		t.Fatalf("ReorderTodoItems: %v", err)
	}

	todos, _ := svc.ListTodoItems(ctx, plan.ID)
	if todos[0].ID != c.ID || todos[1].ID != b.ID || todos[2].ID != a.ID {
		t.Errorf("order mismatch: got %q, %q, %q", todos[0].ID, todos[1].ID, todos[2].ID)
	}
}

// ─── Dependencies ─────────────────────────────────────────────────────────

func TestStore_SetAndGetDependencies(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	plan, _ := svc.CreatePlan(ctx, CreatePlanRequest{Title: "Plan"})
	a, _ := svc.CreateTodoItem(ctx, plan.ID, CreateTodoRequest{Content: "A"})
	b, _ := svc.CreateTodoItem(ctx, plan.ID, CreateTodoRequest{Content: "B"})
	c, _ := svc.CreateTodoItem(ctx, plan.ID, CreateTodoRequest{Content: "C"})

	// C depends on A and B
	err := svc.SetDependencies(ctx, c.ID, []string{a.ID, b.ID})
	if err != nil {
		t.Fatalf("SetDependencies: %v", err)
	}

	deps, err := svc.GetDependencies(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetDependencies: %v", err)
	}
	if len(deps.DependsOn) != 2 {
		t.Errorf("expected 2 deps, got %d", len(deps.DependsOn))
	}

	// Check reverse: A is a blocker of C
	aDeps, err := svc.GetDependencies(ctx, a.ID)
	if err != nil {
		t.Fatalf("GetDependencies(A): %v", err)
	}
	if len(aDeps.Blockers) != 1 || aDeps.Blockers[0] != c.ID {
		t.Errorf("expected A to be blocked by C, got %v", aDeps.Blockers)
	}
}

func TestStore_CycleDetection(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	plan, _ := svc.CreatePlan(ctx, CreatePlanRequest{Title: "Plan"})
	a, _ := svc.CreateTodoItem(ctx, plan.ID, CreateTodoRequest{Content: "A"})
	b, _ := svc.CreateTodoItem(ctx, plan.ID, CreateTodoRequest{Content: "B"})

	// A → B
	_ = svc.SetDependencies(ctx, a.ID, []string{b.ID})

	// B → A should be rejected (cycle)
	err := svc.SetDependencies(ctx, b.ID, []string{a.ID})
	if err == nil {
		t.Error("expected cycle detection error")
	}
}

func TestStore_SelfDependency(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	plan, _ := svc.CreatePlan(ctx, CreatePlanRequest{Title: "Plan"})
	a, _ := svc.CreateTodoItem(ctx, plan.ID, CreateTodoRequest{Content: "A"})

	err := svc.SetDependencies(ctx, a.ID, []string{a.ID})
	if err == nil {
		t.Error("expected error for self-dependency")
	}
}

func TestStore_CascadeDelete(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	plan, _ := svc.CreatePlan(ctx, CreatePlanRequest{
		Title: "Plan",
		TodoItems: []CreateTodoRequest{
			{Content: "Task 1"},
			{Content: "Task 2"},
		},
	})

	// Delete plan — todos should cascade
	err := svc.DeletePlan(ctx, plan.ID)
	if err != nil {
		t.Fatalf("DeletePlan: %v", err)
	}

	// Verify plan is gone
	_, err = svc.GetPlan(ctx, plan.ID)
	if err == nil {
		t.Error("expected plan to be deleted")
	}
}

// ─── Validation ──────────────────────────────────────────────────────────

func TestValidation_TitleRequired(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.CreatePlan(context.Background(), CreatePlanRequest{Title: ""})
	if err == nil {
		t.Error("expected error for empty title")
	}
}

func TestValidation_TitleWhitespace(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.CreatePlan(context.Background(), CreatePlanRequest{Title: "   "})
	if err == nil {
		t.Error("expected error for whitespace-only title")
	}
}

func TestValidation_TodoContentRequired(t *testing.T) {
	svc := newTestService(t)
	plan, _ := svc.CreatePlan(context.Background(), CreatePlanRequest{Title: "Plan"})
	_, err := svc.CreateTodoItem(context.Background(), plan.ID, CreateTodoRequest{Content: ""})
	if err == nil {
		t.Error("expected error for empty todo content")
	}
}

func TestValidation_InvalidStatus(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	plan, _ := svc.CreatePlan(ctx, CreatePlanRequest{Title: "Plan"})

	invalidStatus := "invalid"
	_, err := svc.UpdatePlan(ctx, plan.ID, UpdatePlanRequest{Status: &invalidStatus})
	if err == nil {
		t.Error("expected error for invalid status")
	}
}

// ─── GetPlan with Todo Items ──────────────────────────────────────────────

func TestStore_GetPlan_WithDependencies(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	plan, _ := svc.CreatePlan(ctx, CreatePlanRequest{Title: "Dep Plan"})
	a, _ := svc.CreateTodoItem(ctx, plan.ID, CreateTodoRequest{Content: "A"})
	b, _ := svc.CreateTodoItem(ctx, plan.ID, CreateTodoRequest{Content: "B"})

	_ = svc.SetDependencies(ctx, b.ID, []string{a.ID})

	full, err := svc.GetPlan(ctx, plan.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if len(full.TodoItems) != 2 {
		t.Fatalf("expected 2 todo items, got %d", len(full.TodoItems))
	}

	// Find B and check dependencies
	for _, item := range full.TodoItems {
		if item.ID == b.ID {
			if len(item.DependsOn) != 1 || item.DependsOn[0] != a.ID {
				t.Errorf("B deps = %v, want [%s]", item.DependsOn, a.ID)
			}
		}
		if item.ID == a.ID {
			if len(item.Blockers) != 1 || item.Blockers[0] != b.ID {
				t.Errorf("A blockers = %v, want [%s]", item.Blockers, b.ID)
			}
		}
	}
}

// ─── Status Validation ────────────────────────────────────────────────────

func TestValidPlanStatus(t *testing.T) {
	tests := []struct {
		status string
		valid  bool
	}{
		{"plan", true},
		{"running", true},
		{"done", true},
		{"", false},
		{"invalid", false},
		{"PLAN", false},
	}
	for _, tt := range tests {
		if got := ValidPlanStatus(tt.status); got != tt.valid {
			t.Errorf("ValidPlanStatus(%q) = %v, want %v", tt.status, got, tt.valid)
		}
	}
}

// ─── ID Generation Override ────────────────────────────────────────────────

func TestStore_CustomID(t *testing.T) {
	oldID := newID
	newID = func() string { return "custom-id-001" }
	defer func() { newID = oldID }()

	svc := newTestService(t)
	plan, err := svc.CreatePlan(context.Background(), CreatePlanRequest{Title: "Custom ID"})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if plan.ID != "custom-id-001" {
		t.Errorf("ID = %q, want custom-id-001", plan.ID)
	}
}

// ─── Concurrency Safety ────────────────────────────────────────────────────

func TestStore_ConcurrentReads(t *testing.T) {
	store := newTestStore(t)
	// Sanity check: ListPlans should be safe for concurrent reads
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			_, _ = store.ListPlans(context.Background(), "")
			done <- true
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

// ─── Integration: Full Workflow ────────────────────────────────────────────

func TestFullWorkflow(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	// 1. Create a plan
	plan, err := svc.CreatePlan(ctx, CreatePlanRequest{
		Title:   "Build Feature X",
		Content: "## Description\nImplement feature X with proper testing.",
		Tags:    "feature,backend",
		Creator: "alice",
		TodoItems: []CreateTodoRequest{
			{Content: "Design API"},
			{Content: "Implement handler"},
			{Content: "Write tests"},
			{Content: "Code review"},
		},
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	// 2. Verify plan details
	full, err := svc.GetPlan(ctx, plan.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if len(full.TodoItems) != 4 {
		t.Fatalf("expected 4 items, got %d", len(full.TodoItems))
	}

	// Find items by content
	var designID, implID, testID, reviewID string
	for _, item := range full.TodoItems {
		switch item.Content {
		case "Design API":
			designID = item.ID
		case "Implement handler":
			implID = item.ID
		case "Write tests":
			testID = item.ID
		case "Code review":
			reviewID = item.ID
		}
	}

	// 3. Set dependencies: impl→design, test→impl, review→impl
	_ = svc.SetDependencies(ctx, implID, []string{designID})
	_ = svc.SetDependencies(ctx, testID, []string{implID})
	_ = svc.SetDependencies(ctx, reviewID, []string{implID})

	// 4. Start plan
	_, _ = svc.UpdatePlanStatus(ctx, plan.ID, StatusRunning)

	// 5. Complete design (no deps, should work)
	design, err := svc.ToggleTodoItem(ctx, designID)
	if err != nil {
		t.Fatalf("Toggle design: %v", err)
	}
	if !design.Completed {
		t.Error("design should be completed")
	}

	// 6. Try to complete test before impl — should fail
	_, err = svc.ToggleTodoItem(ctx, testID)
	if err == nil {
		t.Error("expected error: impl not completed")
	}

	// 7. Complete impl (design is done)
	_, _ = svc.ToggleTodoItem(ctx, implID)

	// 8. Now complete test and review (both depend on impl, which is done)
	test, err := svc.ToggleTodoItem(ctx, testID)
	if err != nil {
		t.Fatalf("Toggle test: %v", err)
	}
	if !test.Completed {
		t.Error("test should be completed")
	}

	_, err = svc.ToggleTodoItem(ctx, reviewID)
	if err != nil {
		t.Fatalf("Toggle review: %v", err)
	}

	// 9. Mark plan as done
	plan, err = svc.UpdatePlanStatus(ctx, plan.ID, StatusDone)
	if err != nil {
		t.Fatalf("UpdatePlanStatus done: %v", err)
	}
	if plan.Status != StatusDone {
		t.Errorf("expected done, got %s", plan.Status)
	}

	// 10. List done plans
	done, err := svc.ListPlans(ctx, "done")
	if err != nil {
		t.Fatalf("ListPlans(done): %v", err)
	}
	if len(done) != 1 {
		t.Errorf("expected 1 done plan, got %d", len(done))
	}
}

// ─── Edge Cases ────────────────────────────────────────────────────────────

func TestStore_EmptyDepsOnUpdate(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	plan, _ := svc.CreatePlan(ctx, CreatePlanRequest{Title: "Plan"})
	a, _ := svc.CreateTodoItem(ctx, plan.ID, CreateTodoRequest{Content: "A"})
	b, _ := svc.CreateTodoItem(ctx, plan.ID, CreateTodoRequest{Content: "B"})

	// Set deps
	_ = svc.SetDependencies(ctx, a.ID, []string{b.ID})

	// Clear deps
	err := svc.SetDependencies(ctx, a.ID, nil)
	if err != nil {
		t.Fatalf("SetDependencies(nil): %v", err)
	}

	deps, err := svc.GetDependencies(ctx, a.ID)
	if err != nil {
		t.Fatalf("GetDependencies: %v", err)
	}
	if len(deps.DependsOn) != 0 {
		t.Errorf("expected 0 deps after clearing, got %d", len(deps.DependsOn))
	}
}

func TestStore_ListPlans_EmptyStatus(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	// Empty DB
	plans, err := svc.ListPlans(ctx, "")
	if err != nil {
		t.Fatalf("ListPlans(empty): %v", err)
	}
	if len(plans) != 0 {
		t.Errorf("expected 0 plans from empty DB, got %d", len(plans))
	}
}

// Test that the DB path creation works when the directory doesn't exist
func TestNewStore_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "path")
	path := filepath.Join(dir, "entries.db")

	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore with non-existent parent dir: %v", err)
	}
	defer store.Close()

	// Verify directory was created
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("expected directory to be created")
	}
}
