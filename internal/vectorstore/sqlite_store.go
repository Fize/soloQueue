package vectorstore

import (
	"context"
	"database/sql"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStore stores memory entries in a SQLite database.
// Embeddings are serialized as little-endian float32 BLOBs.
// Writes are serialized via mutex; reads are concurrent.
type SQLiteStore struct {
	db  *sql.DB
	mu  sync.Mutex // serializes Upsert calls (SQLite single-writer)
}

// NewSQLiteStore opens or creates a SQLite-backed vector store.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL")
	if err != nil {
		return nil, err
	}

	s := &SQLiteStore{db: db}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS memories (
		id TEXT PRIMARY KEY,
		content TEXT NOT NULL,
		embedding BLOB NOT NULL,
		timestamp TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// Upsert inserts or replaces a memory entry.
func (s *SQLiteStore) Upsert(ctx context.Context, entry MemoryEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	embedBlob := encodeEmbedding(entry.Embedding)

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO memories (id, content, embedding, timestamp, source) VALUES (?, ?, ?, ?, ?)`,
		entry.ID, entry.Content, embedBlob, entry.Timestamp.Format(time.RFC3339), entry.Source,
	)
	return err
}

// Query returns the top-K entries most similar to the query embedding.
func (s *SQLiteStore) Query(ctx context.Context, embedding []float32, topK int, minSimilarity float32) ([]MemoryEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `SELECT id, content, embedding, timestamp, source FROM memories`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type scored struct {
		entry MemoryEntry
		score float32
	}

	var results []scored
	for rows.Next() {
		var entry MemoryEntry
		var embedBlob []byte
		var ts string
		if err := rows.Scan(&entry.ID, &entry.Content, &embedBlob, &ts, &entry.Source); err != nil {
			continue
		}
		entry.Timestamp, _ = time.Parse(time.RFC3339, ts)
		entry.Embedding = decodeEmbedding(embedBlob)

		sim := CosineSimilarity(embedding, entry.Embedding)
		if sim >= minSimilarity {
			results = append(results, scored{entry: entry, score: sim})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if topK > len(results) {
		topK = len(results)
	}

	out := make([]MemoryEntry, topK)
	for i := 0; i < topK; i++ {
		out[i] = results[i].entry
	}
	return out, nil
}

// Count returns the number of entries in the store.
func (s *SQLiteStore) Count(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memories`).Scan(&n)
	return n, err
}

// encodeEmbedding serializes []float32 to little-endian bytes.
func encodeEmbedding(vec []float32) []byte {
	if len(vec) == 0 {
		return nil
	}
	b := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(v))
	}
	return b
}

// decodeEmbedding deserializes bytes back to []float32.
func decodeEmbedding(b []byte) []float32 {
	if len(b) == 0 {
		return nil
	}
	vec := make([]float32, len(b)/4)
	for i := range vec {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return vec
}

// Compile-time check
var _ VectorStore = (*SQLiteStore)(nil)
