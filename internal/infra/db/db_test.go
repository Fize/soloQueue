package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
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
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(path)
	if err != nil {
		t.Fatalf("Open current schema = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
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

func TestOpenRejectsIncompatibleMCPPolicySchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "incompatible.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`
		CREATE TABLE mcp_policies (
			scope TEXT NOT NULL,
			server_name TEXT NOT NULL,
			state TEXT NOT NULL,
			revision INTEGER NOT NULL DEFAULT 1,
			definition_digest TEXT NOT NULL,
			approved_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT (datetime('now')),
			PRIMARY KEY (scope, server_name)
		);
		INSERT INTO mcp_policies (
			scope, server_name, state, definition_digest, approved_at
		) VALUES ('global', 'fixture', 'approved', 'digest', '2026-08-27T00:00:00Z');
		PRAGMA user_version = 20;
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	database, err := Open(path)
	if err == nil {
		database.Close()
		t.Fatal("Open accepted an incompatible MCP policy schema")
	}
	if !strings.Contains(err.Error(), "recreate the database") {
		t.Fatalf("Open error = %q, want recreate-database guidance", err)
	}

	raw, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var version int
	if err := raw.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 20 {
		t.Fatalf("schema version = %d, want 20", version)
	}
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
	var memoryType, scopeType, status, updatedAt, ownerType, ownerID string
	if err := db.QueryRow(`
		SELECT memory_type, scope_type, status, updated_at, owner_type, owner_id
		FROM mem_entries WHERE id = 'm1'
	`).Scan(&memoryType, &scopeType, &status, &updatedAt, &ownerType, &ownerID); err != nil {
		t.Fatal(err)
	}
	if memoryType != "legacy" || scopeType != "global" || status != "active" ||
		updatedAt != "2026-07-01T00:00:00Z" || ownerType != "l1" || ownerID != "" {
		t.Fatalf("unexpected migrated metadata: %q %q %q %q %q %q",
			memoryType, scopeType, status, updatedAt, ownerType, ownerID)
	}
}

func TestOpenMigratesMemoryRevisionMetadataV20(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory-v19.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`
		CREATE TABLE mem_entries (
			id TEXT PRIMARY KEY, content TEXT NOT NULL, content_hash TEXT NOT NULL UNIQUE,
			date TEXT NOT NULL, tags TEXT NOT NULL DEFAULT '', event_time TEXT NOT NULL,
			salience REAL NOT NULL DEFAULT 1.0, last_recalled_at TEXT NOT NULL DEFAULT '',
			memory_type TEXT NOT NULL DEFAULT 'legacy', scope_type TEXT NOT NULL DEFAULT 'global',
			scope_id TEXT NOT NULL DEFAULT '', source_type TEXT NOT NULL DEFAULT 'legacy',
			source_id TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'active',
			confidence REAL NOT NULL DEFAULT 1.0, expires_at TEXT NOT NULL DEFAULT '',
			supersedes_hash TEXT NOT NULL DEFAULT '', canonical_hash TEXT NOT NULL DEFAULT '',
			recall_count INTEGER NOT NULL DEFAULT 0, used_count INTEGER NOT NULL DEFAULT 0,
			last_used_at TEXT NOT NULL DEFAULT '', owner_type TEXT NOT NULL DEFAULT 'l1',
			owner_id TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL, created_at TEXT NOT NULL
		);
		INSERT INTO mem_entries (
			id, content, content_hash, date, event_time, memory_type, status, updated_at, created_at
		) VALUES
			('active', 'current value', 'active-hash', '2026-08-01', '2026-08-01T09:00:00Z',
			 'stable_fact', 'active', '2026-08-01T09:00:00Z', '2026-08-01T09:00:00Z'),
			('old', 'old value', 'old-hash', '2026-07-01', '2026-07-01T09:00:00Z',
			 'stable_fact', 'superseded', '2026-08-01T09:00:00Z', '2026-07-01T09:00:00Z');
	`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`PRAGMA user_version = 19`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	rows, err := database.Query(`
		SELECT id, subject_key, valid_from, valid_until
		FROM mem_entries ORDER BY id
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	type revisionMetadata struct {
		id, subjectKey, validFrom, validUntil string
	}
	var got []revisionMetadata
	for rows.Next() {
		var row revisionMetadata
		if err := rows.Scan(&row.id, &row.subjectKey, &row.validFrom, &row.validUntil); err != nil {
			t.Fatal(err)
		}
		got = append(got, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []revisionMetadata{
		{id: "active", validFrom: "2026-08-01T09:00:00Z"},
		{id: "old", validFrom: "2026-07-01T09:00:00Z", validUntil: "2026-08-01T09:00:00Z"},
	}
	if len(got) != len(want) {
		t.Fatalf("revision metadata row count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("revision metadata row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	var version int
	if err := database.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != schemaVersion {
		t.Fatalf("schema version = %d, want %d", version, schemaVersion)
	}
}

func TestMemoryRevisionSubjectIndexRejectsDuplicateActiveMutableMemory(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	insert := `INSERT INTO mem_entries (
		id, content, content_hash, date, event_time, memory_type, scope_type, scope_id,
		source_type, status, owner_type, owner_id, subject_key, valid_from
	) VALUES (?, ?, ?, '2026-08-01', '2026-08-01T09:00:00Z',
		'stable_fact', 'project', '/work/project', 'agent', 'active', 'l1', '',
		'project.runtime.go_version', '2026-08-01T09:00:00Z')`
	if _, err := database.Exec(insert, "m1", "Go 1.24", "hash-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(insert, "m2", "Go 1.25.8", "hash-2"); err == nil {
		t.Fatal("duplicate active mutable subject was accepted")
	}
}

func TestOpenMigratesExistingKnowledgeGraphToL1Owner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory-v18.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`
		CREATE TABLE kg_nodes (
			id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE,
			type TEXT NOT NULL DEFAULT 'entity', mention_count INTEGER NOT NULL DEFAULT 1,
			first_seen TEXT NOT NULL, last_seen TEXT NOT NULL, confidence REAL NOT NULL DEFAULT 1.0
		);
		CREATE TABLE kg_edges (
			id INTEGER PRIMARY KEY AUTOINCREMENT, source INTEGER NOT NULL, target INTEGER NOT NULL,
			rel_type TEXT NOT NULL, weight REAL NOT NULL DEFAULT 1.0, evidence TEXT NOT NULL DEFAULT '',
			source_hash TEXT NOT NULL DEFAULT '', event_time TEXT NOT NULL, valid_from TEXT NOT NULL DEFAULT '',
			valid_until TEXT, last_reinforced TEXT NOT NULL DEFAULT '', UNIQUE(source, target, rel_type)
		);
		CREATE TABLE kg_aliases (
			id INTEGER PRIMARY KEY AUTOINCREMENT, alias TEXT NOT NULL, canonical TEXT NOT NULL,
			UNIQUE(alias, canonical)
		);
		INSERT INTO kg_nodes VALUES (1, 'SoloQueue', 'project', 1, '2026-01-01', '2026-01-01', 1.0);
		INSERT INTO kg_nodes VALUES (2, 'SQLite', 'tool', 1, '2026-01-01', '2026-01-01', 1.0);
		INSERT INTO kg_edges VALUES (1, 1, 2, 'uses', 1.0, '', 'hash', '2026-01-01', '', NULL, '2026-01-01');
		INSERT INTO kg_aliases VALUES (1, 'SQ', 'SoloQueue');
	`)
	if err != nil {
		t.Fatal(err)
	}
	raw.Close()

	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, table := range []string{"kg_nodes", "kg_edges", "kg_aliases"} {
		var count int
		query := `SELECT COUNT(*) FROM ` + table + ` WHERE owner_type = 'l1' AND owner_id = ''`
		if err := database.QueryRow(query).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count == 0 {
			t.Fatalf("%s rows were not assigned to L1", table)
		}
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
