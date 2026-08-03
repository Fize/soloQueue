package engine

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) (*sql.DB, *sync.Mutex) {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", dir+"/test.db")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS mem_entries (
			id TEXT PRIMARY KEY, content TEXT NOT NULL, content_hash TEXT NOT NULL UNIQUE,
			date TEXT NOT NULL, tags TEXT NOT NULL DEFAULT '', event_time TEXT NOT NULL,
			salience REAL NOT NULL DEFAULT 1.0, last_recalled_at TEXT NOT NULL DEFAULT '',
			memory_type TEXT NOT NULL DEFAULT 'legacy', scope_type TEXT NOT NULL DEFAULT 'global',
			scope_id TEXT NOT NULL DEFAULT '', source_type TEXT NOT NULL DEFAULT 'legacy',
			source_id TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'active',
			confidence REAL NOT NULL DEFAULT 1.0, expires_at TEXT NOT NULL DEFAULT '',
			supersedes_hash TEXT NOT NULL DEFAULT '', canonical_hash TEXT NOT NULL DEFAULT '',
			recall_count INTEGER NOT NULL DEFAULT 0, used_count INTEGER NOT NULL DEFAULT 0,
			last_used_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT (datetime('now')),
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS mem_fts USING fts5(content, date, content='mem_entries', content_rowid='rowid', tokenize='unicode61')`,
		`CREATE TRIGGER IF NOT EXISTS mem_fts_ai AFTER INSERT ON mem_entries BEGIN INSERT INTO mem_fts(rowid, content, date) VALUES (new.rowid, new.content, new.date); END`,
		`CREATE TABLE IF NOT EXISTS kg_nodes (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, type TEXT NOT NULL DEFAULT 'entity', mention_count INTEGER NOT NULL DEFAULT 1, first_seen TEXT NOT NULL, last_seen TEXT NOT NULL, confidence REAL NOT NULL DEFAULT 1.0)`,
		`CREATE TABLE IF NOT EXISTS kg_edges (id INTEGER PRIMARY KEY AUTOINCREMENT, source INTEGER NOT NULL, target INTEGER NOT NULL, rel_type TEXT NOT NULL, weight REAL NOT NULL DEFAULT 1.0, evidence TEXT NOT NULL DEFAULT '', source_hash TEXT NOT NULL DEFAULT '', event_time TEXT NOT NULL, valid_from TEXT NOT NULL DEFAULT '', valid_until TEXT, last_reinforced TEXT NOT NULL DEFAULT '', UNIQUE(source, target, rel_type))`,
		`CREATE TABLE IF NOT EXISTS kg_aliases (id INTEGER PRIMARY KEY AUTOINCREMENT, alias TEXT NOT NULL, canonical TEXT NOT NULL, UNIQUE(alias, canonical))`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("exec schema: %v\nSQL: %s", err, s)
		}
	}

	t.Cleanup(func() { db.Close() })
	return db, &sync.Mutex{}
}

func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	db, mu := openTestDB(t)
	log, err := logger.System(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	t.Cleanup(func() { log.Close() })
	return New(db, mu, nil, nil, log)
}

func TestEngine_Save(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()

	hash, isNew, err := e.Save(ctx, "Hello world, this is a test memory", "2026-01-01", "test", "2026-01-01T10:00:00Z")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if hash == "" {
		t.Error("Save returned empty hash")
	}
	if !isNew {
		t.Error("first save should be new")
	}

	_, isNew2, err := e.Save(ctx, "Hello world, this is a test memory", "2026-01-01", "test", "2026-01-01T10:00:00Z")
	if err != nil {
		t.Fatalf("Save again: %v", err)
	}
	if isNew2 {
		t.Error("duplicate save should not be new")
	}
}

func TestEngine_SaveWithEntities(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()

	entities := []EntityExtraction{
		{
			Name: "Alice", Type: "person",
			Relations: []RelationExtraction{
				{TargetName: "Bob", RelType: "friend", Weight: 0.8},
			},
		},
		{Name: "Bob", Type: "person"},
	}

	hash, isNew, err := e.SaveWithEntities(ctx, "Alice met Bob today", "2026-01-01", "test", "2026-01-01T12:00:00Z", entities)
	if err != nil {
		t.Fatalf("SaveWithEntities: %v", err)
	}
	if hash == "" {
		t.Error("SaveWithEntities returned empty hash")
	}
	if !isNew {
		t.Error("first save should be new")
	}
}

func TestEngine_Search(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()

	e.Save(ctx, "Alice went to the store", "2026-01-01", "test", "2026-01-01")
	e.Save(ctx, "Bob went to the park", "2026-01-02", "test", "2026-01-02")

	results, err := e.Search(ctx, SearchQuery{Text: "Alice store", Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results.Results) == 0 {
		t.Error("Search returned no results")
	}
}

func TestEngine_Search_EmptyResults(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()

	results, err := e.Search(ctx, SearchQuery{Text: "nonexistent", Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results.Results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results.Results))
	}
}

func TestEngine_IndexEntity(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()

	id, err := e.IndexEntity(ctx, "test-entity", "test_type")
	if err != nil {
		t.Fatalf("IndexEntity: %v", err)
	}
	if id <= 0 {
		t.Errorf("IndexEntity returned invalid id: %d", id)
	}

	id2, err := e.IndexEntity(ctx, "test-entity", "test_type")
	if err != nil {
		t.Fatalf("IndexEntity again: %v", err)
	}
	if id != id2 {
		t.Errorf("re-index returned different id: %d vs %d", id, id2)
	}
}

func TestEngine_ConnectEntities(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()

	id1, _ := e.IndexEntity(ctx, "X", "person")
	id2, _ := e.IndexEntity(ctx, "Y", "person")

	now := time.Now()
	err := e.ConnectEntities(ctx, "X", "Y", "friend", 0.8, "evidence", "hash123", now, nil, nil)
	if err != nil {
		t.Fatalf("ConnectEntities: %v", err)
	}

	edges, err := e.graph.GetEdgesFrom(ctx, id1, true)
	if err != nil {
		t.Fatalf("GetEdgesFrom: %v", err)
	}
	if len(edges) == 0 {
		edges, err = e.graph.GetEdgesTo(ctx, id1, true)
		if err != nil {
			t.Fatalf("GetEdgesTo: %v", err)
		}
	}
	if len(edges) == 0 {
		t.Error("expected at least 1 edge connecting X and Y, got 0")
	}
	_ = id2
}

func TestEngine_Timeline(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()

	e.Save(ctx, "Day 1 memory", "2026-01-01", "", "2026-01-01")
	e.Save(ctx, "Day 2 memory", "2026-01-02", "", "2026-01-02")

	entries, err := e.Timeline(ctx, "2026-01-01", "2026-12-31", 10)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if len(entries) < 1 {
		t.Error("Timeline returned no entries")
	}
}

func TestEngine_Consolidate(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()

	e.Save(ctx, "Memory 1 about AI", "2026-01-01", "", "2026-01-01")
	e.Save(ctx, "Memory 2 about coding", "2026-01-02", "", "2026-01-02")

	report, err := e.Consolidate(ctx)
	if err != nil {
		t.Fatalf("Consolidate: %v", err)
	}
	if report == nil {
		t.Fatal("Consolidate returned nil report")
	}
}

func TestEngine_BoostSalience(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()

	hash, _, _ := e.Save(ctx, "important memory", "2026-01-01", "", "2026-01-01")
	err := e.BoostSalience(ctx, hash)
	if err != nil {
		t.Fatalf("BoostSalience: %v", err)
	}
}

func TestEngine_Save_Concurrent(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()

	done := make(chan struct{}, 5)
	for i := 0; i < 5; i++ {
		go func(n int) {
			content := "concurrent memory " + string(rune('A'+n))
			_, _, err := e.Save(ctx, content, "2026-01-01", "", "2026-01-01")
			if err != nil {
				t.Errorf("concurrent save %d: %v", n, err)
			}
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 5; i++ {
		<-done
	}

	entries, err := e.Timeline(ctx, "2026-01-01", "2026-12-31", 10)
	if err != nil {
		t.Fatalf("Timeline after concurrent saves: %v", err)
	}
	if len(entries) != 5 {
		t.Errorf("expected 5 entries after concurrent saves, got %d", len(entries))
	}
}

func TestEbbinghausSalience(t *testing.T) {
	// daysSinceRecall=1, halfLifeDays=30 (default): slight decay
	s := EbbinghausSalience(1.0, 1, 0.0)
	if s <= 0 || s >= 1.0 {
		t.Errorf("slight decay salience should be < 1.0, got %f", s)
	}

	// daysSinceRecall=30, halfLifeDays=30: decay to ~1/e ≈ 0.368
	s2 := EbbinghausSalience(1.0, 30, 30.0)
	if s2 <= 0 || s2 >= 1.0 {
		t.Errorf("decayed salience should be between 0 and 1, got %f", s2)
	}

	// daysSinceRecall=0: no decay
	s3 := EbbinghausSalience(1.0, 0, 30.0)
	if s3 != 1.0 {
		t.Errorf("zero-day recall should return 1.0, got %f", s3)
	}
}

func TestEngine_RecallEntity(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()

	hash, _, err := e.SaveWithEntities(ctx, "Alice discussed the project",
		"2026-01-01", "test", "2026-01-01T12:00:00Z",
		[]EntityExtraction{
			{Name: "Alice", Type: "person",
				Relations: []RelationExtraction{
					{TargetName: "Bob", RelType: "colleague", Weight: 0.9},
				}},
			{Name: "Bob", Type: "person"},
		})
	if err != nil {
		t.Fatalf("SaveWithEntities: %v", err)
	}
	_ = hash

	results, err := e.RecallEntity(ctx, "Alice", 2, 10)
	if err != nil {
		t.Fatalf("RecallEntity: %v", err)
	}
	t.Logf("RecallEntity returned %d results", len(results))
}

func TestEngine_RecallEntity_NotFound(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()

	results, err := e.RecallEntity(ctx, "NoSuchEntity", 2, 10)
	if err != nil {
		t.Fatalf("RecallEntity: %v", err)
	}
	if len(results) != 0 {
		t.Error("RecallEntity for missing entity should return empty results")
	}
}

func TestEngine_ShortestPath(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()

	if _, err := e.IndexEntity(ctx, "N1", "node"); err != nil {
		t.Fatalf("IndexEntity N1: %v", err)
	}
	if _, err := e.IndexEntity(ctx, "N2", "node"); err != nil {
		t.Fatalf("IndexEntity N2: %v", err)
	}
	if _, err := e.IndexEntity(ctx, "N3", "node"); err != nil {
		t.Fatalf("IndexEntity N3: %v", err)
	}

	now := time.Now()
	if err := e.ConnectEntities(ctx, "N1", "N2", "link", 1.0, "", "", now, nil, nil); err != nil {
		t.Fatalf("Connect N1→N2: %v", err)
	}
	if err := e.ConnectEntities(ctx, "N2", "N3", "link", 1.0, "", "", now, nil, nil); err != nil {
		t.Fatalf("Connect N2→N3: %v", err)
	}

	nodes, pathEdges, err := e.ShortestPath(ctx, "N1", "N3", 3)
	if err != nil {
		t.Fatalf("ShortestPath: %v", err)
	}
	if len(nodes) == 0 {
		t.Error("expected nodes in path")
	}
	if len(pathEdges) == 0 {
		t.Error("expected edges in path")
	}
}
