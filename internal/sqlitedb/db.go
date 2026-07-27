// Package sqlitedb provides a shared SQLite database connection with
// centralized schema migrations. All subsystems (memory, config, cron,
// team store, telemetry) share the same physical file (soloqueue.db),
// so opening it from multiple places causes DDL races and fragmented
// migrations. This package exposes a single *sql.DB and a shared write
// mutex that callers use to serialize writes across stores.
package sqlitedb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

// schemaVersion is written to PRAGMA user_version as a marker that the
// snapshot migration has completed.
const schemaVersion = 9

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

// snapshot contains the full idempotent DDL for all live tables.
const snapshot = `
-- scheduled_tasks
CREATE TABLE IF NOT EXISTS scheduled_tasks (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL CHECK(length(trim(title)) > 0),
	task_type TEXT NOT NULL CHECK(task_type IN ('general','engineering','research')),
	expression TEXT NOT NULL,
	instruction TEXT NOT NULL,
	target_agent TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','paused','running','completed','failed')),
	last_run_at TEXT,
	next_run_at TEXT NOT NULL,
	created_at TEXT NOT NULL DEFAULT (datetime('now')),
	updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_next_run ON scheduled_tasks(next_run_at) WHERE status = 'active';

-- llm_providers
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

-- llm_models
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
	vision INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL DEFAULT (datetime('now')),
	updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_llm_models_provider ON llm_models(provider_id);

-- system_settings
CREATE TABLE IF NOT EXISTS system_settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL,
	updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- projects
CREATE TABLE IF NOT EXISTS projects (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	path TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT (datetime('now')),
	updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- mem_entries (BM25 memory store)
CREATE TABLE IF NOT EXISTS mem_entries (
	id TEXT PRIMARY KEY,
	content TEXT NOT NULL,
	content_hash TEXT NOT NULL UNIQUE,
	date TEXT NOT NULL,
	tags TEXT NOT NULL DEFAULT '',
	event_time TEXT NOT NULL,
	salience REAL NOT NULL DEFAULT 1.0,
	last_recalled_at TEXT NOT NULL DEFAULT '',
	memory_type TEXT NOT NULL DEFAULT 'legacy',
	scope_type TEXT NOT NULL DEFAULT 'global',
	scope_id TEXT NOT NULL DEFAULT '',
	source_type TEXT NOT NULL DEFAULT 'legacy',
	source_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'active',
	confidence REAL NOT NULL DEFAULT 1.0,
	expires_at TEXT NOT NULL DEFAULT '',
	supersedes_hash TEXT NOT NULL DEFAULT '',
	canonical_hash TEXT NOT NULL DEFAULT '',
	recall_count INTEGER NOT NULL DEFAULT 0,
	used_count INTEGER NOT NULL DEFAULT 0,
	last_used_at TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL DEFAULT (datetime('now')),
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_mem_entries_date ON mem_entries(date);
CREATE INDEX IF NOT EXISTS idx_mem_entries_event_time ON mem_entries(event_time);

-- mem_fts (FTS5 virtual table over mem_entries)
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

-- kg_nodes (knowledge graph nodes)
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

-- kg_edges (knowledge graph edges with temporal validity windows)
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

-- kg_aliases (entity name aliases)
CREATE TABLE IF NOT EXISTS kg_aliases (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	alias TEXT NOT NULL,
	canonical TEXT NOT NULL REFERENCES kg_nodes(name),
	UNIQUE(alias, canonical)
);

-- usage_metrics (token usage and router stats)
CREATE TABLE IF NOT EXISTS usage_metrics (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
	metric_category TEXT NOT NULL,
	usage_type TEXT NOT NULL DEFAULT '',
	team_id TEXT NOT NULL DEFAULT '',
	model_name TEXT NOT NULL DEFAULT '',
	prompt_tokens INTEGER NOT NULL DEFAULT 0,
	completion_tokens INTEGER NOT NULL DEFAULT 0,
	total_tokens INTEGER NOT NULL DEFAULT 0,
	cache_hit_tokens INTEGER NOT NULL DEFAULT 0,
	cache_miss_tokens INTEGER NOT NULL DEFAULT 0,
	classification_level TEXT NOT NULL DEFAULT '',
	classification_source TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_usage_metrics_timestamp ON usage_metrics(timestamp);
CREATE INDEX IF NOT EXISTS idx_usage_metrics_team ON usage_metrics(team_id, usage_type);

-- classifier_decisions (fast-track vs LLM comparison for optimization)
CREATE TABLE IF NOT EXISTS classifier_decisions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
	prompt_trunc TEXT NOT NULL DEFAULT '',
	ft_level TEXT NOT NULL DEFAULT '',
	ft_confidence INTEGER NOT NULL DEFAULT 0,
	llm_invoked INTEGER NOT NULL DEFAULT 0,
	llm_level TEXT NOT NULL DEFAULT '',
	llm_confidence INTEGER NOT NULL DEFAULT 0,
	llm_error TEXT NOT NULL DEFAULT '',
	final_level TEXT NOT NULL DEFAULT '',
	final_source TEXT NOT NULL DEFAULT '',
	hybrid_applied INTEGER NOT NULL DEFAULT 0,
	prior_level TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_classifier_decisions_ts ON classifier_decisions(timestamp);

-- cron_execution_history
CREATE TABLE IF NOT EXISTS cron_execution_history (
	id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	executed_at TEXT NOT NULL,
	completed_at TEXT NOT NULL,
	duration_ms INTEGER NOT NULL DEFAULT 0,
	status TEXT NOT NULL DEFAULT 'success' CHECK(status IN ('success','failed','panic')),
	result_summary TEXT NOT NULL DEFAULT '',
	error_message TEXT NOT NULL DEFAULT '',
	task_type TEXT NOT NULL DEFAULT '',
	target_agent TEXT NOT NULL DEFAULT '',
	model_id TEXT NOT NULL DEFAULT '',
	provider_id TEXT NOT NULL DEFAULT '',
	timeline_dir TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_cron_history_task_id ON cron_execution_history(task_id);
CREATE INDEX IF NOT EXISTS idx_cron_history_executed_at ON cron_execution_history(executed_at);

-- workflow_runs stores durable immutable snapshots. Keeping node-run detail in
-- JSON avoids a write-heavy normalized table while the engine publishes state
-- transitions frequently.
CREATE TABLE IF NOT EXISTS workflow_runs (
	id TEXT PRIMARY KEY,
	workflow_name TEXT NOT NULL,
	status TEXT NOT NULL,
	started_at TEXT NOT NULL,
	finished_at TEXT NOT NULL DEFAULT '',
	snapshot_json TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_workflow_runs_workflow_started ON workflow_runs(workflow_name, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_workflow_runs_status ON workflow_runs(status);
`

