// Package sqlitedb provides a shared SQLite database connection with
// centralized schema migrations. Both the permanent memory vector store
// and the todo/plan store open the same physical file (entries.db), so
// opening it from multiple places causes DDL races and fragmented
// migrations. This package exposes a single *sql.DB and a shared write
// mutex that callers use to serialize writes across stores.
package sqlitedb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

// currentSchemaVersion is the latest schema version. Bump this when adding
// migrations to the migrations slice.
const currentSchemaVersion = 14

// DB wraps a shared *sql.DB together with a write mutex used to serialize
// writes across all logical stores that share the same underlying SQLite
// file (SQLite allows only a single writer at a time).
type DB struct {
	*sql.DB
	// WMu must be acquired by any store performing a write operation that
	// needs to be serialized with writes from other stores on the same
	// database file. Reads do not need to acquire it (WAL allows concurrent
	// readers).
	WMu sync.Mutex
}

// Open opens (or creates) the shared SQLite database at the given path and
// runs all pending migrations. It should be called exactly once per process.
// The caller owns the returned *DB and is responsible for calling Close.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("sqlitedb: mkdir: %w", err)
	}

	// WAL for concurrent readers + busy_timeout so competing writers wait
	// rather than returning SQLITE_BUSY immediately. foreign_keys=ON for
	// referential integrity on kg_edges (REFERENCES kg_nodes).
	dsn := path + "?_journal_mode=WAL&_foreign_keys=ON&_busy_timeout=5000&_pragma=synchronous(normal)&_txlock=immediate"
	raw, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlitedb: open: %w", err)
	}
	// Optimize pool settings: allow up to 100 conns, keep up to 10 idle conns warm.
	raw.SetMaxOpenConns(100)
	raw.SetMaxIdleConns(10)
	raw.SetConnMaxLifetime(0)

	db := &DB{DB: raw}
	if err := db.migrate(); err != nil {
		raw.Close()
		return nil, fmt.Errorf("sqlitedb: migrate: %w", err)
	}
	return db, nil
}

