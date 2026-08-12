package vectorstore

import (
	"container/heap"
	"context"
	"database/sql"
	"encoding/binary"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/db"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

// SQLiteStore stores memory entries in a SQLite database.
// Embeddings are serialized as little-endian float32 BLOBs.
// Writes are serialized via mutex; reads are concurrent.
type SQLiteStore struct {
	db        *sql.DB
	mu        *sync.Mutex // serializes writes (SQLite single-writer); may be shared with other stores
	tableName string      // SQL table name (default "memories")
	// ownsDB indicates whether Close should close the underlying *sql.DB.
	// When a caller injects a shared DB via NewSQLiteStoreFromDB, ownership
	// stays with the caller and ownsDB is false.
	ownsDB   bool
	sharedDB *db.DB // non-nil only when this store owns the *db.DB (path-based constructor)
	log      *logger.Logger
}

// WithLogger sets the logger for the SQLiteStore. If nil, debug-level
// diagnostic messages are silently discarded.
func WithLogger(l *logger.Logger) func(*SQLiteStore) {
	return func(s *SQLiteStore) { s.log = l }
}

// WithTableName sets the table name for the SQLiteStore. Default is "memories".
// Use this to avoid collisions when multiple vector stores share the same database.
func WithTableName(name string) func(*SQLiteStore) {
	return func(s *SQLiteStore) { s.tableName = name }
}

// NewSQLiteStore opens or creates a SQLite-backed vector store that owns
// its own connection. Prefer NewSQLiteStoreFromDB when the same database
// file is shared with other stores (e.g. the todo store).
func NewSQLiteStore(path string, opts ...func(*SQLiteStore)) (*SQLiteStore, error) {
	shared, err := db.Open(path)
	if err != nil {
		return nil, err
	}
	s := &SQLiteStore{
		db:        shared.DB,
		mu:        &shared.WMu,
		tableName: "memories",
		ownsDB:    true,
		sharedDB:  shared,
	}
	for _, opt := range opts {
		opt(s)
	}
	if err := s.initTable(context.Background()); err != nil {
		shared.Close()
		return nil, err
	}
	return s, nil
}

// NewSQLiteStoreFromDB wires the vector store onto an externally managed
// shared database. The caller owns db and is responsible for closing it.
// mu must be the write mutex shared by all stores on the same file so that
// writes are serialized across stores (SQLite allows only one writer).
func NewSQLiteStoreFromDB(db *sql.DB, mu *sync.Mutex, opts ...func(*SQLiteStore)) *SQLiteStore {
	s := &SQLiteStore{db: db, mu: mu, tableName: "memories", ownsDB: false}
	for _, opt := range opts {
		opt(s)
	}
	_ = s.initTable(context.Background())
	return s
}

func (s *SQLiteStore) initTable(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `CREATE TABLE IF NOT EXISTS ` + s.tableName + ` (
		id TEXT PRIMARY KEY,
		content TEXT NOT NULL,
		embedding BLOB NOT NULL,
		timestamp TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT '',
		owner_type TEXT NOT NULL DEFAULT 'l1',
		owner_id TEXT NOT NULL DEFAULT '',
		scope_type TEXT NOT NULL DEFAULT 'global',
		scope_id TEXT NOT NULL DEFAULT ''
	)`
	if _, err := s.db.ExecContext(ctx, query); err != nil {
		return err
	}
	for _, column := range []struct{ name, ddl string }{
		{"owner_type", "TEXT NOT NULL DEFAULT 'l1'"},
		{"owner_id", "TEXT NOT NULL DEFAULT ''"},
		{"scope_type", "TEXT NOT NULL DEFAULT 'global'"},
		{"scope_id", "TEXT NOT NULL DEFAULT ''"},
	} {
		present, err := s.hasColumn(ctx, column.name)
		if err != nil {
			return err
		}
		if !present {
			if _, err := s.db.ExecContext(ctx, `ALTER TABLE `+s.tableName+` ADD COLUMN `+column.name+` `+column.ddl); err != nil {
				return err
			}
		}
	}
	_, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_`+s.tableName+`_owner_scope ON `+s.tableName+` (owner_type, owner_id, scope_type, scope_id)`)
	return err
}

func (s *SQLiteStore) hasColumn(ctx context.Context, name string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+s.tableName+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var columnName, columnType string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if columnName == name {
			return true, nil
		}
	}
	return false, rows.Err()
}

// Close releases resources owned by this store. When the store was created
// via NewSQLiteStoreFromDB it does NOT close the underlying database,
// because the caller retains ownership.
func (s *SQLiteStore) Close() error {
	if s.ownsDB && s.sharedDB != nil {
		return s.sharedDB.Close()
	}
	return nil
}

// Upsert inserts or replaces a memory entry.
func (s *SQLiteStore) Upsert(ctx context.Context, entry MemoryEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	embedBlob := encodeEmbedding(entry.Embedding)

	s.mu.Lock()
	defer s.mu.Unlock()

	query := `INSERT OR REPLACE INTO ` + s.tableName + `
		(id, content, embedding, timestamp, source, owner_type, owner_id, scope_type, scope_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query,
		entry.ID, entry.Content, embedBlob, entry.Timestamp.Format(time.RFC3339), entry.Source,
		defaultOwnerType(entry.OwnerType), entry.OwnerID, defaultScopeType(entry.ScopeType), entry.ScopeID,
	)
	return err
}

// Query returns the top-K entries most similar to the query embedding.
func (s *SQLiteStore) Query(ctx context.Context, embedding []float32, topK int, minSimilarity float32) ([]MemoryEntry, error) {
	return s.QueryScoped(ctx, embedding, topK, minSimilarity, QueryFilter{OwnerType: "l1"})
}

func (s *SQLiteStore) QueryScoped(ctx context.Context, embedding []float32, topK int, minSimilarity float32, filter QueryFilter) ([]MemoryEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if topK <= 0 {
		return []MemoryEntry{}, nil
	}

	// Pre-normalize the query vector once to avoid recomputing its norm
	// for every row. We compute dot(queryNorm, b) / normB per row, which
	// is numerically equivalent to the original CosineSimilarity formula
	// up to float32 rounding.
	queryNorm, queryHasNorm := NormalizeVector(embedding)

	ownerType := defaultOwnerType(filter.OwnerType)
	query := `SELECT id, content, embedding, timestamp, source, owner_type, owner_id, scope_type, scope_id FROM ` + s.tableName + ` WHERE owner_type = ? AND owner_id = ?`
	args := []any{ownerType, filter.OwnerID}
	if filter.ScopeType != "" {
		if filter.IncludeGlobal && filter.ScopeType != "global" {
			query += ` AND ((scope_type = ? AND scope_id = ?) OR scope_type = 'global')`
			args = append(args, filter.ScopeType, filter.ScopeID)
		} else {
			query += ` AND scope_type = ? AND scope_id = ?`
			args = append(args, filter.ScopeType, filter.ScopeID)
		}
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	h := &scoredHeap{}
	heap.Init(h)

	// Reusable decode buffer for rows that do NOT make it into the heap.
	// Rows that DO enter the heap get their own copy (we still need to
	// return entry.Embedding in the result, preserving the original
	// Query semantics). Size is fixed on first decode.
	var buf []float32
	var scanned, kept int
	for rows.Next() {
		scanned++
		var (
			id, content, source, ownerType, ownerID, scopeType, scopeID string
			embedBlob                                                   []byte
			ts                                                          string
		)
		if err := rows.Scan(&id, &content, &embedBlob, &ts, &source, &ownerType, &ownerID, &scopeType, &scopeID); err != nil {
			if s.log != nil {
				s.log.DebugContext(ctx, logger.CatApp, "vectorstore: skip row due to scan error",
					"err", err.Error(),
				)
			}
			continue
		}

		// Decode embedding into buf (allocate/resize once).
		n := len(embedBlob) / 4
		if n == 0 {
			if s.log != nil {
				s.log.DebugContext(ctx, logger.CatApp, "vectorstore: skip row with empty embedding",
					"id", id,
				)
			}
			continue
		}
		if cap(buf) < n {
			buf = make([]float32, n)
		} else {
			buf = buf[:n]
		}
		for i := 0; i < n; i++ {
			buf[i] = math.Float32frombits(binary.LittleEndian.Uint32(embedBlob[i*4:]))
		}

		// Compute similarity. If dimensions mismatch or either norm is
		// zero, similarity is 0 (same as CosineSimilarity).
		var sim float32
		if queryHasNorm && len(queryNorm) == n {
			dot, normB := dotAndNormB(queryNorm, buf)
			if normB > 0 {
				sim = float32(dot / normB)
			}
		}

		if sim < minSimilarity {
			continue
		}

		// Candidate qualifies. If heap not full, push. Otherwise only
		// replace the min if strictly better.
		if h.Len() < topK {
			entry := MemoryEntry{
				ID:        id,
				Content:   content,
				Embedding: append([]float32(nil), buf...),
				Source:    source,
				OwnerType: ownerType,
				OwnerID:   ownerID,
				ScopeType: scopeType,
				ScopeID:   scopeID,
			}
			entry.Timestamp, _ = time.Parse(time.RFC3339, ts)
			heap.Push(h, scored{entry: entry, score: sim})
			kept++
		} else if sim > (*h)[0].score {
			entry := MemoryEntry{
				ID:        id,
				Content:   content,
				Embedding: append([]float32(nil), buf...),
				Source:    source,
				OwnerType: ownerType,
				OwnerID:   ownerID,
				ScopeType: scopeType,
				ScopeID:   scopeID,
			}
			entry.Timestamp, _ = time.Parse(time.RFC3339, ts)
			(*h)[0] = scored{entry: entry, score: sim}
			heap.Fix(h, 0)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Drain heap into descending-order slice.
	size := h.Len()
	out := make([]MemoryEntry, size)
	for i := size - 1; i >= 0; i-- {
		out[i] = heap.Pop(h).(scored).entry
	}

	if s.log != nil {
		s.log.DebugContext(ctx, logger.CatApp, "vectorstore: query stats",
			"scanned", scanned,
			"kept", kept,
			"returned", len(out),
			"topK", topK,
			"minSim", minSimilarity,
		)
	}
	return out, nil
}

// Count returns the number of entries in the store.
func (s *SQLiteStore) Count(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+s.tableName).Scan(&n)
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
var _ ScopedVectorStore = (*SQLiteStore)(nil)

func defaultOwnerType(value string) string {
	if strings.TrimSpace(value) == "" {
		return "l1"
	}
	return value
}

func defaultScopeType(value string) string {
	if strings.TrimSpace(value) == "" {
		return "global"
	}
	return value
}

// --- internal helpers for Query (kept package-private) ---

// scored pairs an entry with its similarity score.
type scored struct {
	entry MemoryEntry
	score float32
}

// scoredHeap is a min-heap of scored items (smallest score at index 0).
// Used by Query to maintain the top-K largest scores in O(n log K).
type scoredHeap []scored

func (h scoredHeap) Len() int            { return len(h) }
func (h scoredHeap) Less(i, j int) bool  { return h[i].score < h[j].score }
func (h scoredHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *scoredHeap) Push(x interface{}) { *h = append(*h, x.(scored)) }
func (h *scoredHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// NormalizeVector returns a unit-length copy of v. The second return
// value is false when v has zero norm (or is empty), meaning any cosine
// similarity against it is 0.
func NormalizeVector(v []float32) ([]float32, bool) {
	if len(v) == 0 {
		return nil, false
	}
	var sq float64
	for _, x := range v {
		sq += float64(x) * float64(x)
	}
	if sq == 0 {
		return nil, false
	}
	inv := 1.0 / math.Sqrt(sq)
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(float64(x) * inv)
	}
	return out, true
}

// dotAndNormB returns (dot(aNorm, b), |b|) in one pass. aNorm is assumed
// to be already unit-normalized; only b's norm is computed. Both slices
// must have the same length (checked by the caller).
func dotAndNormB(aNorm, b []float32) (float64, float64) {
	var dot, sqB float64
	for i := range aNorm {
		ai := float64(aNorm[i])
		bi := float64(b[i])
		dot += ai * bi
		sqB += bi * bi
	}
	return dot, math.Sqrt(sqB)
}
