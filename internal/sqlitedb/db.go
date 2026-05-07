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
const currentSchemaVersion = 1

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
	// rather than returning SQLITE_BUSY immediately. foreign_keys=ON is
	// required for ON DELETE CASCADE on todo_dependencies / todo_items.
	dsn := path + "?_journal_mode=WAL&_foreign_keys=ON&_busy_timeout=5000"
	raw, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlitedb: open: %w", err)
	}
	// Keep the pool small; SQLite writes are serialized anyway.
	raw.SetMaxOpenConns(4)

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