// migrate applies the schema snapshot on every startup.
// All DDL uses IF NOT EXISTS / IF EXISTS, so it is idempotent and safe
// regardless of the current database state.
func (d *DB) migrate() error {
	tx, err := d.Begin()
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	if _, err := tx.Exec(snapshot); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("apply snapshot: %w", err)
	}
	// Default model roles are now stored in settings as model_routes. The old
	// table was only a v1 cache and must not survive as an alternate source.
	if _, err := tx.Exec(`DROP TABLE IF EXISTS llm_default_models`); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("remove legacy default models table: %w", err)
	}

	// v3: scheduled tasks require a user-facing title and an explicit task
	// level. CREATE TABLE IF NOT EXISTS does not alter existing databases, so
	// rebuild the table transactionally when either column is absent.
	hasTitle, err := tableHasColumn(tx, "scheduled_tasks", "title")
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("inspect scheduled_tasks title column: %w", err)
	}
	hasTaskLevel, err := tableHasColumn(tx, "scheduled_tasks", "task_level")
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("inspect scheduled_tasks task_level column: %w", err)
	}
	if !hasTitle {
		if _, err := tx.Exec(`
			DROP INDEX IF EXISTS idx_scheduled_tasks_next_run;
			ALTER TABLE scheduled_tasks RENAME TO scheduled_tasks_v2;
			CREATE TABLE scheduled_tasks (
				id TEXT PRIMARY KEY,
				title TEXT NOT NULL CHECK(length(trim(title)) > 0),
				task_level TEXT NOT NULL CHECK(task_level IN ('L0','L1','L2','L3','L4')),
				expression TEXT NOT NULL,
				instruction TEXT NOT NULL,
				target_agent TEXT NOT NULL,
				status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','paused','running','completed','failed')),
				last_run_at TEXT,
				next_run_at TEXT NOT NULL,
				created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		INSERT INTO scheduled_tasks (
			id, title, task_level, expression, instruction, target_agent,
			status, last_run_at, next_run_at, created_at, updated_at
		)
		SELECT
			id,
			CASE
				WHEN length(trim(instruction)) > 0 THEN substr(trim(
					CASE WHEN instr(trim(instruction), char(10)) > 0
					THEN substr(trim(instruction), 1, instr(trim(instruction), char(10)) - 1)
					ELSE trim(instruction) END
				), 1, 100)
				ELSE 'Scheduled task ' || substr(id, 1, 8)
			END,
			CASE WHEN trim(target_agent) = '' OR upper(trim(target_agent)) = 'L1' THEN 'L1' ELSE 'L2' END,
			expression, instruction, target_agent, status, last_run_at,
			next_run_at, created_at, updated_at
		FROM scheduled_tasks_v2;
			DROP TABLE scheduled_tasks_v2;
			CREATE INDEX idx_scheduled_tasks_next_run ON scheduled_tasks(next_run_at) WHERE status = 'active';
		`); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migrate scheduled_tasks v3: %w", err)
		}
	}

	// v4: add L4 to the scheduled_tasks task_level CHECK constraint. SQLite
	// cannot alter CHECK constraints in place, so rebuild existing v3 tables.
	var scheduledTasksSQL string
	if err := tx.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'scheduled_tasks'`).Scan(&scheduledTasksSQL); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("inspect scheduled_tasks schema: %w", err)
	}
	if hasTaskLevel && !strings.Contains(scheduledTasksSQL, "'L4'") {
		if _, err := tx.Exec(`
			DROP INDEX IF EXISTS idx_scheduled_tasks_next_run;
			ALTER TABLE scheduled_tasks RENAME TO scheduled_tasks_v3;
			CREATE TABLE scheduled_tasks (
				id TEXT PRIMARY KEY,
				title TEXT NOT NULL CHECK(length(trim(title)) > 0),
				task_level TEXT NOT NULL CHECK(task_level IN ('L0','L1','L2','L3','L4')),
				expression TEXT NOT NULL,
				instruction TEXT NOT NULL,
				target_agent TEXT NOT NULL,
				status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','paused','running','completed','failed')),
				last_run_at TEXT,
				next_run_at TEXT NOT NULL,
				created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		INSERT INTO scheduled_tasks (
			id, title, task_level, expression, instruction, target_agent,
			status, last_run_at, next_run_at, created_at, updated_at
		)
		SELECT
			id, title, task_level, expression, instruction, target_agent,
			status, last_run_at, next_run_at, created_at, updated_at
		FROM scheduled_tasks_v3;
			DROP TABLE scheduled_tasks_v3;
			CREATE INDEX idx_scheduled_tasks_next_run ON scheduled_tasks(next_run_at) WHERE status = 'active';
		`); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migrate scheduled_tasks v4: %w", err)
		}
	}

	// v10: replace obsolete L0-L4 cron levels with the three task types used by
	// routing. Existing task data is retained: conversational levels become
	// general; implementation-oriented levels become engineering.
	hasTaskType, err := tableHasColumn(tx, "scheduled_tasks", "task_type")
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("inspect scheduled_tasks task_type column: %w", err)
	}
	if !hasTaskType {
		if _, err := tx.Exec(`
			DROP INDEX IF EXISTS idx_scheduled_tasks_next_run;
			ALTER TABLE scheduled_tasks RENAME TO scheduled_tasks_legacy_router;
			CREATE TABLE scheduled_tasks (
				id TEXT PRIMARY KEY,
				title TEXT NOT NULL CHECK(length(trim(title)) > 0),
				task_type TEXT NOT NULL CHECK(task_type IN ('general','engineering','research')),
				expression TEXT NOT NULL,
				instruction TEXT NOT NULL,
				target_agent TEXT NOT NULL,
				status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','paused','running','completed','failed')),
				last_run_at TEXT,
				next_run_at TEXT NOT NULL,
				created_at TEXT NOT NULL DEFAULT (datetime('now')),
				updated_at TEXT NOT NULL DEFAULT (datetime('now'))
			);
			INSERT INTO scheduled_tasks (id, title, task_type, expression, instruction, target_agent, status, last_run_at, next_run_at, created_at, updated_at)
			SELECT id, title,
				CASE WHEN task_level IN ('L0', 'L1') THEN 'general' ELSE 'engineering' END,
				expression, instruction, target_agent, status, last_run_at, next_run_at, created_at, updated_at
			FROM scheduled_tasks_legacy_router;
			DROP TABLE scheduled_tasks_legacy_router;
			CREATE INDEX idx_scheduled_tasks_next_run ON scheduled_tasks(next_run_at) WHERE status = 'active';
		`); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migrate scheduled_tasks to task types: %w", err)
		}
	}

	hasHistoryTaskType, err := tableHasColumn(tx, "cron_execution_history", "task_type")
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("inspect cron history task_type column: %w", err)
	}
	if !hasHistoryTaskType {
		if _, err := tx.Exec(`
			ALTER TABLE cron_execution_history RENAME TO cron_execution_history_legacy_router;
			CREATE TABLE cron_execution_history (
				id TEXT PRIMARY KEY, task_id TEXT NOT NULL, executed_at TEXT NOT NULL, completed_at TEXT NOT NULL,
				duration_ms INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'success' CHECK(status IN ('success','failed','panic')),
				result_summary TEXT NOT NULL DEFAULT '', error_message TEXT NOT NULL DEFAULT '', task_type TEXT NOT NULL DEFAULT '',
				target_agent TEXT NOT NULL DEFAULT '', model_id TEXT NOT NULL DEFAULT '', provider_id TEXT NOT NULL DEFAULT '', timeline_dir TEXT NOT NULL DEFAULT ''
			);
			INSERT INTO cron_execution_history (id, task_id, executed_at, completed_at, duration_ms, status, result_summary, error_message, task_type, target_agent, model_id, provider_id, timeline_dir)
			SELECT id, task_id, executed_at, completed_at, duration_ms, status, result_summary, error_message,
				CASE WHEN task_level IN ('L0', 'L1') THEN 'general' ELSE 'engineering' END,
				target_agent, model_id, provider_id, timeline_dir
			FROM cron_execution_history_legacy_router;
			DROP TABLE cron_execution_history_legacy_router;
			CREATE INDEX IF NOT EXISTS idx_cron_history_task_id ON cron_execution_history(task_id);
			CREATE INDEX IF NOT EXISTS idx_cron_history_executed_at ON cron_execution_history(executed_at);
		`); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migrate cron history to task types: %w", err)
		}
	}

	// v6: change llm_models PRIMARY KEY from id to (provider_id, id) composite
	// so model IDs only need to be unique within each provider.
	{
		var pkSQL string
		if err := tx.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'llm_models'`).Scan(&pkSQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("inspect llm_models schema: %w", err)
		}
		if !strings.Contains(pkSQL, "PRIMARY KEY (provider_id, id)") {
			if _, err := tx.Exec(`
				CREATE TABLE llm_models_v6 (
					provider_id TEXT NOT NULL REFERENCES llm_providers(id) ON DELETE CASCADE,
					id TEXT NOT NULL,
					name TEXT NOT NULL,
					api_model TEXT NOT NULL DEFAULT '',
					context_window INTEGER NOT NULL DEFAULT 0,
					enabled INTEGER NOT NULL DEFAULT 1,
					temperature REAL NOT NULL DEFAULT 0.0,
					max_tokens INTEGER NOT NULL DEFAULT 0,
					thinking_enabled INTEGER NOT NULL DEFAULT 0,
					reasoning_effort TEXT NOT NULL DEFAULT '',
					vision INTEGER NOT NULL DEFAULT 0,
					created_at TEXT NOT NULL DEFAULT (datetime('now')),
					updated_at TEXT NOT NULL DEFAULT (datetime('now')),
					PRIMARY KEY (provider_id, id)
				);
				INSERT INTO llm_models_v6 SELECT * FROM llm_models;
				DROP TABLE llm_models;
				ALTER TABLE llm_models_v6 RENAME TO llm_models;
				CREATE INDEX IF NOT EXISTS idx_llm_models_provider ON llm_models(provider_id);
			`); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("migrate llm_models v6: %w", err)
			}
		}
	}

	// Drop legacy tables no longer in use.
	// NOTE: memories is NOT dropped — it is used by the vector store.
	tx.Exec(`
		DROP TABLE IF EXISTS issue_comments;
		DROP TABLE IF EXISTS todo_dependencies;
		DROP TABLE IF EXISTS todo_items;
		DROP TABLE IF EXISTS issue;
		DROP TABLE IF EXISTS old_plans;
		DROP TABLE IF EXISTS plans;
		DROP TABLE IF EXISTS teams;
		DROP TABLE IF EXISTS agents;
	`)

	// Migrate legacy memories data into mem_entries if the old table still has
	// entries that were created before the vector store took over the table.
	// Error is ignored — the table may not exist or contain no legacy rows.
	tx.Exec(`
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
		WHERE content IS NOT NULL AND content != ''
	`)

	// v8: add typed, scoped lifecycle metadata to long-term memories. Columns
	// are added individually so existing local databases keep their rowids and
	// FTS external-content links intact.
	memoryColumns := []struct {
		name string
		ddl  string
	}{
		{"memory_type", `ALTER TABLE mem_entries ADD COLUMN memory_type TEXT NOT NULL DEFAULT 'legacy'`},
		{"scope_type", `ALTER TABLE mem_entries ADD COLUMN scope_type TEXT NOT NULL DEFAULT 'global'`},
		{"scope_id", `ALTER TABLE mem_entries ADD COLUMN scope_id TEXT NOT NULL DEFAULT ''`},
		{"source_type", `ALTER TABLE mem_entries ADD COLUMN source_type TEXT NOT NULL DEFAULT 'legacy'`},
		{"source_id", `ALTER TABLE mem_entries ADD COLUMN source_id TEXT NOT NULL DEFAULT ''`},
		{"status", `ALTER TABLE mem_entries ADD COLUMN status TEXT NOT NULL DEFAULT 'active'`},
		{"confidence", `ALTER TABLE mem_entries ADD COLUMN confidence REAL NOT NULL DEFAULT 1.0`},
		{"expires_at", `ALTER TABLE mem_entries ADD COLUMN expires_at TEXT NOT NULL DEFAULT ''`},
		{"supersedes_hash", `ALTER TABLE mem_entries ADD COLUMN supersedes_hash TEXT NOT NULL DEFAULT ''`},
		{"canonical_hash", `ALTER TABLE mem_entries ADD COLUMN canonical_hash TEXT NOT NULL DEFAULT ''`},
		{"recall_count", `ALTER TABLE mem_entries ADD COLUMN recall_count INTEGER NOT NULL DEFAULT 0`},
		{"used_count", `ALTER TABLE mem_entries ADD COLUMN used_count INTEGER NOT NULL DEFAULT 0`},
		{"last_used_at", `ALTER TABLE mem_entries ADD COLUMN last_used_at TEXT NOT NULL DEFAULT ''`},
		{"updated_at", `ALTER TABLE mem_entries ADD COLUMN updated_at TEXT NOT NULL DEFAULT ''`},
	}
	memorySchemaChanged := false
	for _, column := range memoryColumns {
		hasColumn, err := tableHasColumn(tx, "mem_entries", column.name)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("inspect mem_entries %s column: %w", column.name, err)
		}
		if !hasColumn {
			if _, err := tx.Exec(column.ddl); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("migrate mem_entries v8 add %s: %w", column.name, err)
			}
			memorySchemaChanged = true
		}
	}
	if memorySchemaChanged {
		if _, err := tx.Exec(`
			INSERT INTO mem_fts(mem_fts) VALUES('rebuild');
			UPDATE mem_entries SET updated_at = created_at WHERE updated_at = '';
		`); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migrate mem_entries v8 data: %w", err)
		}
	}
	if _, err := tx.Exec(`
		CREATE INDEX IF NOT EXISTS idx_mem_entries_scope_status
			ON mem_entries(scope_type, scope_id, status);
		CREATE INDEX IF NOT EXISTS idx_mem_entries_canonical
			ON mem_entries(canonical_hash);
	`); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("migrate mem_entries v8 indexes: %w", err)
	}

	// Fix corrupted llm_models rows where the "vision" column accidentally holds
	// a timestamp string (caused by a column-offset bug when the vision column was
	// first introduced). In affected rows the data is shifted: vision has the
	// original created_at, created_at has the original updated_at, and updated_at
	// has the original vision 0/1 value. We swap them back into place.
	tx.Exec(`
		UPDATE llm_models
		SET created_at = vision,
		    updated_at = created_at,
		    vision = CAST(updated_at AS INTEGER)
		WHERE typeof(vision) != 'integer'
	`)

	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("bump user_version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	return nil
}

func tableHasColumn(tx *sql.Tx, table, column string) (bool, error) {
	rows, err := tx.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
