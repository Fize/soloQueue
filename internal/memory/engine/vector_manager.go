package engine

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/memory/engine/embedding"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine/embedding/local"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine/embedding/managed"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine/vectorstore"
)

// LocalSpaceID binds preprocessing and immutable artifacts, excluding device/backend.
var LocalSpaceID = fmt.Sprintf("e5-small-%x", sha256.Sum256([]byte(managed.Revision+managed.ModelHash+managed.TokenizerHash+"query: |passage: |mean-mask|l2|max512|dim384|tokenizers0.22.2")))
var ErrLocalInactive = errors.New("local embedding is inactive")

type VectorConfig struct {
	Provider                 string
	Enabled                  bool
	Model, Device, RemoteKey string
}
type EmbeddingStatus struct {
	Provider        string `json:"provider"`
	State           string `json:"state"`
	Model           string `json:"model"`
	Device          string `json:"device"`
	Backend         string `json:"backend"`
	Dimension       int    `json:"dimension"`
	DownloadedBytes int64  `json:"downloadedBytes"`
	TotalBytes      int64  `json:"totalBytes"`
	Indexed         int64  `json:"indexed"`
	Pending         int64  `json:"pending"`
	Failed          int64  `json:"failed"`
	Error           string `json:"error,omitempty"`
	FallbackReason  string `json:"fallbackReason,omitempty"`
}
type LocalWorker interface {
	Embed(context.Context, []string, string) (local.Batch, error)
	Status() local.Status
	Close() error
}
type VectorOptions struct {
	Prepare func(context.Context, managed.Progress) (managed.Paths, error)
	Start   func(context.Context, local.Config) (LocalWorker, error)
}
type localBinding struct {
	worker     LocalWorker
	space      string
	ctx        context.Context
	generation uint64
}

// VectorManager is a stable dependency; an operation always retains one binding.
type VectorManager struct {
	applyMu    sync.Mutex
	db         *sql.DB
	writeMu    *sync.Mutex
	mu         sync.Mutex
	cfg        VectorConfig
	status     EmbeddingStatus
	binding    *localBinding
	remote     embedding.Embedder
	legacy     vectorstore.VectorStore
	generation uint64
	cancel     context.CancelFunc
	lastDone   chan struct{}
	wake       chan struct{}
	closed     bool
	wg         sync.WaitGroup
	profileErr error
	dataErr    error
	initErr    error
	options    VectorOptions
}