// migrate applies all pending schema migrations using PRAGMA user_version
// as the bookkeeping mechanism. Each migration is applied in its own
// transaction so that a failure leaves the database in a consistent state.
func (d *DB) migrate() error {
	var version int
	if err := d.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}

	// NOTE: Append new migrations to this slice; never reorder or edit
	// existing ones. Index i corresponds to bumping user_version from i to i+1.
	migrations := []string{
		// v0 -> v1: initial schema shared by vectorstore and todo stores.
		`
		CREATE TABLE IF NOT EXISTS memories (
			id TEXT PRIMARY KEY,
			content TEXT NOT NULL,
			embedding BLOB NOT NULL,
			timestamp TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT ''
		);

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

		CREATE INDEX IF NOT EXISTS idx_todo_plan         ON todo_items(plan_id);
		CREATE INDEX IF NOT EXISTS idx_plans_status      ON plans(status);
		CREATE INDEX IF NOT EXISTS idx_todo_deps_todo    ON todo_dependencies(todo_id);
		CREATE INDEX IF NOT EXISTS idx_todo_deps_depends ON todo_dependencies(depends_on);
		`,

		// v1 → v2: teams and agents tables.
		`
		CREATE TABLE IF NOT EXISTS teams (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			description TEXT NOT NULL DEFAULT '',
			workspaces TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS agents (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			description TEXT NOT NULL DEFAULT '',
			team_name TEXT NOT NULL DEFAULT '',
			is_leader INTEGER NOT NULL DEFAULT 0,
			model TEXT NOT NULL DEFAULT '',
			system_prompt TEXT NOT NULL DEFAULT '',
			permission INTEGER NOT NULL DEFAULT 0,
			mcp_servers TEXT NOT NULL DEFAULT '[]',
			skill_ids TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now')),
			FOREIGN KEY (team_name) REFERENCES teams(name) ON DELETE SET DEFAULT
		);

		CREATE INDEX IF NOT EXISTS idx_agents_team ON agents(team_name);
		CREATE INDEX IF NOT EXISTS idx_agents_leader ON agents(is_leader);
		`,

		// v2 → v3: scheduled tasks table.
		`
		CREATE TABLE IF NOT EXISTS scheduled_tasks (
			id TEXT PRIMARY KEY,
			expression TEXT NOT NULL,
			instruction TEXT NOT NULL,
			target_agent TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active', 'paused', 'completed')),
			last_run_at TEXT,
			next_run_at TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);

		CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_next_run ON scheduled_tasks(next_run_at) WHERE status = 'active';
		`,

		// v3 -> v4: add qq bot metadata to scheduled tasks.
		`
		ALTER TABLE scheduled_tasks ADD COLUMN qq_source INTEGER DEFAULT -1;
		ALTER TABLE scheduled_tasks ADD COLUMN qq_openid TEXT;
		ALTER TABLE scheduled_tasks ADD COLUMN qq_target_openid TEXT;
		ALTER TABLE scheduled_tasks ADD COLUMN qq_chat_id TEXT;
		`,

		// v4 -> v5: LLM providers, LLM models, and default models tables.
		`
		CREATE TABLE IF NOT EXISTS llm_providers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			base_url TEXT NOT NULL,
			api_key TEXT NOT NULL DEFAULT '',
			api_key_env TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			is_default INTEGER NOT NULL DEFAULT 0,
			timeout_ms INTEGER NOT NULL DEFAULT 0,
			max_retries INTEGER NOT NULL DEFAULT 0,
			initial_delay_ms INTEGER NOT NULL DEFAULT 0,
			max_delay_ms INTEGER NOT NULL DEFAULT 0,
			backoff_multiplier REAL NOT NULL DEFAULT 0.0,
			headers TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS llm_models (
			id TEXT PRIMARY KEY,
			provider_id TEXT NOT NULL REFERENCES llm_providers(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			api_model TEXT NOT NULL DEFAULT '',
			context_window INTEGER NOT NULL DEFAULT 0,
			enabled INTEGER NOT NULL DEFAULT 1,
			temperature REAL NOT NULL DEFAULT 0.0,
			max_tokens INTEGER NOT NULL DEFAULT 0,
			thinking_enabled INTEGER NOT NULL DEFAULT 0,
			reasoning_effort TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS llm_default_models (
			role TEXT PRIMARY KEY,
			model_ref TEXT NOT NULL,
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);

		CREATE INDEX IF NOT EXISTS idx_llm_models_provider ON llm_models(provider_id);
		`,

		// v5 -> v6: migrate plans/todo tables to issue/todo_items with status check, plan column, and comments
		`
		ALTER TABLE plans RENAME TO old_plans;
		CREATE TABLE IF NOT EXISTS issue (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			plan TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'backlog' CHECK(status IN ('backlog','todo','running','done')),
			tags TEXT NOT NULL DEFAULT '',
			creator TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		INSERT INTO issue (id, title, plan, status, tags, creator, created_at, updated_at)
		SELECT id, title, content, CASE WHEN status = 'plan' THEN 'todo' ELSE status END, tags, creator, created_at, updated_at FROM old_plans;
		DROP TABLE old_plans;

		ALTER TABLE todo_items RENAME TO old_todo_items;
		CREATE TABLE IF NOT EXISTS todo_items (
			id TEXT PRIMARY KEY,
			issue_id TEXT NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
			content TEXT NOT NULL,
			completed INTEGER NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		);
		INSERT INTO todo_items (id, issue_id, content, completed, sort_order, created_at)
		SELECT id, plan_id, content, completed, sort_order, created_at FROM old_todo_items;
		DROP TABLE old_todo_items;

		CREATE INDEX IF NOT EXISTS idx_todo_issue ON todo_items(issue_id);
		CREATE INDEX IF NOT EXISTS idx_issue_status ON issue(status);

		CREATE TABLE IF NOT EXISTS issue_comments (
			id TEXT PRIMARY KEY,
			issue_id TEXT NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
			author TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_issue_comments_issue_id ON issue_comments(issue_id);
		`,

		// v6 -> v7: add description column to issue table and restore content/plan separation
		`
		ALTER TABLE issue ADD COLUMN description TEXT NOT NULL DEFAULT '';
		UPDATE issue SET description = plan;
		UPDATE issue SET plan = '';
		`,

		// v7 -> v8: add author column to issue table, copy creator values, and drop creator column
		`
		ALTER TABLE issue ADD COLUMN author TEXT NOT NULL DEFAULT '';
		UPDATE issue SET author = creator;
		ALTER TABLE issue DROP COLUMN creator;
		`,

		// v8 -> v9: generic system settings table for JSON-serialized configs.
		`
		CREATE TABLE IF NOT EXISTS system_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		`,

		// v9 -> v10: projects table.
		`
		CREATE TABLE IF NOT EXISTS projects (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			path TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		`,

		// v10 -> v11: projects table (re-run/ensure in case user_version got out of sync)
		`
		CREATE TABLE IF NOT EXISTS projects (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			path TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		`,

		// v11 -> v12: memory engine tables (BM25 + Knowledge Graph).
		// mem_entries replaces the old v1 "memories" table (avoids collision).
		// FTS5 triggers auto-sync content/date from mem_entries to mem_fts.
		// kg_edges has temporal validity windows (valid_from, valid_until).
		`
		CREATE TABLE IF NOT EXISTS mem_entries (
			id TEXT PRIMARY KEY,
			content TEXT NOT NULL,
			content_hash TEXT NOT NULL UNIQUE,
			date TEXT NOT NULL,
			tags TEXT NOT NULL DEFAULT '',
			event_time TEXT NOT NULL,
			salience REAL NOT NULL DEFAULT 1.0,
			last_recalled_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_mem_entries_date ON mem_entries(date);
		CREATE INDEX IF NOT EXISTS idx_mem_entries_event_time ON mem_entries(event_time);

		CREATE VIRTUAL TABLE IF NOT EXISTS mem_fts USING fts5(
			content, date,
			content='mem_entries', content_rowid='rowid',
			tokenize='unicode61'
		);

		CREATE TRIGGER IF NOT EXISTS mem_fts_ai AFTER INSERT ON mem_entries BEGIN
			INSERT INTO mem_fts(rowid, content, date) VALUES (new.rowid, new.content, new.date);
		END;
		CREATE TRIGGER IF NOT EXISTS mem_fts_ad AFTER DELETE ON mem_entries BEGIN
			INSERT INTO mem_fts(mem_fts, rowid, content, date) VALUES('delete', old.rowid, old.content, old.date);
		END;
		CREATE TRIGGER IF NOT EXISTS mem_fts_au AFTER UPDATE ON mem_entries BEGIN
			INSERT INTO mem_fts(mem_fts, rowid, content, date) VALUES('delete', old.rowid, old.content, old.date);
			INSERT INTO mem_fts(rowid, content, date) VALUES (new.rowid, new.content, new.date);
		END;

		CREATE TABLE IF NOT EXISTS kg_nodes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			type TEXT NOT NULL DEFAULT 'entity',
			mention_count INTEGER NOT NULL DEFAULT 1,
			first_seen TEXT NOT NULL,
			last_seen TEXT NOT NULL,
			confidence REAL NOT NULL DEFAULT 1.0
		);
		CREATE INDEX IF NOT EXISTS idx_kg_nodes_type ON kg_nodes(type);
		CREATE INDEX IF NOT EXISTS idx_kg_nodes_mention_count ON kg_nodes(mention_count DESC);

		CREATE TABLE IF NOT EXISTS kg_edges (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source INTEGER NOT NULL REFERENCES kg_nodes(id),
			target INTEGER NOT NULL REFERENCES kg_nodes(id),
			rel_type TEXT NOT NULL,
			weight REAL NOT NULL DEFAULT 1.0,
			evidence TEXT NOT NULL DEFAULT '',
			source_hash TEXT NOT NULL DEFAULT '',
			event_time TEXT NOT NULL,
			valid_from TEXT NOT NULL DEFAULT '',
			valid_until TEXT,
			last_reinforced TEXT NOT NULL DEFAULT '',
			UNIQUE(source, target, rel_type)
		);
		CREATE INDEX IF NOT EXISTS idx_kg_edges_source ON kg_edges(source);
		CREATE INDEX IF NOT EXISTS idx_kg_edges_target ON kg_edges(target);
		CREATE INDEX IF NOT EXISTS idx_kg_edges_valid_until ON kg_edges(valid_until);
		CREATE INDEX IF NOT EXISTS idx_kg_edges_source_hash ON kg_edges(source_hash);

		CREATE TABLE IF NOT EXISTS kg_aliases (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			alias TEXT NOT NULL,
			canonical TEXT NOT NULL REFERENCES kg_nodes(name),
			UNIQUE(alias, canonical)
		);
		`,

		// v12 -> v13: migrate old memories table to mem_entries, then drop dead tables.
		// Old memories schema: id, content, embedding (BLOB), timestamp, source.
		// New mem_entries schema: id, content, content_hash, date, tags, event_time, salience, created_at.
		// content_hash uses old id as a stable identifier (SHA-256 is used for new entries).
		// Embeddings are discarded — if an embedding provider is configured, re-embedding happens lazily.
		`
		INSERT OR IGNORE INTO mem_entries (id, content, content_hash, date, tags, event_time, salience, created_at)
		SELECT
			id,
			content,
			'legacy:' || id,
			COALESCE(substr(timestamp, 1, 10), date('now')),
			COALESCE(NULLIF(source, ''), 'legacy'),
			COALESCE(NULLIF(timestamp, ''), datetime('now')),
			0.5,
			COALESCE(NULLIF(timestamp, ''), datetime('now'))
		FROM memories
		WHERE content IS NOT NULL AND content != '';

		DROP TABLE IF EXISTS memories;
		DROP TABLE IF EXISTS issue_comments;
		DROP TABLE IF EXISTS todo_dependencies;
		DROP TABLE IF EXISTS todo_items;
		DROP TABLE IF EXISTS issue;
		`,

		// v13 → v14: add 'running' and 'failed' to scheduled_tasks CHECK constraint.
		// Uses table swap because SQLite does not support ALTER TABLE for CHECK constraints.
		`CREATE TABLE IF NOT EXISTS scheduled_tasks_new (
			id TEXT PRIMARY KEY,
			expression TEXT NOT NULL,
			instruction TEXT NOT NULL,
			target_agent TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','paused','running','completed','failed')),
			last_run_at TEXT,
			next_run_at TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now')),
			qq_source INTEGER DEFAULT -1,
			qq_openid TEXT,
			qq_target_openid TEXT,
			qq_chat_id TEXT
		);

		INSERT OR IGNORE INTO scheduled_tasks_new SELECT * FROM scheduled_tasks;

		DROP TABLE IF EXISTS scheduled_tasks;
		ALTER TABLE scheduled_tasks_new RENAME TO scheduled_tasks;

		CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_next_run ON scheduled_tasks(next_run_at) WHERE status = 'active';
		`,
	}

	for v := version; v < currentSchemaVersion && v < len(migrations); v++ {
		tx, err := d.Begin()
		if err != nil {
			return fmt.Errorf("begin v%d: %w", v+1, err)
		}
		if _, err := tx.Exec(migrations[v]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply v%d: %w", v+1, err)
		}
		// PRAGMA does not support parameter binding; v+1 is a bounded int,
		// so Sprintf is safe here.
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, v+1)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("bump user_version to %d: %w", v+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit v%d: %w", v+1, err)
		}
	}
	return nil
}
