package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/cron"
	"github.com/xiaobaitu/soloqueue/internal/infra/db"
)

func newCronToolTestConfig(t *testing.T, scope CronAccessScope) Config {
	t.Helper()
	db, err := db.Open(filepath.Join(t.TempDir(), "cron.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := cron.NewDBStore(db)
	return Config{
		CronStore: store, CronScheduler: cron.NewScheduler(store, nil, nil), CronScope: scope,
	}
}

func findCronTool(t *testing.T, cfg Config, name string) Tool {
	t.Helper()
	for _, tool := range Build(cfg) {
		if tool.Name() == name {
			return tool
		}
	}
	t.Fatalf("tool %q not registered", name)
	return nil
}

func TestManageCron_SchemasHideTargetFromTeamScope(t *testing.T) {
	for _, scope := range []CronAccessScope{
		{Mode: CronAccessGlobal},
		{Mode: CronAccessTeam, Owner: "engineering"},
	} {
		cfg := newCronToolTestConfig(t, scope)
		tool := findCronTool(t, cfg, "manage_cron")
		var schema map[string]any
		if err := json.Unmarshal(tool.Parameters(), &schema); err != nil {
			t.Fatalf("manage_cron schema is invalid: %v", err)
		}
		props, _ := schema["properties"].(map[string]any)
		_, exposesTarget := props["target_agent"]
		if scope.IsTeam() && exposesTarget {
			t.Fatalf("team-scoped manage_cron exposes target_agent")
		}
	}
}

func TestManageCron_RejectInvalidArguments(t *testing.T) {
	cfg := newCronToolTestConfig(t, CronAccessScope{Mode: CronAccessGlobal})
	tool := findCronTool(t, cfg, "manage_cron")
	tests := []struct {
		name string
		raw  string
	}{
		{name: "invalid action", raw: `{"action":"unknown"}`},
		{name: "create invalid title", raw: `{"action":"create","title":"","task_type":"general","schedule":"0 9 * * 1","instruction":"run"}`},
		{name: "list invalid status", raw: `{"action":"list","status":"unknown"}`},
		{name: "update missing id", raw: `{"action":"update"}`},
		{name: "delete missing id", raw: `{"action":"delete"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tool.Execute(context.Background(), tt.raw); err == nil {
				t.Fatalf("%s accepted invalid arguments", tt.name)
			}
		})
	}
}

func TestManageCron_TeamToolsAreOwnerScoped(t *testing.T) {
	ctx := context.Background()
	cfg := newCronToolTestConfig(t, CronAccessScope{Mode: CronAccessTeam, Owner: "engineering"})
	manage := findCronTool(t, cfg, "manage_cron")

	createdRaw, err := manage.Execute(ctx, `{"action":"create","title":"Team report","task_type":"research","schedule":"0 9 * * 1","instruction":"Prepare report","target_agent":"finance"}`)
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(createdRaw), &created); err != nil {
		t.Fatal(err)
	}
	task, err := cfg.CronStore.GetTask(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.TargetAgent != "engineering" {
		t.Fatalf("team create target = %q, want engineering", task.TargetAgent)
	}

	foreign, err := cfg.CronStore.CreateTask(ctx, cron.CreateTaskInput{
		Title: "Finance report", TaskType: "general", Expression: "0 10 * * 1",
		Instruction: "Prepare finance report", TargetAgent: "finance", NextRunAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	listRaw, err := manage.Execute(ctx, `{"action":"list"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listRaw, created.ID) || strings.Contains(listRaw, foreign.ID) {
		t.Fatalf("team list leaked or omitted jobs: %s", listRaw)
	}

	if _, err := manage.Execute(ctx, `{"action":"update","task_id":"`+foreign.ID+`","title":"stolen"}`); err == nil {
		t.Fatal("team update unexpectedly modified a foreign job")
	}
	if _, err := manage.Execute(ctx, `{"action":"update","task_id":"`+created.ID+`","title":"Updated","target_agent":"finance"}`); err != nil {
		t.Fatal(err)
	}
	task, _ = cfg.CronStore.GetTask(ctx, created.ID)
	if task.Title != "Updated" || task.TargetAgent != "engineering" {
		t.Fatalf("team update escaped scope: %+v", task)
	}

	if _, err := manage.Execute(ctx, `{"action":"delete","task_id":"`+foreign.ID+`"}`); err == nil {
		t.Fatal("team delete unexpectedly removed a foreign job")
	}
	if _, err := cfg.CronStore.GetTask(ctx, foreign.ID); err != nil {
		t.Fatalf("foreign job was deleted: %v", err)
	}
}

func TestManageCron_GlobalCanTargetAnyAgent(t *testing.T) {
	cfg := newCronToolTestConfig(t, CronAccessScope{Mode: CronAccessGlobal})
	manage := findCronTool(t, cfg, "manage_cron")
	raw, err := manage.Execute(context.Background(), `{"action":"create","title":"Finance report","task_type":"general","schedule":"0 9 * * 1","instruction":"Prepare report","target_agent":"finance"}`)
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal([]byte(raw), &created)
	task, err := cfg.CronStore.GetTask(context.Background(), created.ID)
	if err != nil || task.TargetAgent != "finance" {
		t.Fatalf("global create target: task=%+v err=%v", task, err)
	}
}

func TestManageCron_ListFiltersByStatusAndTarget(t *testing.T) {
	ctx := context.Background()
	cfg := newCronToolTestConfig(t, CronAccessScope{Mode: CronAccessGlobal})
	manage := findCronTool(t, cfg, "manage_cron")
	createTask := func(title, target string) *cron.Task {
		t.Helper()
		task, err := cfg.CronStore.CreateTask(ctx, cron.CreateTaskInput{
			Title: title, TaskType: "general", Expression: "0 9 * * 1",
			Instruction: "Run " + title, TargetAgent: target, NextRunAt: time.Now().Add(time.Hour),
		})
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	pausedEngineering := createTask("Paused engineering", "engineering")
	activeEngineering := createTask("Active engineering", "engineering")
	pausedFinance := createTask("Paused finance", "finance")
	if err := cfg.CronStore.UpdateTaskStatus(ctx, pausedEngineering.ID, "paused"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.CronStore.UpdateTaskStatus(ctx, pausedFinance.ID, "paused"); err != nil {
		t.Fatal(err)
	}

	raw, err := manage.Execute(ctx, `{"action":"list","status":"paused","target_agent":"engineering"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, pausedEngineering.ID) || strings.Contains(raw, activeEngineering.ID) || strings.Contains(raw, pausedFinance.ID) {
		t.Fatalf("unexpected filtered list: %s", raw)
	}
}

func TestManageCron_UpdateUpdatesAllEditableFields(t *testing.T) {
	ctx := context.Background()
	cfg := newCronToolTestConfig(t, CronAccessScope{Mode: CronAccessGlobal})
	manage := findCronTool(t, cfg, "manage_cron")
	task, err := cfg.CronStore.CreateTask(ctx, cron.CreateTaskInput{
		Title: "Old title", TaskType: "general", Expression: "0 9 * * 1",
		Instruction: "Old instruction", TargetAgent: "engineering", NextRunAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	raw := `{"action":"update","task_id":"` + task.ID + `","title":"New title","task_type":"engineering","schedule":"30 10 * * 2","instruction":"New instruction","target_agent":"finance","status":"paused"}`
	if _, err := manage.Execute(ctx, raw); err != nil {
		t.Fatal(err)
	}
	updated, err := cfg.CronStore.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "New title" || updated.TaskType != "engineering" || updated.Expression != "30 10 * * 2" ||
		updated.Instruction != "New instruction" || updated.TargetAgent != "finance" || updated.Status != "paused" {
		t.Fatalf("unexpected updated job: %+v", updated)
	}
}

func TestManageCron_DeleteRemovesJob(t *testing.T) {
	ctx := context.Background()
	cfg := newCronToolTestConfig(t, CronAccessScope{Mode: CronAccessGlobal})
	manage := findCronTool(t, cfg, "manage_cron")
	task, err := cfg.CronStore.CreateTask(ctx, cron.CreateTaskInput{
		Title: "Delete me", TaskType: "general", Expression: "0 9 * * 1",
		Instruction: "Delete test", TargetAgent: "L1", NextRunAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manage.Execute(ctx, `{"action":"delete","task_id":"`+task.ID+`"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.CronStore.GetTask(ctx, task.ID); err == nil {
		t.Fatal("deleted cron job still exists")
	}
}
