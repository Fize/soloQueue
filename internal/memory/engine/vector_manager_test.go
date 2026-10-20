package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/memory/engine/embedding"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine/embedding/local"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine/embedding/managed"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine/vectorstore"
)

// These controlled workers test orchestration only. Real model evidence is in
// TestManagedNativeOffline and the independently executed native platform gate.
type managedTestWorker struct {
	embed  func(context.Context, []string, string) (local.Batch, error)
	closed atomic.Bool
}

func (w *managedTestWorker) Embed(ctx context.Context, texts []string, purpose string) (local.Batch, error) {
	if w.closed.Load() {
		return local.Batch{}, errors.New("closed")
	}
	if w.embed != nil {
		return w.embed(ctx, texts, purpose)
	}
	return testLocalBatch(texts), nil
}
func (w *managedTestWorker) Status() local.Status {
	return local.Status{Model: "intfloat/multilingual-e5-small", Dimension: 384, Backend: "cpu"}
}
func (w *managedTestWorker) Close() error { w.closed.Store(true); return nil }
func testLocalBatch(texts []string) local.Batch {
	results := make([]local.Result, len(texts))
	for i := range results {
		v := make([]float32, 384)
		v[0] = 1
		results[i] = local.Result{Embedding: v, Tokens: 3}
	}
	return local.Batch{Backend: "cpu", Results: results}
}
func testVectorOptions(start func(context.Context, local.Config) (LocalWorker, error)) VectorOptions {
	return VectorOptions{Prepare: func(context.Context, managed.Progress) (managed.Paths, error) { return managed.Paths{}, nil }, Start: start}
}
func localVectorConfig() VectorConfig {
	return VectorConfig{Provider: "onnx", Enabled: true, Model: "multilingual-e5-small", Device: "auto"}
}
func waitVectorStatus(t *testing.T, m *VectorManager, predicate func(EmbeddingStatus) bool) EmbeddingStatus {
	t.Helper()
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		status := m.Status()
		if predicate(status) {
			return status
		}
		select {
		case <-deadline.C:
			t.Fatalf("timed out waiting for local status: %+v", status)
		case <-ticker.C:
		}
	}
}
func TestManagedFailureRetryCancelAndGeneration(t *testing.T) {
	db, mu := openTestDB(t)
	var prepares atomic.Int32
	failQuery := atomic.Bool{}
	failQuery.Store(true)
	options := testVectorOptions(func(context.Context, local.Config) (LocalWorker, error) {
		return &managedTestWorker{embed: func(ctx context.Context, texts []string, purpose string) (local.Batch, error) {
			if purpose == "query" && failQuery.Load() {
				return local.Batch{}, errors.New("native failed")
			}
			return testLocalBatch(texts), nil
		}}, nil
	})
	options.Prepare = func(context.Context, managed.Progress) (managed.Paths, error) {
		if prepares.Add(1) == 1 {
			return managed.Paths{}, errors.New("download failed")
		}
		return managed.Paths{}, nil
	}
	m := NewVectorManager(db, mu, t.TempDir(), options)
	defer m.Close()
	m.Apply(localVectorConfig(), nil, nil)
	waitVectorStatus(t, m, func(s EmbeddingStatus) bool { return s.State == "error" })
	if _, err := m.Action("retry"); err != nil {
		t.Fatal(err)
	}
	waitVectorStatus(t, m, func(s EmbeddingStatus) bool { return s.State == "ready" })
	if results, err := m.Search(context.Background(), SearchQuery{Text: "query", OwnerType: OwnerL1}, 5); err != nil || len(results) != 0 {
		t.Fatal(results, err)
	}
	waitVectorStatus(t, m, func(s EmbeddingStatus) bool { return s.State == "error" })
	m.Notify()
	time.Sleep(30 * time.Millisecond)
	if m.Status().State != "error" {
		t.Fatal("failed query reverted to ready")
	}
	failQuery.Store(false)
	if _, err := m.Action("retry"); err != nil {
		t.Fatal(err)
	}
	waitVectorStatus(t, m, func(s EmbeddingStatus) bool { return s.State == "ready" })
	count := prepares.Load()
	m.Apply(localVectorConfig(), nil, nil)
	if prepares.Load() != count {
		t.Fatal("unchanged configuration restarted worker")
	}
	if _, err := m.Action("cancel"); err != nil {
		t.Fatal(err)
	}
	m.Notify()
	if m.Status().State != "cancelled" {
		t.Fatal(m.Status())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Search(ctx, SearchQuery{}, 5); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestManagedOldGenerationCannotPublish(t *testing.T) {
	db, mu := openTestDB(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls, starts atomic.Int32
	opts := testVectorOptions(func(context.Context, local.Config) (LocalWorker, error) {
		starts.Add(1)
		return &managedTestWorker{}, nil
	})
	opts.Prepare = func(context.Context, managed.Progress) (managed.Paths, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return managed.Paths{}, nil
	}
	m := NewVectorManager(db, mu, t.TempDir(), opts)
	defer m.Close()
	m.Apply(localVectorConfig(), nil, nil)
	<-entered
	m.Apply(VectorConfig{Provider: "none"}, nil, nil)
	m.Apply(localVectorConfig(), nil, nil)
	close(release)
	waitVectorStatus(t, m, func(s EmbeddingStatus) bool { return s.State == "ready" })
	if starts.Load() != 1 {
		t.Fatalf("obsolete generation started worker: %d", starts.Load())
	}
}
func TestManagedConcurrentCatchupDeletionAndNamespace(t *testing.T) {
	db, mu := openTestDB(t)
	// Match production's WAL mode for concurrent readers and writers.
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000`); err != nil {
		t.Fatal(err)
	}
	e := New(db, mu, nil, nil, nil)
	ctx := context.Background()
	deleted, _, err := e.Save(ctx, "deleted during inference", "2026-01-01", "", "2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	m := NewVectorManager(db, mu, t.TempDir(), testVectorOptions(func(context.Context, local.Config) (LocalWorker, error) {
		return &managedTestWorker{embed: func(ctx context.Context, texts []string, purpose string) (local.Batch, error) {
			if purpose == "document" {
				once.Do(func() {
					close(entered)
					select {
					case <-release:
					case <-ctx.Done():
					}
				})
			}
			return testLocalBatch(texts), ctx.Err()
		}}, nil
	}))
	defer m.Close()
	e.SetVectorManager(m)
	m.Apply(localVectorConfig(), nil, nil)
	<-entered
	hash, _, err := e.Save(ctx, "written while old batch is running", "2026-01-02", "", "2026-01-02")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.Delete(ctx, deleted); err != nil {
		t.Fatal(err)
	}
	close(release)
	waitVectorStatus(t, m, func(s EmbeddingStatus) bool { return s.State == "ready" && s.Indexed == 1 && s.Pending == 0 })
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM mem_vec_local WHERE content_hash=?`, deleted).Scan(&count); err != nil || count != 0 {
		t.Fatal("resurrected deleted memory", count, err)
	}
	if _, err := db.Exec(`INSERT INTO mem_vec_local VALUES('another-space','l1','',?,?,?)`, hash, "written while old batch is running", encodeLocal(testLocalBatch([]string{"x"}).Results[0].Embedding)); err != nil {
		t.Fatal(err)
	}
	results, err := m.Search(ctx, SearchQuery{Text: "query: literal", OwnerType: OwnerL1, ScopeType: ScopeGlobal}, 10)
	if err != nil || len(results) != 1 || results[0].ContentHash != hash {
		t.Fatal(results, err, m.Status())
	}
	if _, err := m.Action("reindex"); err != nil {
		t.Fatal(err)
	}
	waitVectorStatus(t, m, func(s EmbeddingStatus) bool { return s.State == "ready" && s.Indexed == 1 })
	if err := db.QueryRow(`SELECT COUNT(*) FROM mem_vec_local WHERE space_id='another-space'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("reindex removed another namespace", count, err)
	}
}
func TestManagedScopeLifecycleAndDatabaseErrors(t *testing.T) {
	db, mu := openTestDB(t)
	e := New(db, mu, nil, nil, nil)
	ctx := context.Background()
	hash, _, err := e.Save(ctx, "historical memory", "2026-01-01", "", "2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE mem_entries SET status='superseded',valid_from='2026-01-01',valid_until='2026-02-01',scope_type='project',scope_id='allowed' WHERE content_hash=?`, hash); err != nil {
		t.Fatal(err)
	}
	// Invalid owners precede valid entries; they must not occupy the first batch forever.
	for i := 0; i < 20; i++ {
		content := fmt.Sprintf("invalid-owner-%d", i)
		h := hashContent(content)
		if _, err = db.Exec(`INSERT INTO mem_entries(id,content,content_hash,date,event_time,owner_type,owner_id,created_at) VALUES(?,?,?,'2026-01-01','2026-01-01','l2_group','invalid','2000-01-01')`, h[:16], content, h); err != nil {
			t.Fatal(err)
		}
	}
	m := NewVectorManager(db, mu, t.TempDir(), testVectorOptions(func(context.Context, local.Config) (LocalWorker, error) { return &managedTestWorker{}, nil }))
	defer m.Close()
	m.Apply(localVectorConfig(), nil, nil)
	waitVectorStatus(t, m, func(s EmbeddingStatus) bool { return s.State == "ready" && s.Indexed == 1 && s.Pending == 0 })
	query := SearchQuery{Text: "memory", OwnerType: OwnerL1, ScopeType: ScopeProject, ScopeID: "allowed", AsOf: "2026-01-20"}
	results, err := m.Search(ctx, query, 5)
	if err != nil || len(results) != 1 {
		t.Fatal(results, err)
	}
	query.ScopeID = "denied"
	results, err = m.Search(ctx, query, 5)
	if err != nil || len(results) != 0 {
		t.Fatal("scope leak", results, err)
	}
	query.ScopeID = "allowed"
	query.AsOf = ""
	results, err = m.Search(ctx, query, 5)
	if err != nil || len(results) != 0 {
		t.Fatal("superseded leak", results, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Search(ctx, query, 5); err == nil {
		t.Fatal("database error swallowed as local unavailability")
	}
}

type managedRemoteRecorder struct {
	mu    sync.Mutex
	texts []string
}

func (r *managedRemoteRecorder) Dimension() int { return 384 }
func (r *managedRemoteRecorder) Embed(ctx context.Context, texts []string) ([]embedding.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.texts = append(r.texts, texts...)
	results := make([]embedding.Result, len(texts))
	for i := range results {
		results[i] = embedding.Result{Embedding: testLocalBatch([]string{"x"}).Results[0].Embedding}
	}
	return results, nil
}
func TestManagedRemoteLegacyActivationAndPrefix(t *testing.T) {
	db, mu := openTestDB(t)
	e := New(db, mu, nil, nil, nil)
	hash, _, _ := e.Save(context.Background(), "legacy content", "2026-01-01", "", "2026-01-01")
	remote := &managedRemoteRecorder{}
	legacy := vectorstore.NewSQLiteStoreFromDB(db, mu, vectorstore.WithTableName("mem_vec"))
	if err := legacy.Upsert(context.Background(), vectorstore.MemoryEntry{ID: hash[:16], Content: "legacy content", Source: "memoryengine", Embedding: testLocalBatch([]string{"x"}).Results[0].Embedding, OwnerType: OwnerL1, ScopeType: ScopeGlobal}); err != nil {
		t.Fatal(err)
	}
	m := NewVectorManager(db, mu, t.TempDir(), VectorOptions{})
	defer m.Close()
	m.Apply(VectorConfig{Provider: "openai", Enabled: false}, remote, legacy)
	results, err := m.Search(context.Background(), SearchQuery{Text: "needle", OwnerType: OwnerL1}, 5)
	if err != nil || len(results) != 1 || results[0].ContentHash != hash {
		t.Fatal(results, err)
	}
	m.IndexSaved(context.Background(), hash[:16], hash, "raw document", OwnerL1, "", ScopeGlobal, "")
	if len(remote.texts) != 2 || remote.texts[0] != "query: needle" || remote.texts[1] != "raw document" {
		t.Fatal(remote.texts)
	}
	if m.Status().State != "ready" {
		t.Fatal("legacy activation now incorrectly depends on enabled flag")
	}
}

type offlineTransport struct{}

func (offlineTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("offline integration forbids downloads")
}
func linkOrCopy(t *testing.T, source, target string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if os.Link(source, target) == nil {
		return
	}
	in, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatal(copyErr, closeErr)
	}
}
func TestManagedNativeOffline(t *testing.T) {
	if os.Getenv("SOLOQUEUE_ONNX_INTEGRATION") != "1" {
		t.Skip("explicit real model integration: set SOLOQUEUE_ONNX_INTEGRATION=1 and SOLOQUEUE_ONNX_ARTIFACTS")
	}
	artifacts := os.Getenv("SOLOQUEUE_ONNX_ARTIFACTS")
	if artifacts == "" {
		t.Fatal("SOLOQUEUE_ONNX_ARTIFACTS required")
	}
	workDir := t.TempDir()
	modelCache := filepath.Join(workDir, "models", "embedding", "multilingual-e5-small", managed.Revision, "onnx")
	for _, name := range []string{"model.onnx", "tokenizer.json"} {
		linkOrCopy(t, filepath.Join(artifacts, "model", "onnx", name), filepath.Join(modelCache, name))
	}
	archives, err := filepath.Glob(filepath.Join(artifacts, "runtime", "archives", "*"))
	if err != nil || len(archives) == 0 {
		t.Fatal("verified runtime archives required", err)
	}
	cache := filepath.Join(workDir, "runtimes", "embedding", managed.RuntimeVersion, runtime.GOOS+"-"+runtime.GOARCH, "archives")
	for _, source := range archives {
		linkOrCopy(t, source, filepath.Join(cache, filepath.Base(source)))
	}
	db, mu := openTestDB(t)
	e := New(db, mu, nil, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	matching, _, err := e.Save(ctx, "南瓜可以切块蒸熟后压成泥，加入牛奶煮成南瓜汤。", "2026-01-01", "", "2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = e.Save(ctx, "The database server listens on port 5432 and stores customer records.", "2026-01-01", "", "2026-01-01"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE mem_vec(sentinel TEXT);INSERT INTO mem_vec VALUES('preserve legacy vectors')`); err != nil {
		t.Fatal(err)
	}
	installer := managed.Installer{WorkDir: workDir, Client: &http.Client{Transport: offlineTransport{}}}
	options := VectorOptions{Prepare: installer.Prepare}
	m := NewVectorManager(db, mu, workDir, options)
	e.SetVectorManager(m)
	cfg := localVectorConfig()
	cfg.Device = "cpu"
	m.Apply(cfg, nil, nil)
	deadline := time.NewTimer(2 * time.Minute)
	defer deadline.Stop()
	for m.Status().State != "ready" {
		status := m.Status()
		if status.State == "error" {
			m.Close()
			t.Fatal(status)
		}
		select {
		case <-deadline.C:
			m.Close()
			t.Fatal("native manager not ready", status)
		case <-time.After(20 * time.Millisecond):
		}
	}
	results, err := m.Search(ctx, SearchQuery{Text: "How can I cook pumpkin soup?", OwnerType: OwnerL1, ScopeType: ScopeGlobal}, 2)
	if err != nil || len(results) != 2 || results[0].ContentHash != matching {
		m.Close()
		t.Fatal("cross-language retrieval mismatch", results, err)
	}
	if _, _, err = e.Save(ctx, "新增记忆：南瓜汤也可以加入少量奶油。", "2026-01-02", "", "2026-01-02"); err != nil {
		m.Close()
		t.Fatal(err)
	}
	waitVectorStatus(t, m, func(s EmbeddingStatus) bool { return s.State == "ready" && s.Indexed == 3 })
	m.Close()
	restarted := NewVectorManager(db, mu, workDir, options)
	defer restarted.Close()
	restarted.Apply(cfg, nil, nil)
	deadline2 := time.NewTimer(2 * time.Minute)
	defer deadline2.Stop()
	for restarted.Status().State != "ready" {
		status := restarted.Status()
		if status.State == "error" {
			t.Fatal(status)
		}
		select {
		case <-deadline2.C:
			t.Fatal("offline restart not ready", status)
		case <-time.After(20 * time.Millisecond):
		}
	}
	if restarted.Status().Indexed != 3 || restarted.Status().Pending != 0 {
		t.Fatal(restarted.Status())
	}
	var sentinel string
	if err = db.QueryRow(`SELECT sentinel FROM mem_vec`).Scan(&sentinel); err != nil || !strings.Contains(sentinel, "preserve") {
		t.Fatal("legacy vectors changed", sentinel, err)
	}
	t.Logf("real native offline manager PASS backend=%s dimension=%d indexed=%d", restarted.Status().Backend, restarted.Status().Dimension, restarted.Status().Indexed)
}

func TestManagedRemoteProfileIsolationPersists(t *testing.T) {
	db, mu := openTestDB(t)
	e := New(db, mu, nil, nil, nil)
	ctx := context.Background()
	hash, _, _ := e.Save(ctx, "original remote memory", "2026-01-01", "", "2026-01-01")
	legacy := vectorstore.NewSQLiteStoreFromDB(db, mu, vectorstore.WithTableName("mem_vec"))
	if err := legacy.Upsert(ctx, vectorstore.MemoryEntry{ID: hash[:16], Source: hash, Content: "original remote memory", OwnerType: OwnerL1, ScopeType: ScopeGlobal, Embedding: testLocalBatch([]string{"x"}).Results[0].Embedding}); err != nil {
		t.Fatal(err)
	}
	remote := &managedRemoteRecorder{}
	m := NewVectorManager(db, mu, t.TempDir(), VectorOptions{})
	m.Apply(VectorConfig{Provider: "openai", RemoteKey: "original-model-same-dimension"}, remote, legacy)
	query := SearchQuery{Text: "remote", OwnerType: OwnerL1}
	results, err := m.Search(ctx, query, 5)
	if err != nil || len(results) != 1 {
		t.Fatal(results, err)
	}
	m.Apply(VectorConfig{Provider: "openai", RemoteKey: "different-model-same-dimension"}, remote, legacy)
	results, err = m.Search(ctx, query, 5)
	if err != nil || len(results) != 0 {
		t.Fatal("new model read legacy vectors", results, err)
	}
	// No background remote embedding cost on model/profile switch.
	if len(remote.texts) != 2 {
		t.Fatal("remote automatically reindexed", remote.texts)
	}
	m.IndexSaved(ctx, hash[:16], hash, "original remote memory", OwnerL1, "", ScopeGlobal, "")
	results, err = m.Search(ctx, query, 5)
	if err != nil || len(results) != 1 {
		t.Fatal(results, err)
	}
	m.Close()
	restarted := NewVectorManager(db, mu, t.TempDir(), VectorOptions{})
	defer restarted.Close()
	restarted.Apply(VectorConfig{Provider: "onnx", Enabled: false, RemoteKey: "original-model-same-dimension"}, nil, nil)
	restarted.Apply(VectorConfig{Provider: "openai", RemoteKey: "original-model-same-dimension"}, remote, legacy)
	results, err = restarted.Search(ctx, query, 5)
	if err != nil || len(results) != 1 {
		t.Fatal("original legacy profile not restored", results, err)
	}
	restarted.Apply(VectorConfig{Provider: "openai", RemoteKey: "different-model-same-dimension"}, remote, legacy)
	results, err = restarted.Search(ctx, query, 5)
	if err != nil || len(results) != 1 {
		t.Fatal("new isolated profile not restored", results, err)
	}
}

func TestManagedInvalidScopesExcludedAndPendingAccurateOnFailure(t *testing.T) {
	db, mu := openTestDB(t)
	e := New(db, mu, nil, nil, nil)
	ctx := context.Background()
	hash, _, err := e.Save(ctx, "valid pending memory", "2026-01-01", "", "2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	for i, scope := range []struct{ kind, id string }{{"unknown", ""}, {ScopeGlobal, "unexpected"}, {ScopeProject, ""}} {
		content := fmt.Sprintf("invalid scope %d", i)
		h := hashContent(content)
		if _, err = db.Exec(`INSERT INTO mem_entries(id,content,content_hash,date,event_time,scope_type,scope_id) VALUES(?,?,?,'2026-01-01','2026-01-01',?,?)`, h[:16], content, h, scope.kind, scope.id); err != nil {
			t.Fatal(err)
		}
	}
	fail := atomic.Bool{}
	fail.Store(true)
	options := testVectorOptions(func(context.Context, local.Config) (LocalWorker, error) {
		return &managedTestWorker{embed: func(ctx context.Context, texts []string, purpose string) (local.Batch, error) {
			if purpose == "document" && fail.Load() {
				return local.Batch{}, errors.New("index failure")
			}
			return testLocalBatch(texts), nil
		}}, nil
	})
	m := NewVectorManager(db, mu, t.TempDir(), options)
	defer m.Close()
	m.Apply(localVectorConfig(), nil, nil)
	status := waitVectorStatus(t, m, func(s EmbeddingStatus) bool { return s.State == "error" })
	if status.Pending != 1 || status.Failed != 1 {
		t.Fatal("pending counts lost on inference failure", status)
	}
	fail.Store(false)
	if _, err = m.Action("retry"); err != nil {
		t.Fatal(err)
	}
	waitVectorStatus(t, m, func(s EmbeddingStatus) bool { return s.State == "ready" && s.Indexed == 1 && s.Pending == 0 })
	results, err := m.Search(ctx, SearchQuery{Text: "memory", OwnerType: OwnerL1}, 10)
	if err != nil || len(results) != 1 || results[0].ContentHash != hash {
		t.Fatal("scope-less query included invalid scope", results, err)
	}
}

func TestManagedBackgroundDatabaseFailurePropagates(t *testing.T) {
	db, mu := openTestDB(t)
	db.SetMaxOpenConns(1)
	e := New(db, mu, nil, nil, nil)
	if _, _, err := e.Save(context.Background(), "authoritative memory", "2026-01-01", "", "2026-01-01"); err != nil {
		t.Fatal(err)
	}
	m := NewVectorManager(db, mu, t.TempDir(), testVectorOptions(func(context.Context, local.Config) (LocalWorker, error) { return &managedTestWorker{}, nil }))
	defer m.Close()
	m.Apply(localVectorConfig(), nil, nil)
	waitVectorStatus(t, m, func(s EmbeddingStatus) bool { return s.State == "ready" })
	mu.Lock()
	_, err := db.Exec(`DROP TABLE mem_vec_local`)
	mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	m.Notify()
	waitVectorStatus(t, m, func(s EmbeddingStatus) bool { return s.State == "error" })
	if _, err := m.Search(context.Background(), SearchQuery{Text: "memory", OwnerType: OwnerL1}, 5); err == nil {
		t.Fatal("background SQLite corruption was hidden as unavailable local inference")
	}
}

func TestManagedRemoteActivationRefreshKeepsNamespace(t *testing.T) {
	db, mu := openTestDB(t)
	e := New(db, mu, nil, nil, nil)
	ctx := context.Background()
	hash, _, err := e.Save(ctx, "remote activation memory", "2026-01-01", "", "2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	legacy := vectorstore.NewSQLiteStoreFromDB(db, mu, vectorstore.WithTableName("mem_vec"))
	if err := legacy.Upsert(ctx, vectorstore.MemoryEntry{ID: hash[:16], Source: hash, Content: "remote activation memory", OwnerType: OwnerL1, ScopeType: ScopeGlobal, Embedding: testLocalBatch([]string{"x"}).Results[0].Embedding}); err != nil {
		t.Fatal(err)
	}
	remote := &managedRemoteRecorder{}
	m := NewVectorManager(db, mu, t.TempDir(), VectorOptions{})
	defer m.Close()
	cfg := VectorConfig{Provider: "openai", RemoteKey: "unchanged-model-provider-identity", Enabled: false}
	query := SearchQuery{Text: "activation", OwnerType: OwnerL1}
	m.Apply(cfg, remote, legacy)
	if results, err := m.Search(ctx, query, 5); err != nil || len(results) != 1 {
		t.Fatal(results, err)
	}
	for _, activation := range []string{"model", "provider"} {
		t.Run(activation, func(t *testing.T) {
			before := len(remote.texts)
			// Runtime supplies no client/store when either model or provider is disabled.
			m.Apply(cfg, nil, nil)
			if m.Enabled() || m.Status().State != "disabled" {
				t.Fatal("disabled remote remains active", m.Status())
			}
			if results, err := m.Search(ctx, query, 5); err != nil || len(results) != 0 {
				t.Fatal(results, err)
			}
			m.IndexSaved(ctx, hash[:16], hash, "remote activation memory", OwnerL1, "", ScopeGlobal, "")
			if len(remote.texts) != before {
				t.Fatal("disabled remote made paid embedding calls")
			}
			m.Apply(cfg, remote, legacy)
			if !m.Enabled() || m.Status().State != "ready" {
				t.Fatal("re-enabled remote remains unavailable", m.Status())
			}
			if results, err := m.Search(ctx, query, 5); err != nil || len(results) != 1 || results[0].ContentHash != hash {
				t.Fatal("re-enable lost namespace", results, err)
			}
			m.IndexSaved(ctx, hash[:16], hash, "remote activation memory", OwnerL1, "", ScopeGlobal, "")
			if len(remote.texts) != before+2 {
				t.Fatal("re-enabled queries/writes unavailable", remote.texts)
			}
		})
	}
}

// Run the real transport against a controlled NDJSON child. This exercises its
// queue/cancellation behavior, not native model accuracy.
func init() {
	if dir := os.Getenv("SOLOQUEUE_MANAGER_WORKER_TEST"); dir != "" {
		emit := func(id uint64, data any) {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"version": 1, "id": id, "data": data, "error": nil})
		}
		emit(0, local.Status{Model: "intfloat/multilingual-e5-small", Dimension: 384, Backend: "cpu"})
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		for scanner.Scan() {
			var request struct {
				ID      uint64
				Texts   []string
				Purpose string
			}
			if json.Unmarshal(scanner.Bytes(), &request) != nil {
				os.Exit(2)
			}
			if request.Purpose == os.Getenv("SOLOQUEUE_MANAGER_BLOCK_PURPOSE") {
				if os.WriteFile(filepath.Join(dir, "entered"), nil, 0600) != nil {
					os.Exit(2)
				}
				for {
					if _, err := os.Stat(filepath.Join(dir, "release")); err == nil {
						break
					}
					time.Sleep(time.Millisecond)
				}
			}
			emit(request.ID, testLocalBatch(request.Texts))
		}
		os.Exit(0)
	}
}

type observedQueueContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (c *observedQueueContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}

func TestManagedRequestErrorsPreserveIndexingWorker(t *testing.T) {
	for _, kind := range []string{"queued_cancel", "oversize", "invalid_utf8", "active_cancel"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("SOLOQUEUE_MANAGER_WORKER_TEST", dir)
			purpose := "document"
			if kind == "active_cancel" {
				purpose = "query"
			}
			t.Setenv("SOLOQUEUE_MANAGER_BLOCK_PURPOSE", purpose)
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			db, mu := openTestDB(t)
			e := New(db, mu, nil, nil, nil)
			if _, _, err := e.Save(context.Background(), "indexed memory", "2026-01-01", "", "2026-01-01"); err != nil {
				t.Fatal(err)
			}
			opts := testVectorOptions(func(ctx context.Context, _ local.Config) (LocalWorker, error) {
				return local.Start(ctx, local.Config{Worker: exe, ModelDir: dir, Runtime: exe, Device: "cpu"})
			})
			m := NewVectorManager(db, mu, dir, opts)
			defer m.Close()
			m.Apply(localVectorConfig(), nil, nil)
			waitEntered := func() {
				t.Helper()
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					if _, err := os.Stat(filepath.Join(dir, "entered")); err == nil {
						return
					}
					time.Sleep(time.Millisecond)
				}
				t.Fatal("worker request did not start")
			}
			if kind == "active_cancel" {
				waitVectorStatus(t, m, func(s EmbeddingStatus) bool { return s.State == "ready" })
			} else {
				waitEntered()
			}
			query := SearchQuery{Text: "memory", OwnerType: OwnerL1, ScopeType: ScopeGlobal}
			if kind == "queued_cancel" || kind == "active_cancel" {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				observed := &observedQueueContext{Context: ctx, entered: make(chan struct{})}
				result := make(chan error, 1)
				go func() { _, err := m.Search(observed, query, 5); result <- err }()
				if kind == "active_cancel" {
					waitEntered()
				} else {
					select {
					case <-observed.entered:
					case <-time.After(5 * time.Second):
						t.Fatal("query did not enter transport queue")
					}
				}
				cancel()
				select {
				case err := <-result:
					if !errors.Is(err, context.Canceled) {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("query cancellation blocked")
				}
			} else {
				if kind == "oversize" {
					query.Text = strings.Repeat("x", 65537)
				} else {
					query.Text = string([]byte{0xff})
				}
				if _, err := m.Search(context.Background(), query, 5); err == nil {
					t.Fatal("invalid query was not rejected")
				}
			}
			if kind == "active_cancel" {
				waitVectorStatus(t, m, func(s EmbeddingStatus) bool { return s.State == "error" })
				m.Notify()
				if m.Status().State != "error" {
					t.Fatal("dead worker remained available")
				}
				return
			}
			if status := m.Status(); status.State != "reindexing" || status.Failed != 0 {
				t.Fatalf("request-local failure stopped indexing: %+v", status)
			}
			if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			waitVectorStatus(t, m, func(s EmbeddingStatus) bool { return s.State == "ready" && s.Indexed == 1 })
			query.Text = "memory"
			results, err := m.Search(context.Background(), query, 5)
			if err != nil || len(results) != 1 {
				t.Fatalf("healthy query after request error: %v %v", results, err)
			}
		})
	}
}