func NewVectorManager(db *sql.DB, writeMu *sync.Mutex, workDir string, options VectorOptions) *VectorManager {
	if options.Prepare == nil {
		installer := managed.Installer{WorkDir: workDir}
		options.Prepare = installer.Prepare
	}
	if options.Start == nil {
		options.Start = func(ctx context.Context, cfg local.Config) (LocalWorker, error) { return local.Start(ctx, cfg) }
	}
	done := make(chan struct{})
	close(done)
	m := &VectorManager{db: db, writeMu: writeMu, options: options, wake: make(chan struct{}, 1), lastDone: done, status: EmbeddingStatus{Provider: "none", State: "disabled", Model: "multilingual-e5-small", Device: "auto"}}
	writeMu.Lock()
	_, m.initErr = db.Exec(`CREATE TABLE IF NOT EXISTS mem_vec_local (space_id TEXT NOT NULL,owner_type TEXT NOT NULL,owner_id TEXT NOT NULL,content_hash TEXT NOT NULL,content TEXT NOT NULL,embedding BLOB NOT NULL,PRIMARY KEY(space_id,owner_type,owner_id,content_hash))`)
	if m.initErr == nil {
		_, m.initErr = db.Exec(`CREATE TABLE IF NOT EXISTS mem_vec_profile_meta (key TEXT PRIMARY KEY,value TEXT NOT NULL)`)
	}
	writeMu.Unlock()
	return m
}
func normalizedVectorConfig(cfg VectorConfig) VectorConfig {
	if cfg.Provider == "" {
		cfg.Provider = "none"
	}
	if cfg.Model == "" {
		cfg.Model = "multilingual-e5-small"
	}
	if cfg.Device == "" {
		cfg.Device = "auto"
	}
	if cfg.Provider == "onnx" {
		cfg.RemoteKey = ""
	}
	return cfg
}
func (m *VectorManager) Apply(cfg VectorConfig, remote embedding.Embedder, legacy vectorstore.VectorStore) {
	m.applyMu.Lock()
	defer m.applyMu.Unlock()
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return
	}
	// Anchor the saved remote definition even while local mode is selected.
	selected, err := m.remoteNamespace(cfg.RemoteKey, legacy)
	m.mu.Lock()
	m.profileErr = err
	m.mu.Unlock()
	m.apply(normalizedVectorConfig(cfg), remote, selected, false, false)
}
func (m *VectorManager) remoteNamespace(identity string, legacy vectorstore.VectorStore) (vectorstore.VectorStore, error) {
	if m.initErr != nil {
		return nil, m.initErr
	}
	if identity == "" {
		return legacy, nil
	}
	m.writeMu.Lock()
	_, err := m.db.Exec(`INSERT OR IGNORE INTO mem_vec_profile_meta(key,value) VALUES('legacy_remote_profile',?)`, identity)
	m.writeMu.Unlock()
	if err != nil {
		return nil, err
	}
	var anchor string
	if err = m.db.QueryRow(`SELECT value FROM mem_vec_profile_meta WHERE key='legacy_remote_profile'`).Scan(&anchor); err != nil {
		return nil, err
	}
	if identity == anchor {
		return legacy, nil
	}
	if legacy == nil {
		return nil, nil
	}
	table := fmt.Sprintf("mem_vec_remote_%x", sha256.Sum256([]byte(identity)))
	return vectorstore.NewSQLiteStoreFromDB(m.db, m.writeMu, vectorstore.WithTableName(table)), nil
}

func (m *VectorManager) apply(cfg VectorConfig, remote embedding.Embedder, legacy vectorstore.VectorStore, force, rebuild bool) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	if !force && m.cfg == cfg {
		// Model/provider activation changes availability, not vector identity.
		// Refresh remote resources while keeping the namespace and local lifecycle.
		if cfg.Provider == "openai" {
			m.remote = remote
			m.legacy = legacy
			m.status = EmbeddingStatus{Provider: cfg.Provider, State: "disabled", Model: "remote", Device: cfg.Device}
			if remote != nil && legacy != nil {
				m.status.State = "ready"
				m.status.Dimension = remote.Dimension()
			}
		}
		m.mu.Unlock()
		return
	}
	if m.cancel != nil {
		m.cancel()
	}
	previous := m.lastDone
	m.generation++
	generation := m.generation
	m.cfg = cfg
	m.dataErr = nil
	m.binding = nil
	m.remote = remote
	m.legacy = legacy
	m.status = EmbeddingStatus{Provider: cfg.Provider, State: "disabled", Model: cfg.Model, Device: cfg.Device}
	active := cfg.Provider == "onnx" && cfg.Enabled
	if !active {
		if cfg.Provider == "openai" && remote != nil {
			m.status.State = "ready"
			m.status.Model = "remote"
			m.status.Dimension = remote.Dimension()
		}
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	done := make(chan struct{})
	m.lastDone = done
	m.status.State = "preparing"
	m.wg.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.wg.Done()
		defer close(done)
		select {
		case <-previous:
		case <-ctx.Done():
			return
		}
		m.run(ctx, generation, cfg, rebuild)
	}()
}
func (m *VectorManager) Status() EmbeddingStatus { m.mu.Lock(); defer m.mu.Unlock(); return m.status }
func (m *VectorManager) Action(action string) (EmbeddingStatus, error) {
	m.applyMu.Lock()
	defer m.applyMu.Unlock()
	m.mu.Lock()
	cfg := m.cfg
	if m.closed || cfg.Provider != "onnx" || !cfg.Enabled {
		m.mu.Unlock()
		return EmbeddingStatus{}, ErrLocalInactive
	}
	if action == "cancel" {
		if m.cancel != nil {
			m.cancel()
		}
		m.binding = nil
		m.status.State = "cancelled"
		m.status.Error = ""
		status := m.status
		m.mu.Unlock()
		return status, nil
	}
	if action != "retry" && action != "reindex" {
		m.mu.Unlock()
		return EmbeddingStatus{}, errors.New("invalid embedding action")
	}
	m.mu.Unlock()
	m.apply(cfg, nil, nil, true, action == "reindex")
	return m.Status(), nil
}
func (m *VectorManager) Close() {
	m.applyMu.Lock()
	m.mu.Lock()
	m.closed = true
	if m.cancel != nil {
		m.cancel()
	}
	m.binding = nil
	m.mu.Unlock()
	m.applyMu.Unlock()
	m.wg.Wait()
}
func (m *VectorManager) update(generation uint64, fn func(*EmbeddingStatus)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if generation == m.generation && !m.closed && m.status.State != "cancelled" && m.status.State != "error" {
		fn(&m.status)
	}
}
func (m *VectorManager) fail(generation uint64, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.generation != generation || m.closed || m.status.State == "cancelled" || m.status.State == "error" {
		return
	}
	m.status.State = "error"
	m.status.Error = err.Error()
	m.status.Failed++
	m.binding = nil
	if m.cancel != nil {
		m.cancel()
	}
}

