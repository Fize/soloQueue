package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/cron"
	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
)

func newCronToolTestConfig(t *testing.T, scope CronAccessScope) Config {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "cron.db"))
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

func TestCronToolsRequireExplicitScope(t *testing.T) {
	cfg := newCronToolTestConfig(t, CronAccessScope{})
	for _, tool := range Build(cfg) {
		if IsCronTool(tool.Name()) {
			t.Fatalf("disabled scope unexpectedly registered %q", tool.Name())
		}
	}
}

func TestCronToolSchemasHideTargetFromTeamScope(t *testing.T) {
	for _, scope := range []CronAccessScope{
		{Mode: CronAccessGlobal},
		{Mode: CronAccessTeam, Owner: "engineering"},
	} {
		cfg := newCronToolTestConfig(t, scope)
		for _, name := range []string{"create_cron_job", "list_cron_jobs", "update_cron_job", "delete_cron_job"} {
			tool := findCronTool(t, cfg, name)
			var schema map[string]any
			if err := json.Unmarshal(tool.Parameters(), &schema); err != nil {
				t.Fatalf("%s schema is invalid: %v", name, err)
			}
			props, _ := schema["properties"].(map[string]any)
			_, exposesTarget := props["target_agent"]
			if scope.IsTeam() && exposesTarget {
				t.Fatalf("team-scoped %s exposes target_agent", name)
			}
		}
	}
}

func TestCronToolsRejectInvalidArguments(t *testing.T) {
	cfg := newCronToolTestConfig(t, CronAccessScope{Mode: CronAccessGlobal})
	tests := []struct {
		name string
		raw  string
	}{
		{name: "create_cron_job", raw: `{"title":"","task_type":"general","schedule":"0 9 * * 1","instruction":"run"}`},
		{name: "list_cron_jobs", raw: `{"status":"unknown"}`},
		{name: "update_cron_job", raw: `{}`},
		{name: "delete_cron_job", raw: `{}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := findCronTool(t, cfg, tt.name).Execute(context.Background(), tt.raw); err == nil {
				t.Fatalf("%s accepted invalid arguments", tt.name)
			}
		})
	}
}

func TestTeamCronToolsAreOwnerScoped(t *testing.T) {
	ctx := context.Background()
	cfg := newCronToolTestConfig(t, CronAccessScope{Mode: CronAccessTeam, Owner: "engineering"})

	create := findCronTool(t, cfg, "create_cron_job")
	createdRaw, err := create.Execute(ctx, `{"title":"Team report","task_type":"research","schedule":"0 9 * * 1","instruction":"Prepare report","target_agent":"finance"}`)
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

	listRaw, err := findCronTool(t, cfg, "list_cron_jobs").Execute(ctx, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listRaw, created.ID) || strings.Contains(listRaw, foreign.ID) {
		t.Fatalf("team list leaked or omitted jobs: %s", listRaw)
	}

	update := findCronTool(t, cfg, "update_cron_job")
	if _, err := update.Execute(ctx, `{"task_id":"`+foreign.ID+`","title":"stolen"}`); err == nil {
		t.Fatal("team update unexpectedly modified a foreign job")
	}
	if _, err := update.Execute(ctx, `{"task_id":"`+created.ID+`","title":"Updated","target_agent":"finance"}`); err != nil {
		t.Fatal(err)
	}
	task, _ = cfg.CronStore.GetTask(ctx, created.ID)
	if task.Title != "Updated" || task.TargetAgent != "engineering" {
		t.Fatalf("team update escaped scope: %+v", task)
	}

	deleteTool := findCronTool(t, cfg, "delete_cron_job")
	if _, err := deleteTool.Execute(ctx, `{"task_id":"`+foreign.ID+`"}`); err == nil {
		t.Fatal("team delete unexpectedly removed a foreign job")
	}
	if _, err := cfg.CronStore.GetTask(ctx, foreign.ID); err != nil {
		t.Fatalf("foreign job was deleted: %v", err)
	}
}

func TestCreateCronJobGlobalCanTargetAnyAgent(t *testing.T) {
	cfg := newCronToolTestConfig(t, CronAccessScope{Mode: CronAccessGlobal})
	create := findCronTool(t, cfg, "create_cron_job")
	raw, err := create.Execute(context.Background(), `{"title":"Finance report","task_type":"general","schedule":"0 9 * * 1","instruction":"Prepare report","target_agent":"finance"}`)
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

func TestListCronJobsFiltersByStatusAndTarget(t *testing.T) {
	ctx := context.Background()
	cfg := newCronToolTestConfig(t, CronAccessScope{Mode: CronAccessGlobal})
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

	raw, err := findCronTool(t, cfg, "list_cron_jobs").Execute(ctx, `{"status":"paused","target_agent":"engineering"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, pausedEngineering.ID) || strings.Contains(raw, activeEngineering.ID) || strings.Contains(raw, pausedFinance.ID) {
		t.Fatalf("unexpected filtered list: %s", raw)
	}
}

func TestUpdateCronJobUpdatesAllEditableFields(t *testing.T) {
	ctx := context.Background()
	cfg := newCronToolTestConfig(t, CronAccessScope{Mode: CronAccessGlobal})
	task, err := cfg.CronStore.CreateTask(ctx, cron.CreateTaskInput{
		Title: "Old title", TaskType: "general", Expression: "0 9 * * 1",
		Instruction: "Old instruction", TargetAgent: "engineering", NextRunAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	raw := `{"task_id":"` + task.ID + `","title":"New title","task_type":"engineering","schedule":"30 10 * * 2","instruction":"New instruction","target_agent":"finance","status":"paused"}`
	if _, err := findCronTool(t, cfg, "update_cron_job").Execute(ctx, raw); err != nil {
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

func TestDeleteCronJobRemovesJob(t *testing.T) {
	ctx := context.Background()
	cfg := newCronToolTestConfig(t, CronAccessScope{Mode: CronAccessGlobal})
	task, err := cfg.CronStore.CreateTask(ctx, cron.CreateTaskInput{
		Title: "Delete me", TaskType: "general", Expression: "0 9 * * 1",
		Instruction: "Delete test", TargetAgent: "L1", NextRunAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := findCronTool(t, cfg, "delete_cron_job").Execute(ctx, `{"task_id":"`+task.ID+`"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.CronStore.GetTask(ctx, task.ID); err == nil {
		t.Fatal("deleted cron job still exists")
	}
}
