package vectorstore

import (
	"container/heap"
	"context"
	"database/sql"
	"encoding/binary"
	"math"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
)

// SQLiteStore stores memory entries in a SQLite database.
// Embeddings are serialized as little-endian float32 BLOBs.
// Writes are serialized via mutex; reads are concurrent.
type SQLiteStore struct {
	db *sql.DB
	mu *sync.Mutex // serializes writes (SQLite single-writer); may be shared with other stores
	// ownsDB indicates whether Close should close the underlying *sql.DB.
	// When a caller injects a shared DB via NewSQLiteStoreFromDB, ownership
	// stays with the caller and ownsDB is false.
	ownsDB   bool
	sharedDB *sqlitedb.DB // non-nil only when this store owns the *sqlitedb.DB (path-based constructor)
	log      *logger.Logger
}

// WithLogger sets the logger for the SQLiteStore. If nil, debug-level
// diagnostic messages are silently discarded.
func WithLogger(l *logger.Logger) func(*SQLiteStore) {
	return func(s *SQLiteStore) { s.log = l }
}

// NewSQLiteStore opens or creates a SQLite-backed vector store that owns
// its own connection. Prefer NewSQLiteStoreFromDB when the same database
// file is shared with other stores (e.g. the todo store).
func NewSQLiteStore(path string, opts ...func(*SQLiteStore)) (*SQLiteStore, error) {
	shared, err := sqlitedb.Open(path)
	if err != nil {
		return nil, err
	}
	s := &SQLiteStore{
		db:       shared.DB,
		mu:       &shared.WMu,
		ownsDB:   true,
		sharedDB: shared,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// NewSQLiteStoreFromDB wires the vector store onto an externally managed
// shared database. The caller owns db and is responsible for closing it.
// mu must be the write mutex shared by all stores on the same file so that
// writes are serialized across stores (SQLite allows only one writer).
func NewSQLiteStoreFromDB(db *sql.DB, mu *sync.Mutex, opts ...func(*SQLiteStore)) *SQLiteStore {
	s := &SQLiteStore{db: db, mu: mu, ownsDB: false}
	for _, opt := range opts {
		opt(s)
	}
	return s
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
	if topK <= 0 {
		return []MemoryEntry{}, nil
	}

	// Pre-normalize the query vector once to avoid recomputing its norm
	// for every row. We compute dot(queryNorm, b) / normB per row, which
	// is numerically equivalent to the original CosineSimilarity formula
	// up to float32 rounding.
	queryNorm, queryHasNorm := NormalizeVector(embedding)

	rows, err := s.db.QueryContext(ctx, `SELECT id, content, embedding, timestamp, source FROM memories`)
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
			id, content, source string
			embedBlob           []byte
			ts                  string
		)
		if err := rows.Scan(&id, &content, &embedBlob, &ts, &source); err != nil {
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
