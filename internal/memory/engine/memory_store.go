package engine

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine/embedding"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine/vectorstore"
)

// MemoryStore provides CRUD and BM25 search over mem_entries.
type MemoryStore struct {
	db       *sql.DB
	mu       *sync.Mutex
	embedder embedding.Embedder
	vecStore vectorstore.VectorStore
	log      *logger.Logger
}

// NewMemoryStore creates a MemoryStore backed by the shared database.
func NewMemoryStore(db *sql.DB, mu *sync.Mutex, embedder embedding.Embedder, vecStore vectorstore.VectorStore, log *logger.Logger) *MemoryStore {
	return &MemoryStore{db: db, mu: mu, embedder: embedder, vecStore: vecStore, log: log}
}

// Save stores a memory and returns its content hash. If the same content
// already exists (by hash), isNew is false. If embedder/vecStore are set,
// the content is also embedded and stored as a vector.
//
// The mutex is released before the embedding HTTP call to avoid self-deadlock
// (vectorstore.Upsert uses the same mutex).
func (m *MemoryStore) Save(ctx context.Context, content, date, tags, eventTime string) (contentHash string, isNew bool, err error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}

	contentHash = hashContent(content)
	now := time.Now().UTC().Format(time.RFC3339)

	// --- Step 1: INSERT into mem_entries under lock ---
	m.mu.Lock()

	var existing string
	err = m.db.QueryRowContext(ctx,
		`SELECT id FROM mem_entries WHERE content_hash = ? AND owner_type = ? AND owner_id = ?`, contentHash, OwnerL1, "",
	).Scan(&existing)
	if err == nil {
		m.mu.Unlock()
		return contentHash, false, nil // already exists
	}
	if err != sql.ErrNoRows {
		m.mu.Unlock()
		return "", false, fmt.Errorf("memory save: %w", err)
	}

	id := contentHash[:16]

	_, err = m.db.ExecContext(ctx,
		`INSERT INTO mem_entries (id, content, content_hash, date, tags, event_time, created_at, owner_type, owner_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, content, contentHash, date, tags, eventTime, now, OwnerL1, "",
	)
	if err != nil {
		m.mu.Unlock()
		return "", false, fmt.Errorf("memory save: %w", err)
	}

	m.mu.Unlock()

	// --- Step 2: Embedding + vector upsert (no lock held — vecStore handles its own locking) ---
	if m.embedder != nil && m.vecStore != nil {
		results, embErr := m.embedder.Embed(ctx, []string{content})
		if embErr != nil {
			m.logWarn("memory save: embed failed", embErr)
		} else if len(results) > 0 {
			if upsertErr := m.vecStore.Upsert(ctx, vectorstore.MemoryEntry{
				ID:        id,
				Content:   content,
				Embedding: results[0].Embedding,
				Timestamp: time.Now().UTC(),
				Source:    "memoryengine",
				OwnerType: OwnerL1,
			}); upsertErr != nil {
				m.logWarn("memory save: vector upsert failed", upsertErr)
			}
		}
	}

	return contentHash, true, nil
}

func (m *MemoryStore) saveCandidate(ctx context.Context, candidate MemoryCandidate, canonicalHash string) (contentHash string, isNew bool, err error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}

	hashInput := candidate.ScopeType + "\x00" + candidate.ScopeID + "\x00" + candidate.Content
	if candidate.OwnerType != OwnerL1 || candidate.OwnerID != "" {
		hashInput = candidate.OwnerType + "\x00" + candidate.OwnerID + "\x00" + hashInput
	}
	contentHash = hashContent(hashInput)
	now := time.Now().UTC().Format(time.RFC3339)
	confidence := candidate.Confidence
	if confidence <= 0 {
		confidence = 1.0
	}

	m.mu.Lock()
	var existingHash string
	err = m.db.QueryRowContext(ctx,
		`SELECT content_hash FROM mem_entries
		 WHERE canonical_hash = ? AND owner_type = ? AND owner_id = ?
		   AND scope_type = ? AND scope_id = ? AND status = 'active'
		 LIMIT 1`,
		canonicalHash, candidate.OwnerType, candidate.OwnerID, candidate.ScopeType, candidate.ScopeID,
	).Scan(&existingHash)
	if err == nil {
		m.mu.Unlock()
		return existingHash, false, nil
	}
	if err != sql.ErrNoRows {
		m.mu.Unlock()
		return "", false, fmt.Errorf("memory save candidate lookup: %w", err)
	}

	id := contentHash[:16]
	_, err = m.db.ExecContext(ctx,
		`INSERT INTO mem_entries (
			id, content, content_hash, date, tags, event_time, created_at,
			memory_type, scope_type, scope_id, source_type, source_id,
			status, confidence, expires_at, canonical_hash, updated_at, owner_type, owner_id
		) VALUES (?, ?, ?, ?, '', ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?, ?, ?, ?, ?)`,
		id, candidate.Content, contentHash, candidate.Date, candidate.EventTime, now,
		candidate.MemoryType, candidate.ScopeType, candidate.ScopeID,
		candidate.SourceType, candidate.SourceID, confidence, candidate.ExpiresAt,
		canonicalHash, now, candidate.OwnerType, candidate.OwnerID,
	)
	m.mu.Unlock()
	if err != nil {
		return "", false, fmt.Errorf("memory save candidate: %w", err)
	}

	m.embed(ctx, id, contentHash, candidate.Content, candidate.OwnerType, candidate.OwnerID, candidate.ScopeType, candidate.ScopeID)
	return contentHash, true, nil
}

func (m *MemoryStore) embed(ctx context.Context, id, sourceHash, content, ownerType, ownerID, scopeType, scopeID string) {
	if m.embedder == nil || m.vecStore == nil {
		return
	}
	results, err := m.embedder.Embed(ctx, []string{content})
	if err != nil {
		m.logWarn("memory save: embed failed", err)
		return
	}
	if len(results) == 0 {
		return
	}
	if err := m.vecStore.Upsert(ctx, vectorstore.MemoryEntry{
		ID:        id,
		Content:   content,
		Embedding: results[0].Embedding,
		Timestamp: time.Now().UTC(),
		Source:    sourceHash,
		OwnerType: ownerType,
		OwnerID:   ownerID,
		ScopeType: scopeType,
		ScopeID:   scopeID,
	}); err != nil {
		m.logWarn("memory save: vector upsert failed", err)
	}
}

const memorySelectColumns = `id, content, content_hash, date, tags, event_time, salience, last_recalled_at, created_at,
	memory_type, scope_type, scope_id, source_type, source_id, status, confidence,
	expires_at, supersedes_hash, canonical_hash, last_used_at, owner_type, owner_id`

func scanMemoryEntry(scanner interface{ Scan(...any) error }, e *MemoryEntry) error {
	return scanner.Scan(
		&e.ID, &e.Content, &e.ContentHash, &e.Date, &e.Tags, &e.EventTime,
		&e.Salience, &e.LastRecalledAt, &e.CreatedAt, &e.MemoryType, &e.ScopeType, &e.ScopeID,
		&e.SourceType, &e.SourceID, &e.Status, &e.Confidence, &e.ExpiresAt,
		&e.SupersedesHash, &e.CanonicalHash, &e.LastUsedAt, &e.OwnerType, &e.OwnerID,
	)
}

// GetByContentHashes fetches multiple memories by their content hashes.
func (m *MemoryStore) GetByContentHashes(ctx context.Context, hashes []string) ([]MemoryEntry, error) {
	return m.GetByContentHashesOwned(ctx, hashes, OwnerL1, "")
}

func (m *MemoryStore) GetByContentHashesOwned(ctx context.Context, hashes []string, ownerType, ownerID string) ([]MemoryEntry, error) {
	if len(hashes) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(hashes))
	args := make([]interface{}, 0, len(hashes)+2)
	for i, h := range hashes {
		placeholders[i] = "?"
		args = append(args, h)
	}
	args = append(args, ownerType, ownerID)

	query := fmt.Sprintf(
		`SELECT `+memorySelectColumns+`
		 FROM mem_entries WHERE content_hash IN (%s) AND owner_type = ? AND owner_id = ?`,
		strings.Join(placeholders, ","),
	)

	rows, err := m.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []MemoryEntry
	for rows.Next() {
		var e MemoryEntry
		if err := scanMemoryEntry(rows, &e); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// BM25Search runs FTS5 full-text search with BM25 ranking.
// Returns results sorted by BM25 score (descending) and the max score for normalization.
// Each token in the query is individually escaped for FTS5 safety.
func (m *MemoryStore) BM25Search(ctx context.Context, query string, limit int) ([]SearchResult, float64, error) {
	return m.BM25SearchOwned(ctx, query, limit, OwnerL1, "", "", "", false)
}

func (m *MemoryStore) BM25SearchOwned(ctx context.Context, query string, limit int, ownerType, ownerID, scopeType, scopeID string, includeGlobal bool) ([]SearchResult, float64, error) {
	if limit <= 0 {
		limit = 20
	}

	tokens := tokenizeForFTS5(query)
	if len(tokens) == 0 {
		return nil, 0, nil
	}

	ftsQuery := strings.Join(tokens, " ")

	scopeSQL := ""
	args := []any{ftsQuery, ownerType, ownerID}
	if scopeType != "" {
		if includeGlobal && scopeType != ScopeGlobal {
			scopeSQL = ` AND ((m.scope_type = ? AND m.scope_id = ?) OR m.scope_type = ?)`
			args = append(args, scopeType, scopeID, ScopeGlobal)
		} else {
			scopeSQL = ` AND m.scope_type = ? AND m.scope_id = ?`
			args = append(args, scopeType, scopeID)
		}
	}
	args = append(args, limit)
	rows, err := m.db.QueryContext(ctx,
		`SELECT m.content_hash, m.content, m.date, m.tags, m.event_time,
		        m.memory_type, m.scope_type, m.scope_id, m.status, m.expires_at,
		        m.owner_type, m.owner_id, rank
		 FROM mem_fts JOIN mem_entries m ON m.rowid = mem_fts.rowid
		 WHERE mem_fts MATCH ? AND m.owner_type = ? AND m.owner_id = ? AND m.status = 'active'
		   AND (m.expires_at = '' OR m.expires_at > datetime('now'))
		 `+scopeSQL+`
		 ORDER BY rank
		 LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("bm25 search: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	var maxRank float64
	for rows.Next() {
		var r SearchResult
		var rank float64
		if err := rows.Scan(
			&r.ContentHash, &r.Content, &r.Date, &r.Tags, &r.EventTime,
			&r.MemoryType, &r.ScopeType, &r.ScopeID, &r.Status, &r.ExpiresAt,
			&r.OwnerType, &r.OwnerID, &rank,
		); err != nil {
			return nil, 0, err
		}
		r.Source = "bm25"
		r.Score = math.Abs(rank) // BM25 rank is negative (more negative = better)
		if r.Score > maxRank {
			maxRank = r.Score
		}
		results = append(results, r)
	}

	// Normalize scores to [0, 1] relative to the best match
	if maxRank > 0 {
		for i := range results {
			results[i].Score /= maxRank
		}
	}

	return results, maxRank, rows.Err()
}

// Timeline returns memories chronologically within a date range.
func (m *MemoryStore) Timeline(ctx context.Context, from, to string, limit int) ([]MemoryEntry, error) {
	return m.TimelineScoped(ctx, from, to, limit, "", "", false)
}

func (m *MemoryStore) TimelineScoped(ctx context.Context, from, to string, limit int, scopeType, scopeID string, includeGlobal bool) ([]MemoryEntry, error) {
	return m.TimelineOwned(ctx, from, to, limit, OwnerL1, "", scopeType, scopeID, includeGlobal)
}

func (m *MemoryStore) TimelineOwned(ctx context.Context, from, to string, limit int, ownerType, ownerID, scopeType, scopeID string, includeGlobal bool) ([]MemoryEntry, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `SELECT ` + memorySelectColumns + `
		 FROM mem_entries WHERE owner_type = ? AND owner_id = ? AND status = 'active'
		   AND (expires_at = '' OR expires_at > datetime('now'))`
	args := []interface{}{ownerType, ownerID}

	if from != "" {
		query += ` AND event_time >= ?`
		args = append(args, from)
	}
	if to != "" {
		query += ` AND event_time <= ?`
		args = append(args, to)
	}
	if scopeType != "" {
		if includeGlobal && scopeType != ScopeGlobal {
			query += ` AND ((scope_type = ? AND scope_id = ?) OR scope_type = ?)`
			args = append(args, scopeType, scopeID, ScopeGlobal)
		} else {
			query += ` AND scope_type = ? AND scope_id = ?`
			args = append(args, scopeType, scopeID)
		}
	}
	query += ` ORDER BY event_time DESC LIMIT ?`
	args = append(args, limit)

	rows, err := m.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []MemoryEntry
	for rows.Next() {
		var e MemoryEntry
		if err := scanMemoryEntry(rows, &e); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// BoostSalience increases the salience of a memory (called on recall).
func (m *MemoryStore) BoostSalience(ctx context.Context, contentHash string) error {
	return m.BoostSalienceOwned(ctx, contentHash, OwnerL1, "")
}

func (m *MemoryStore) BoostSalienceOwned(ctx context.Context, contentHash, ownerType, ownerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := m.db.ExecContext(ctx,
		`UPDATE mem_entries SET salience = MIN(2.0, salience + 0.3), last_recalled_at = ?
		 WHERE content_hash = ? AND owner_type = ? AND owner_id = ?`,
		now, contentHash, ownerType, ownerID,
	)
	return err
}

// Count returns the total number of stored memories.
func (m *MemoryStore) Count(ctx context.Context) (int, error) {
	var n int
	err := m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mem_entries`).Scan(&n)
	return n, err
}

func (m *MemoryStore) ActiveContentHashes(ctx context.Context, hashes []string, scopeType, scopeID string, includeGlobal bool) map[string]bool {
	return m.ActiveContentHashesOwned(ctx, hashes, OwnerL1, "", scopeType, scopeID, includeGlobal)
}

func (m *MemoryStore) ActiveContentHashesOwned(ctx context.Context, hashes []string, ownerType, ownerID, scopeType, scopeID string, includeGlobal bool) map[string]bool {
	active := make(map[string]bool)
	if len(hashes) == 0 {
		return active
	}
	placeholders := make([]string, len(hashes))
	args := make([]any, 0, len(hashes)+2)
	for i, hash := range hashes {
		placeholders[i] = "?"
		args = append(args, hash)
	}
	args = append(args, ownerType, ownerID)
	query := fmt.Sprintf(
		`SELECT content_hash FROM mem_entries
		 WHERE status = 'active' AND content_hash IN (%s) AND owner_type = ? AND owner_id = ?`,
		strings.Join(placeholders, ","),
	)
	if scopeType != "" {
		if includeGlobal && scopeType != ScopeGlobal {
			query += ` AND ((scope_type = ? AND scope_id = ?) OR scope_type = ?)`
			args = append(args, scopeType, scopeID, ScopeGlobal)
		} else {
			query += ` AND scope_type = ? AND scope_id = ?`
			args = append(args, scopeType, scopeID)
		}
	}
	rows, err := m.db.QueryContext(ctx, query, args...)
	if err != nil {
		return active
	}
	defer rows.Close()
	for rows.Next() {
		var hash string
		if rows.Scan(&hash) == nil {
			active[hash] = true
		}
	}
	return active
}

// Delete removes a memory by content hash.
func (m *MemoryStore) Delete(ctx context.Context, contentHash string) error {
	return m.DeleteOwned(ctx, contentHash, OwnerL1, "")
}

func (m *MemoryStore) DeleteOwned(ctx context.Context, contentHash, ownerType, ownerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err := m.db.ExecContext(ctx, `DELETE FROM mem_entries WHERE content_hash = ? AND owner_type = ? AND owner_id = ?`, contentHash, ownerType, ownerID)
	return err
}

// hashContent returns the hex-encoded SHA-256 of content.
func hashContent(content string) string {
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", h)
}

func (m *MemoryStore) logWarn(msg string, err error) {
	if m.log != nil && err != nil {
		m.log.WarnContext(context.Background(), logger.CatApp, msg, "err", err.Error())
	}
}

// tokenizeForFTS5 splits a query into tokens, escapes FTS5 special characters,
// and quotes each token for safe FTS5 matching.
func tokenizeForFTS5(query string) []string {
	// Clean: remove special FTS5 syntax characters
	replacer := strings.NewReplacer(
		"^", " ", "*", " ", "\"", " ", "(", " ", ")", " ",
		"+", " ", "-", " ", "~", " ", "[", " ", "]", " ",
		"{", " ", "}", " ",
	)
	cleaned := replacer.Replace(query)

	// Remove FTS5 operators as standalone words
	for _, op := range []string{"AND", "OR", "NOT", "NEAR"} {
		cleaned = strings.ReplaceAll(cleaned, " "+op+" ", "  ")
	}

	words := strings.Fields(cleaned)
	if len(words) == 0 {
		return nil
	}

	tokens := make([]string, 0, len(words))
	for _, w := range words {
		if len(w) == 0 {
			continue
		}
		tokens = append(tokens, `"`+w+`"`)
	}
	return tokens
}