// A worker failure is the only catchup error eligible for BM25/KG fallback.
// Storage errors remain visible to subsequent searches after the worker stops.
type localInferenceFailure struct{ cause error }

func (e localInferenceFailure) Error() string { return "index local memory: " + e.cause.Error() }
func (e localInferenceFailure) Unwrap() error { return e.cause }
func (m *VectorManager) failData(generation uint64, err error) {
	m.mu.Lock()
	if generation == m.generation && !m.closed && m.status.State != "cancelled" {
		m.dataErr = err
	}
	m.mu.Unlock()
	m.fail(generation, err)
}
func (m *VectorManager) run(ctx context.Context, generation uint64, cfg VectorConfig, rebuild bool) {
	if m.initErr != nil {
		m.fail(generation, m.initErr)
		return
	}
	if cfg.Model != "multilingual-e5-small" || (cfg.Device != "auto" && cfg.Device != "cpu") {
		m.fail(generation, errors.New("unsupported local embedding model or device"))
		return
	}
	if err := m.counts(ctx, &localBinding{space: LocalSpaceID, generation: generation}); err != nil {
		if ctx.Err() == nil {
			m.failData(generation, err)
		}
		return
	}
	paths, err := m.options.Prepare(ctx, func(state string, done, total int64) {
		m.update(generation, func(s *EmbeddingStatus) { s.State = state; s.DownloadedBytes = done; s.TotalBytes = total })
	})
	if err != nil {
		if ctx.Err() == nil {
			m.fail(generation, err)
		}
		return
	}
	if ctx.Err() != nil {
		return
	}
	m.update(generation, func(s *EmbeddingStatus) { s.State = "loading" })
	worker, err := m.options.Start(ctx, local.Config{Worker: paths.Worker, ModelDir: paths.ModelDir, Runtime: paths.Runtime, Device: cfg.Device})
	if err != nil {
		if ctx.Err() == nil {
			m.fail(generation, err)
		}
		return
	}
	defer worker.Close()
	// Closing the client interrupts an active native inference when a generation is canceled.
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			_ = worker.Close()
		case <-stopped:
		}
	}()
	binding := &localBinding{worker: worker, space: LocalSpaceID, ctx: ctx, generation: generation}
	if rebuild {
		m.writeMu.Lock()
		_, err = m.db.ExecContext(ctx, `DELETE FROM mem_vec_local WHERE space_id=?`, binding.space)
		m.writeMu.Unlock()
		if err != nil {
			if ctx.Err() == nil {
				m.failData(generation, err)
			}
			return
		}
	}
	m.mu.Lock()
	if m.generation != generation || ctx.Err() != nil || m.closed {
		m.mu.Unlock()
		return
	}
	m.binding = binding
	status := worker.Status()
	m.status.Backend = status.Backend
	m.status.Dimension = status.Dimension
	if status.FallbackReason != nil {
		m.status.FallbackReason = *status.FallbackReason
	}
	m.status.State = "reindexing"
	m.mu.Unlock()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if err = m.catchup(ctx, binding); err != nil {
			if ctx.Err() == nil {
				var inference localInferenceFailure
				if errors.As(err, &inference) {
					m.fail(generation, err)
				} else {
					m.failData(generation, err)
				}
			}
			return
		}
		if ctx.Err() != nil {
			return
		}
		m.update(generation, func(s *EmbeddingStatus) { s.State = "ready"; s.Error = "" })
		select {
		case <-ctx.Done():
			return
		case <-m.wake:
		case <-ticker.C:
		}
	}
}
func (m *VectorManager) Notify() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}
func (m *VectorManager) active() (*localBinding, embedding.Embedder, vectorstore.VectorStore, VectorConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.binding, m.remote, m.legacy, m.cfg
}
func (m *VectorManager) Enabled() bool {
	_, remote, legacy, cfg := m.active()
	return (cfg.Provider == "onnx" && cfg.Enabled) || (cfg.Provider == "openai" && remote != nil && legacy != nil)
}

