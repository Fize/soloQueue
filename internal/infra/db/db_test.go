package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestOpen_Memory(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:) = %v", err)
	}
	defer db.Close()

	if db.DB == nil {
		t.Fatal("db.DB is nil")
	}
}

func TestOpen_File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%q) = %v", path, err)
	}
	// Verify the file was created.
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("database file not created: %v", statErr)
	}
	db.Close()
}

func TestOpen_SubdirectoryCreation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open should create subdirectory: %v", err)
	}
	db.Close()
}

func TestOpenMigratesLegacyScheduledTasksToTaskTypes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`
		CREATE TABLE scheduled_tasks (
			id TEXT PRIMARY KEY, expression TEXT NOT NULL, instruction TEXT NOT NULL,
			target_agent TEXT NOT NULL, status TEXT NOT NULL, last_run_at TEXT,
			next_run_at TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		);
	`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`INSERT INTO scheduled_tasks VALUES (?, ?, ?, ?, ?, NULL, ?, ?, ?)`,
		"t1", "daily", "Check database health\nInclude slow queries", "engineering", "active",
		"2026-07-18T09:00:00+08:00", "2026-07-17T09:00:00+08:00", "2026-07-17T09:00:00+08:00")
	if err != nil {
		t.Fatal(err)
	}
	raw.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var title, taskType string
	if err := db.QueryRow(`SELECT title, task_type FROM scheduled_tasks WHERE id = 't1'`).Scan(&title, &taskType); err != nil {
		t.Fatal(err)
	}
	if title != "Check database health" || taskType != "engineering" {
		t.Fatalf("unexpected migrated task: title=%q task_type=%q", title, taskType)
	}
}

