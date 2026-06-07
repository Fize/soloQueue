package memoryengine

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/memoryengine/embedding"
	"github.com/xiaobaitu/soloqueue/internal/memoryengine/vectorstore"
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
func (m *MemoryStore) Save(ctx context.Context, content, date, tags, eventTime string) (contentHash string, isNew bool, err error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}

	contentHash = hashContent(content)
	now := time.Now().UTC().Format(time.RFC3339)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for duplicate by content_hash
	var existing string
	err = m.db.QueryRowContext(ctx,
		`SELECT id FROM mem_entries WHERE content_hash = ?`, contentHash,
	).Scan(&existing)
	if err == nil {
		return contentHash, false, nil // already exists
	}
	if err != sql.ErrNoRows {
		return "", false, fmt.Errorf("memory save: %w", err)
	}

	id := contentHash[:16]

	_, err = m.db.ExecContext(ctx,
		`INSERT INTO mem_entries (id, content, content_hash, date, tags, event_time, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, content, contentHash, date, tags, eventTime, now,
	)
	if err != nil {
		return "", false, fmt.Errorf("memory save: %w", err)
	}

	// Store vector embedding if enabled
	if m.embedder != nil && m.vecStore != nil {
		results, embErr := m.embedder.Embed(ctx, []string{content})
		if embErr != nil {
			m.logWarn("memory save: embed failed", embErr)
		} else if len(results) > 0 {
			if err := m.vecStore.Upsert(ctx, vectorstore.MemoryEntry{
				ID:        id,
				Content:   content,
				Embedding: results[0].Embedding,
				Timestamp: time.Now().UTC(),
				Source:    "memoryengine",
			}); err != nil {
				m.logWarn("memory save: vector upsert failed", err)
			}
		}
	}

	return contentHash, true, nil
}

// GetByContentHashes fetches multiple memories by their content hashes.
func (m *MemoryStore) GetByContentHashes(ctx context.Context, hashes []string) ([]MemoryEntry, error) {
	if len(hashes) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(hashes))
	args := make([]interface{}, len(hashes))
	for i, h := range hashes {
		placeholders[i] = "?"
		args[i] = h
	}

	query := fmt.Sprintf(
		`SELECT id, content, content_hash, date, tags, event_time, salience, created_at
		 FROM mem_entries WHERE content_hash IN (%s)`,
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
		if err := rows.Scan(&e.ID, &e.Content, &e.ContentHash, &e.Date, &e.Tags, &e.EventTime, &e.Salience, &e.CreatedAt); err != nil {
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
	if limit <= 0 {
		limit = 20
	}

	tokens := tokenizeForFTS5(query)
	if len(tokens) == 0 {
		return nil, 0, nil
	}

	ftsQuery := strings.Join(tokens, " ")

	rows, err := m.db.QueryContext(ctx,
		`SELECT m.id, m.content, m.date, m.tags, m.content_hash, m.event_time, rank
		 FROM mem_fts JOIN mem_entries m ON m.rowid = mem_fts.rowid
		 WHERE mem_fts MATCH ?
		 ORDER BY rank
		 LIMIT ?`,
		ftsQuery, limit,
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
		if err := rows.Scan(&r.ContentHash, &r.Content, &r.Date, &r.Tags, &r.ContentHash, &r.EventTime, &rank); err != nil {
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
	if limit <= 0 {
		limit = 50
	}

	query := `SELECT id, content, content_hash, date, tags, event_time, salience, created_at
		 FROM mem_entries WHERE 1=1`
	var args []interface{}

	if from != "" {
		query += ` AND event_time >= ?`
		args = append(args, from)
	}
	if to != "" {
		query += ` AND event_time <= ?`
		args = append(args, to)
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
		if err := rows.Scan(&e.ID, &e.Content, &e.ContentHash, &e.Date, &e.Tags, &e.EventTime, &e.Salience, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// BoostSalience increases the salience of a memory (called on recall).
func (m *MemoryStore) BoostSalience(ctx context.Context, contentHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := m.db.ExecContext(ctx,
		`UPDATE mem_entries SET salience = MIN(2.0, salience + 0.3), last_recalled_at = ? WHERE content_hash = ?`,
		now, contentHash,
	)
	return err
}

// Count returns the total number of stored memories.
func (m *MemoryStore) Count(ctx context.Context) (int, error) {
	var n int
	err := m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mem_entries`).Scan(&n)
	return n, err
}

// Delete removes a memory by content hash.
func (m *MemoryStore) Delete(ctx context.Context, contentHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err := m.db.ExecContext(ctx, `DELETE FROM mem_entries WHERE content_hash = ?`, contentHash)
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