// IndexSaved preserves legacy synchronous remote behavior; local indexing is a
// durable authoritative catchup, so writes remain available during installation.
func (m *VectorManager) IndexSaved(ctx context.Context, id, hash, content, ownerType, ownerID, scopeType, scopeID string) {
	_, remote, legacy, cfg := m.active()
	if cfg.Provider == "onnx" {
		m.Notify()
		return
	}
	if cfg.Provider != "openai" || remote == nil || legacy == nil {
		return
	}
	results, err := remote.Embed(ctx, []string{content})
	if err == nil && len(results) > 0 {
		_ = legacy.Upsert(ctx, vectorstore.MemoryEntry{ID: id, Source: hash, Content: content, Embedding: results[0].Embedding, Timestamp: time.Now().UTC(), OwnerType: ownerType, OwnerID: ownerID, ScopeType: scopeType, ScopeID: scopeID})
	}
}
func (m *VectorManager) Search(ctx context.Context, query SearchQuery, limit int) ([]SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	binding, remote, legacy, cfg := m.active()
	m.mu.Lock()
	profileErr := m.profileErr
	dataErr := m.dataErr
	m.mu.Unlock()
	if dataErr != nil {
		return nil, dataErr
	}
	if profileErr != nil {
		return nil, profileErr
	}
	if cfg.Provider != "onnx" {
		results, err := NewVectorSearcher(remote, legacy).Search(ctx, query, limit)
		if err != nil {
			return nil, err
		}
		resolved := make([]SearchResult, 0, len(results))
		for _, result := range results {
			if len(result.ContentHash) == 16 {
				var hash string
				err = m.db.QueryRowContext(ctx, `SELECT content_hash FROM mem_entries WHERE id=? AND owner_type=? AND owner_id=?`, result.ContentHash, query.OwnerType, query.OwnerID).Scan(&hash)
				if err == sql.ErrNoRows {
					continue
				}
				if err != nil {
					return nil, err
				}
				result.ContentHash = hash
			}
			resolved = append(resolved, result)
		}
		return resolved, nil
	}
	if m.initErr != nil {
		return nil, m.initErr
	}
	if !cfg.Enabled || binding == nil || binding.ctx.Err() != nil {
		return nil, nil
	}
	batch, err := binding.worker.Embed(ctx, []string{query.Text}, "query")
	if err != nil {
		m.fail(binding.generation, errors.New("local inference stopped; retry local embedding"))
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, nil
	}
	if binding.ctx.Err() != nil {
		return nil, nil
	}
	m.recordBackend(binding, batch)
	results, err := m.queryLocal(ctx, binding.space, batch.Results[0].Embedding, query, limit)
	if err != nil && ctx.Err() == nil {
		m.failData(binding.generation, err)
	}
	return results, err
}
func (m *VectorManager) recordBackend(binding *localBinding, batch local.Batch) {
	m.update(binding.generation, func(s *EmbeddingStatus) {
		s.Backend = batch.Backend
		if batch.FallbackReason != nil {
			s.FallbackReason = *batch.FallbackReason
		}
	})
}