func TestOpenMigratesScheduledTaskLevelsToTaskTypes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v3.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`
		CREATE TABLE scheduled_tasks (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL CHECK(length(trim(title)) > 0),
			task_level TEXT NOT NULL CHECK(task_level IN ('L0','L1','L2','L3')),
			expression TEXT NOT NULL, instruction TEXT NOT NULL, target_agent TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','paused','running','completed','failed')),
			last_run_at TEXT, next_run_at TEXT NOT NULL, created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		INSERT INTO scheduled_tasks VALUES (
			't1', 'Existing task', 'L3', 'daily', 'run', 'L1', 'active', NULL,
			'2026-07-18T09:00:00+08:00', '2026-07-17T09:00:00+08:00',
			'2026-07-17T09:00:00+08:00'
		);
	`)
	if err != nil {
		t.Fatal(err)
	}
	raw.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		INSERT INTO scheduled_tasks (
			id, title, task_type, expression, instruction, target_agent, status,
			next_run_at, created_at, updated_at
		) VALUES ('t2', 'Research task', 'research', 'daily', 'run', 'L1', 'active',
			'2026-07-19T09:00:00+08:00', '2026-07-17T09:00:00+08:00', '2026-07-17T09:00:00+08:00')
	`); err != nil {
		t.Fatalf("insert task type after migration: %v", err)
	}
	var existingTaskType string
	if err := db.QueryRow(`SELECT task_type FROM scheduled_tasks WHERE id = 't1'`).Scan(&existingTaskType); err != nil {
		t.Fatal(err)
	}
	if existingTaskType != "engineering" {
		t.Fatalf("existing task type changed to %q", existingTaskType)
	}
}

func TestOpenMigratesMemoryMetadataV8(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory-v7.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`
		CREATE TABLE mem_entries (
			id TEXT PRIMARY KEY, content TEXT NOT NULL, content_hash TEXT NOT NULL UNIQUE,
			date TEXT NOT NULL, tags TEXT NOT NULL DEFAULT '', event_time TEXT NOT NULL,
			salience REAL NOT NULL DEFAULT 1.0, last_recalled_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		);
		INSERT INTO mem_entries VALUES (
			'm1', 'legacy memory', 'hash1', '2026-07-01', '', '2026-07-01',
			1.0, '', '2026-07-01T00:00:00Z'
		);
	`)
	if err != nil {
		t.Fatal(err)
	}
	raw.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var memoryType, scopeType, status, updatedAt string
	if err := db.QueryRow(`
		SELECT memory_type, scope_type, status, updated_at
		FROM mem_entries WHERE id = 'm1'
	`).Scan(&memoryType, &scopeType, &status, &updatedAt); err != nil {
		t.Fatal(err)
	}
	if memoryType != "legacy" || scopeType != "global" || status != "active" ||
		updatedAt != "2026-07-01T00:00:00Z" {
		t.Fatalf("unexpected migrated metadata: %q %q %q %q",
			memoryType, scopeType, status, updatedAt)
	}
}

func TestInsertTokenUsage(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	err := db.InsertTokenUsage(context.Background(), "chat", "team-a", "test-model", 10, 5, 15, 0, 0)
	if err != nil {
		t.Fatalf("InsertTokenUsage: %v", err)
	}

	stats, err := db.GetTokenUsageAggregated(context.Background(), "daily", "team-a", "chat", "", "")
	if err != nil {
		t.Fatalf("GetTokenUsageAggregated: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat row, got %d", len(stats))
	}
	if stats[0].TotalTokens != 15 {
		t.Errorf("TotalTokens = %d, want 15", stats[0].TotalTokens)
	}
	if stats[0].PromptTokens != 10 {
		t.Errorf("PromptTokens = %d, want 10", stats[0].PromptTokens)
	}
	if stats[0].CompletionTokens != 5 {
		t.Errorf("CompletionTokens = %d, want 5", stats[0].CompletionTokens)
	}
	if stats[0].ModelName != "test-model" {
		t.Errorf("ModelName = %q, want test-model", stats[0].ModelName)
	}
}

func TestInsertTokenUsage_FilterByTeam(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.InsertTokenUsage(context.Background(), "chat", "team-a", "m1", 10, 5, 15, 0, 0)
	db.InsertTokenUsage(context.Background(), "chat", "team-b", "m1", 20, 10, 30, 0, 0)

	stats, err := db.GetTokenUsageAggregated(context.Background(), "daily", "team-a", "", "", "")
	if err != nil {
		t.Fatalf("GetTokenUsageAggregated: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat row for team-a, got %d", len(stats))
	}
	if stats[0].TeamID != "team-a" {
		t.Errorf("TeamID = %q, want team-a", stats[0].TeamID)
	}
}

func TestInsertTokenUsage_MultipleModels(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.InsertTokenUsage(context.Background(), "chat", "team-a", "m1", 10, 5, 15, 0, 0)
	db.InsertTokenUsage(context.Background(), "chat", "team-a", "m1", 5, 3, 8, 0, 0)
	db.InsertTokenUsage(context.Background(), "chat", "team-a", "m2", 100, 50, 150, 0, 0)

	stats, err := db.GetTokenUsageAggregated(context.Background(), "daily", "", "", "", "")
	if err != nil {
		t.Fatalf("GetTokenUsageAggregated: %v", err)
	}
	if len(stats) != 2 {
		t.Fatalf("expected 2 model groups, got %d", len(stats))
	}
	totalTokens := stats[0].TotalTokens + stats[1].TotalTokens
	if totalTokens != 173 {
		t.Errorf("total = %d, want 173 (15+8+150)", totalTokens)
	}
}

func TestInsertTokenUsage_CacheTokens(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	err := db.InsertTokenUsage(context.Background(), "chat", "team-a", "m1", 100, 50, 150, 80, 20)
	if err != nil {
		t.Fatalf("InsertTokenUsage: %v", err)
	}

	stats, err := db.GetTokenUsageAggregated(context.Background(), "daily", "team-a", "", "", "")
	if err != nil {
		t.Fatalf("GetTokenUsageAggregated: %v", err)
	}
	if stats[0].CacheHitTokens != 80 {
		t.Errorf("CacheHitTokens = %d, want 80", stats[0].CacheHitTokens)
	}
	if stats[0].CacheMissTokens != 20 {
		t.Errorf("CacheMissTokens = %d, want 20", stats[0].CacheMissTokens)
	}
}

func TestInsertRouterClassification(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	err := db.InsertRouterClassification(context.Background(), "router", "team-a", "L2-MediumMultiFile", "llm")
	if err != nil {
		t.Fatalf("InsertRouterClassification: %v", err)
	}

	stats, err := db.GetRouterStatsAggregated(context.Background(), "daily", "team-a", "", "")
	if err != nil {
		t.Fatalf("GetRouterStatsAggregated: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat row, got %d", len(stats))
	}
	if stats[0].Count != 1 {
		t.Errorf("Count = %d, want 1", stats[0].Count)
	}
	if stats[0].ClassificationLevel != "L2-MediumMultiFile" {
		t.Errorf("Level = %q, want L2-MediumMultiFile", stats[0].ClassificationLevel)
	}
}

func TestGetDistinctTeams(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	db.InsertTokenUsage(context.Background(), "chat", "team-a", "m1", 1, 1, 2, 0, 0)
	db.InsertTokenUsage(context.Background(), "chat", "team-b", "m1", 1, 1, 2, 0, 0)
	db.InsertTokenUsage(context.Background(), "chat", "team-a", "m1", 1, 1, 2, 0, 0)

	teams, err := db.GetDistinctTeams(context.Background())
	if err != nil {
		t.Fatalf("GetDistinctTeams: %v", err)
	}
	if len(teams) != 2 {
		t.Fatalf("expected 2 teams, got %d: %v", len(teams), teams)
	}
}

func TestGetDistinctTeams_Empty(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	teams, err := db.GetDistinctTeams(context.Background())
	if err != nil {
		t.Fatalf("GetDistinctTeams: %v", err)
	}
	if len(teams) != 0 {
		t.Errorf("expected 0 teams, got %d", len(teams))
	}
}

func TestGetClassifierStatsAggregated_AvgFloat(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	// Two decisions with ft_confidence 50 and 60 => avg = 55.0
	if err := db.InsertClassifierDecision(context.Background(), ClassifierDecision{FTConfidence: 50, LLMInvoked: 0, FinalSource: "fast-track"}); err != nil {
		t.Fatalf("InsertClassifierDecision: %v", err)
	}
	if err := db.InsertClassifierDecision(context.Background(), ClassifierDecision{FTConfidence: 60, LLMInvoked: 0, FinalSource: "fast-track"}); err != nil {
		t.Fatalf("InsertClassifierDecision: %v", err)
	}

	stats, err := db.GetClassifierStatsAggregated(context.Background(), "daily", "", "")
	if err != nil {
		t.Fatalf("GetClassifierStatsAggregated: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat row, got %d", len(stats))
	}
	if stats[0].AvgFTConf != 55.0 {
		t.Errorf("AvgFTConf = %v, want 55.0", stats[0].AvgFTConf)
	}
}

func TestInsertTokenUsage_Concurrent(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	const n = 10
	done := make(chan struct{})
	for i := 0; i < n; i++ {
		go func(id int) {
			db.InsertTokenUsage(context.Background(), "chat", "team-c", "mm", 1, 1, 2, 0, 0)
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < n; i++ {
		<-done
	}

	stats, err := db.GetTokenUsageAggregated(context.Background(), "daily", "team-c", "", "", "")
	if err != nil {
		t.Fatalf("GetTokenUsageAggregated: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat row, got %d", len(stats))
	}
	if stats[0].TotalTokens != n*2 {
		t.Errorf("TotalTokens = %d, want %d", stats[0].TotalTokens, n*2)
	}
}

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:) = %v", err)
	}
	return db
}
