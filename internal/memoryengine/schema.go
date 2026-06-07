package memoryengine

// MigrationDDL returns the DDL statements for memory engine tables.
func MigrationDDL() string { return migrationSQL }

const migrationSQL = `
-- Memory entries (avoids collision with v1 "memories" table)
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

-- FTS5 virtual table over mem_entries
CREATE VIRTUAL TABLE IF NOT EXISTS mem_fts USING fts5(
    content,
    date,
    content='mem_entries',
    content_rowid='rowid',
    tokenize='unicode61'
);

-- Triggers to keep FTS5 in sync with mem_entries
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

-- Knowledge graph nodes
CREATE TABLE IF NOT EXISTS kg_nodes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    type TEXT NOT NULL DEFAULT 'entity',
    mention_count INTEGER NOT NULL DEFAULT 1,
    first_seen TEXT NOT NULL,
    last_seen TEXT NOT NULL,
    confidence REAL NOT NULL DEFAULT 1.0
);

-- Knowledge graph edges with temporal validity windows
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

-- Entity aliases
CREATE TABLE IF NOT EXISTS kg_aliases (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    alias TEXT NOT NULL,
    canonical TEXT NOT NULL REFERENCES kg_nodes(name),
    UNIQUE(alias, canonical)
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_kg_nodes_type ON kg_nodes(type);
CREATE INDEX IF NOT EXISTS idx_kg_nodes_last_seen ON kg_nodes(last_seen);
CREATE INDEX IF NOT EXISTS idx_kg_nodes_mention_count ON kg_nodes(mention_count DESC);
CREATE INDEX IF NOT EXISTS idx_kg_edges_source ON kg_edges(source);
CREATE INDEX IF NOT EXISTS idx_kg_edges_target ON kg_edges(target);
CREATE INDEX IF NOT EXISTS idx_kg_edges_rel_type ON kg_edges(rel_type);
CREATE INDEX IF NOT EXISTS idx_kg_edges_valid_until ON kg_edges(valid_until);
CREATE INDEX IF NOT EXISTS idx_kg_edges_source_hash ON kg_edges(source_hash);
CREATE INDEX IF NOT EXISTS idx_mem_entries_date ON mem_entries(date);
CREATE INDEX IF NOT EXISTS idx_mem_entries_event_time ON mem_entries(event_time);
`