const localScopePredicate = ` ((e.scope_type='global' AND e.scope_id='') OR (e.scope_type IN ('project','team') AND length(trim(e.scope_id))>0)) AND (e.owner_type!='l2_group' OR (e.scope_type='team' AND e.scope_id=e.owner_id)) `
const localOwnerPredicate = ` ((e.owner_type='l1' AND e.owner_id='') OR (e.owner_type='l2_group' AND length(e.owner_id)=36 AND substr(e.owner_id,9,1)='-' AND substr(e.owner_id,14,1)='-' AND substr(e.owner_id,19,1)='-' AND substr(e.owner_id,24,1)='-' AND length(replace(e.owner_id,'-',''))=32 AND replace(e.owner_id,'-','') NOT GLOB '*[^0-9a-f]*')) `

type pendingMemory struct{ hash, content, ownerType, ownerID string }

func (m *VectorManager) catchup(ctx context.Context, binding *localBinding) error {
	for {
		if err := m.counts(ctx, binding); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, err := m.db.QueryContext(ctx, `SELECT e.content_hash,e.content,e.owner_type,e.owner_id FROM mem_entries e LEFT JOIN mem_vec_local v ON v.space_id=? AND v.owner_type=e.owner_type AND v.owner_id=e.owner_id AND v.content_hash=e.content_hash AND v.content=e.content WHERE e.status IN ('active','superseded') AND v.content_hash IS NULL AND `+localOwnerPredicate+` AND `+localScopePredicate+` ORDER BY e.created_at,e.content_hash LIMIT 16`, binding.space)
		if err != nil {
			return err
		}
		var pending []pendingMemory
		for rows.Next() {
			var entry pendingMemory
			if err = rows.Scan(&entry.hash, &entry.content, &entry.ownerType, &entry.ownerID); err != nil {
				break
			}
			if !validOwner(entry.ownerType, entry.ownerID) {
				continue
			}
			pending = append(pending, entry)
		}
		rowErr := rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if rowErr != nil {
			return rowErr
		}
		if len(pending) == 0 {
			return m.counts(ctx, binding)
		}
		m.update(binding.generation, func(s *EmbeddingStatus) { s.State = "reindexing" })
		texts := make([]string, len(pending))
		for i, entry := range pending {
			texts[i] = entry.content
		}
		batch, err := binding.worker.Embed(ctx, texts, "document")
		if err != nil {
			return localInferenceFailure{cause: err}
		}
		m.recordBackend(binding, batch)
		for i, entry := range pending {
			if err = ctx.Err(); err != nil {
				return err
			}
			blob := encodeLocal(batch.Results[i].Embedding)
			m.writeMu.Lock()
			_, err = m.db.ExecContext(ctx, `INSERT OR REPLACE INTO mem_vec_local(space_id,owner_type,owner_id,content_hash,content,embedding) SELECT ?,owner_type,owner_id,content_hash,content,? FROM mem_entries WHERE content_hash=? AND owner_type=? AND owner_id=? AND content=? AND status IN ('active','superseded')`, binding.space, blob, entry.hash, entry.ownerType, entry.ownerID, entry.content)
			m.writeMu.Unlock()
			if err != nil {
				return err
			}
		}
		if err = m.counts(ctx, binding); err != nil {
			return err
		}
	}
}
func (m *VectorManager) counts(ctx context.Context, b *localBinding) error {
	var indexed, pending int64
	err := m.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN v.content_hash IS NOT NULL THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN v.content_hash IS NULL THEN 1 ELSE 0 END),0) FROM mem_entries e LEFT JOIN mem_vec_local v ON v.space_id=? AND v.owner_type=e.owner_type AND v.owner_id=e.owner_id AND v.content_hash=e.content_hash AND v.content=e.content WHERE e.status IN ('active','superseded') AND `+localOwnerPredicate+` AND `+localScopePredicate, b.space).Scan(&indexed, &pending)
	if err == nil {
		m.update(b.generation, func(s *EmbeddingStatus) { s.Indexed = indexed; s.Pending = pending })
	}
	return err
}
func encodeLocal(vector []float32) []byte {
	blob := make([]byte, len(vector)*4)
	for i, v := range vector {
		binary.LittleEndian.PutUint32(blob[i*4:], math.Float32bits(v))
	}
	return blob
}
func decodeLocal(blob []byte) ([]float32, error) {
	if len(blob) != 384*4 {
		return nil, errors.New("invalid persisted local embedding dimension")
	}
	vector := make([]float32, 384)
	for i := range vector {
		vector[i] = math.Float32frombits(binary.LittleEndian.Uint32(blob[i*4:]))
		if math.IsNaN(float64(vector[i])) || math.IsInf(float64(vector[i]), 0) {
			return nil, errors.New("nonfinite persisted local embedding")
		}
	}
	return vector, nil
}
func (m *VectorManager) queryLocal(ctx context.Context, space string, vector []float32, q SearchQuery, limit int) ([]SearchResult, error) {
	if !validOwner(q.OwnerType, q.OwnerID) {
		return nil, ErrMemoryOwnerInvalid
	}
	sqlText := `SELECT e.content_hash,e.content,e.date,e.event_time,e.status,e.expires_at,e.valid_from,e.valid_until,e.scope_type,e.scope_id,v.embedding FROM mem_vec_local v JOIN mem_entries e ON e.owner_type=v.owner_type AND e.owner_id=v.owner_id AND e.content_hash=v.content_hash AND e.content=v.content WHERE v.space_id=? AND e.owner_type=? AND e.owner_id=?`
	sqlText += ` AND ` + localOwnerPredicate + ` AND ` + localScopePredicate
	args := []any{space, q.OwnerType, q.OwnerID}
	if q.ScopeType != "" {
		sqlText += ` AND ((e.scope_type=? AND e.scope_id=?)`
		args = append(args, q.ScopeType, q.ScopeID)
		if q.IncludeGlobal {
			sqlText += ` OR e.scope_type='global'`
		}
		sqlText += `)`
	}
	rows, err := m.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []SearchResult
	for rows.Next() {
		var result SearchResult
		var blob []byte
		if err := rows.Scan(&result.ContentHash, &result.Content, &result.Date, &result.EventTime, &result.Status, &result.ExpiresAt, &result.ValidFrom, &result.ValidUntil, &result.ScopeType, &result.ScopeID, &blob); err != nil {
			return nil, err
		}
		embedding, err := decodeLocal(blob)
		if err != nil {
			return nil, err
		}
		result.Source = "vector"
		result.Score = float64(vectorstore.CosineSimilarity(vector, embedding))
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	filter := &HybridSearcher{}
	results = filter.filterByLifecycleAndScope(results, q)
	if q.AsOf != "" || q.DateFrom != "" || q.DateTo != "" {
		results = filter.filterByTime(results, q)
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}
