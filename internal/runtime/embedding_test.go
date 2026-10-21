package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/infra/db"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine/embedding/local"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine/embedding/managed"
)

type runtimeTestLocalWorker struct{ calls, closed atomic.Int32 }

func (w *runtimeTestLocalWorker) Embed(_ context.Context, texts []string, _ string) (local.Batch, error) {
	w.calls.Add(1)
	results := make([]local.Result, len(texts))
	for i := range results {
		vector := make([]float32, 384)
		vector[0] = 1
		results[i] = local.Result{Embedding: vector, Tokens: 3}
	}
	return local.Batch{Backend: "cpu", Results: results}, nil
}
func (*runtimeTestLocalWorker) Status() local.Status {
	return local.Status{Model: "intfloat/multilingual-e5-small", Dimension: 384, Backend: "cpu"}
}
func (w *runtimeTestLocalWorker) Close() error { w.closed.Add(1); return nil }
func TestEmbeddingHotReloadKeepsStableEngineAndWorker(t *testing.T) {
	dir := t.TempDir()
	shared, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Close()
	log, err := logger.System(dir, logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	var starts atomic.Int32
	manager := engine.NewVectorManager(shared.DB, &shared.WMu, dir, engine.VectorOptions{Prepare: func(context.Context, managed.Progress) (managed.Paths, error) { return managed.Paths{}, nil }, Start: func(context.Context, local.Config) (engine.LocalWorker, error) {
		starts.Add(1)
		return &runtimeTestLocalWorker{}, nil
	}})
	defer manager.Close()
	memory := engine.New(shared.DB, &shared.WMu, nil, nil, log)
	memory.SetVectorManager(manager)
	stack := &Stack{SharedDB: shared, Log: log, Embedding: manager, MemoryEngine: memory, workDir: dir}
	stack.ToolsCfg.MemoryEngine = memory
	settings := config.DefaultSettings()
	settings.Embedding.Provider = "onnx"
	settings.Embedding.Enabled = true
	if err = stack.OnSettingsChange(settings); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for manager.Status().State != "ready" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if manager.Status().State != "ready" {
		t.Fatal(manager.Status())
	}
	settings.Session.TimelineMaxFileMB++
	if err = stack.OnSettingsChange(settings); err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 1 || stack.MemoryEngine != memory || stack.ToolsCfg.MemoryEngine != memory {
		t.Fatal("unrelated settings restarted local model or engine", starts.Load())
	}
	settings.Embedding.Local.Device = "invalid"
	if err = stack.OnSettingsChange(settings); err == nil {
		t.Fatal("invalid file reload accepted")
	}
}
func TestRemoteEmbeddingIdentityExcludesActivationAndLocalDevice(t *testing.T) {
	cfg := config.DefaultSettings().Embedding
	original := vectorConfiguration(cfg).RemoteKey
	if original == "" {
		t.Fatal("missing default remote profile identity")
	}
	cfg.Enabled = !cfg.Enabled
	cfg.Local.Device = "cpu"
	cfg.Provider = "onnx"
	if vectorConfiguration(cfg).RemoteKey != original {
		t.Fatal("activation/device changed remote vector space")
	}
	cfg.Models[0].ID = "different-model-same-dim"
	if vectorConfiguration(cfg).RemoteKey == original {
		t.Fatal("remote model change reused vector space")
	}
}

func TestProductionEmbeddingDeviceAlwaysAutomatic(t *testing.T) {
	cfg := config.DefaultSettings().Embedding
	cfg.Provider = "onnx"
	cfg.Local.Device = "cpu"
	if vectorConfiguration(cfg).Device != "auto" {
		t.Fatal("saved compatibility device bypassed automatic selection")
	}
}

func TestRemoteModelAndProviderActivationHotReload(t *testing.T) {
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		vector := make([]float32, 384)
		vector[0] = 1
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"index": 0, "embedding": vector}}})
	}))
	defer upstream.Close()
	dir := t.TempDir()
	shared, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Close()
	log, err := logger.System(dir, logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	settings := config.DefaultSettings()
	settings.Embedding.Provider = "openai"
	settings.Embedding.Enabled = true
	settings.Embedding.Providers[0].Enabled = true
	settings.Embedding.Providers[0].BaseURL = upstream.URL
	settings.Embedding.Models[0].Enabled = true
	settings.Embedding.Models[0].Dimension = 384
	stack := &Stack{SharedDB: shared, Log: log, workDir: dir}
	stack.rebuildMemoryEngine(settings)
	defer stack.Embedding.Close()
	manager := stack.Embedding
	hash, _, err := stack.MemoryEngine.Save(context.Background(), "remote hot reload memory", "2026-01-01", "", "2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	query := engine.SearchQuery{Text: "memory", OwnerType: engine.OwnerL1}
	identity := vectorConfiguration(settings.Embedding)
	for _, toggle := range []string{"model", "provider"} {
		t.Run(toggle, func(t *testing.T) {
			if toggle == "model" {
				settings.Embedding.Models[0].Enabled = false
			} else {
				settings.Embedding.Providers[0].Enabled = false
			}
			if vectorConfiguration(settings.Embedding) != identity {
				t.Fatal("activation changed vector identity")
			}
			stack.rebuildMemoryEngine(settings)
			before := requests.Load()
			if manager.Enabled() {
				t.Fatal("disabled remote still enabled")
			}
			results, err := manager.Search(context.Background(), query, 5)
			if err != nil || len(results) != 0 {
				t.Fatal(results, err)
			}
			manager.IndexSaved(context.Background(), hash[:16], hash, "remote hot reload memory", engine.OwnerL1, "", engine.ScopeGlobal, "")
			if requests.Load() != before {
				t.Fatal("disabled remote made embedding requests")
			}
			if toggle == "model" {
				settings.Embedding.Models[0].Enabled = true
			} else {
				settings.Embedding.Providers[0].Enabled = true
			}
			stack.rebuildMemoryEngine(settings)
			if !manager.Enabled() || manager.Status().State != "ready" {
				t.Fatal("re-enabled remote unavailable", manager.Status())
			}
			results, err = manager.Search(context.Background(), query, 5)
			if err != nil || len(results) != 1 || results[0].ContentHash != hash {
				t.Fatal("same namespace not restored", results, err)
			}
			manager.IndexSaved(context.Background(), hash[:16], hash, "remote hot reload memory", engine.OwnerL1, "", engine.ScopeGlobal, "")
			if requests.Load() != before+2 {
				t.Fatal("re-enabled query/write not restored", requests.Load(), before)
			}
			if stack.Embedding != manager {
				t.Fatal("hot reload replaced stable manager")
			}
		})
	}
}

func TestMemoryMasterOffForEveryProviderAndStableHotReload(t *testing.T) {
	for _, provider := range []string{"none", "onnx", "openai"} {
		t.Run(provider, func(t *testing.T) {
			var requests, starts atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				vector := make([]float32, 384)
				vector[0] = 1
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"index": 0, "embedding": vector}}})
			}))
			defer upstream.Close()
			dir := t.TempDir()
			shared, err := db.Open(filepath.Join(dir, "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer shared.Close()
			log, err := logger.System(dir, logger.WithConsole(false), logger.WithFile(false))
			if err != nil {
				t.Fatal(err)
			}
			defer log.Close()
			worker := &runtimeTestLocalWorker{}
			manager := engine.NewVectorManager(shared.DB, &shared.WMu, dir, engine.VectorOptions{
				Prepare: func(context.Context, managed.Progress) (managed.Paths, error) { return managed.Paths{}, nil },
				Start:   func(context.Context, local.Config) (engine.LocalWorker, error) { starts.Add(1); return worker, nil },
			})
			defer manager.Close()
			memory := engine.New(shared.DB, &shared.WMu, nil, nil, log)
			memory.SetVectorManager(manager)
			stack := &Stack{SharedDB: shared, Log: log, Embedding: manager, MemoryEngine: memory, workDir: dir}
			stack.ToolsCfg.MemoryEngine = memory
			access := memory.BindL1(engine.ScopeGlobal, "", false)
			settings := config.DefaultSettings()
			settings.Embedding.Provider = provider
			settings.Embedding.Providers[0].Enabled = true
			settings.Embedding.Providers[0].BaseURL = upstream.URL
			settings.Embedding.Models[0].Enabled = true
			settings.Embedding.Models[0].Dimension = 384
			stack.rebuildMemoryEngine(settings)
			if memory.Enabled() || manager.Enabled() || manager.Status().State != "disabled" || starts.Load() != 0 || requests.Load() != 0 {
				t.Fatal("default off activated memory")
			}
			if _, _, err := memory.Save(context.Background(), "disabled", "", "", ""); !errors.Is(err, engine.ErrMemoryDisabled) {
				t.Fatal(err)
			}
			settings.Embedding.Enabled = true
			stack.rebuildMemoryEngine(settings)
			waitReady := func() {
				t.Helper()
				deadline := time.Now().Add(3 * time.Second)
				for provider == "onnx" && manager.Status().State != "ready" && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				if provider == "onnx" && manager.Status().State != "ready" {
					t.Fatal(manager.Status())
				}
			}
			waitReady()
			candidate := engine.MemoryCandidate{Content: "Retained durable SQLite fact", MemoryType: engine.MemoryTypeStableFact, SourceType: engine.SourceExplicit}
			saved, err := access.Ingest(context.Background(), candidate)
			if err != nil {
				t.Fatal(err)
			}
			if provider == "onnx" {
				deadline := time.Now().Add(3 * time.Second)
				for manager.Status().Indexed != 1 && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				if manager.Status().Indexed != 1 {
					t.Fatal(manager.Status())
				}
			}
			found, err := access.Search(context.Background(), engine.SearchQuery{Text: "SQLite"})
			if err != nil || len(found.Results) == 0 {
				t.Fatal(found, err)
			}
			settings.Embedding.Enabled = false
			stack.rebuildMemoryEngine(settings)
			before := requests.Load()
			localBefore := worker.calls.Load()
			if memory.Enabled() || manager.Enabled() || manager.Status().State != "disabled" {
				t.Fatal("hot reload did not disable memory")
			}
			if _, err := access.Ingest(context.Background(), candidate); !errors.Is(err, engine.ErrMemoryDisabled) {
				t.Fatal(err)
			}
			if _, err := access.Search(context.Background(), engine.SearchQuery{Text: "SQLite"}); !errors.Is(err, engine.ErrMemoryDisabled) {
				t.Fatal(err)
			}
			if _, err := access.Timeline(context.Background(), "", "", 10); !errors.Is(err, engine.ErrMemoryDisabled) {
				t.Fatal(err)
			}
			if _, err := manager.Search(context.Background(), engine.SearchQuery{Text: "SQLite"}, 10); err != nil {
				t.Fatal(err)
			}
			manager.IndexSaved(context.Background(), saved.ContentHash[:16], saved.ContentHash, candidate.Content, engine.OwnerL1, "", engine.ScopeGlobal, "")
			if requests.Load() != before || worker.calls.Load() != localBefore {
				t.Fatal("disabled mode called an embedding backend")
			}
			var count int
			if err := shared.DB.QueryRow("SELECT COUNT(*) FROM mem_entries").Scan(&count); err != nil || count != 1 {
				t.Fatal("off deleted saved memory", count, err)
			}
			settings.Embedding.Enabled = true
			stack.rebuildMemoryEngine(settings)
			waitReady()
			found, err = access.Search(context.Background(), engine.SearchQuery{Text: "SQLite"})
			if err != nil || len(found.Results) == 0 {
				t.Fatal("re-enable lost data", found, err)
			}
			if stack.MemoryEngine != memory || stack.ToolsCfg.MemoryEngine != memory || stack.Embedding != manager {
				t.Fatal("hot reload replaced bound dependencies")
			}
		})
	}
}

func TestMemoryOffHotReloadDrainsAdmittedBoundQueries(t *testing.T) {
	for _, owner := range []string{"l1", "l2"} {
		t.Run(owner, func(t *testing.T) {
			var block atomic.Bool
			var requests atomic.Int32
			entered, release := make(chan struct{}), make(chan struct{})
			var enteredOnce, releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if block.Load() {
					enteredOnce.Do(func() { close(entered) })
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
				vector := make([]float32, 384)
				vector[0] = 1
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"index": 0, "embedding": vector}}})
			}))
			defer upstream.Close()
			defer unblock()
			dir := t.TempDir()
			shared, err := db.Open(filepath.Join(dir, "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer shared.Close()
			log, err := logger.System(dir, logger.WithConsole(false), logger.WithFile(false))
			if err != nil {
				t.Fatal(err)
			}
			defer log.Close()
			settings := config.DefaultSettings()
			settings.Embedding.Provider = "openai"
			settings.Embedding.Enabled = true
			settings.Embedding.Providers[0].Enabled = true
			settings.Embedding.Providers[0].BaseURL = upstream.URL
			settings.Embedding.Models[0].Enabled = true
			settings.Embedding.Models[0].Dimension = 384
			stack := &Stack{SharedDB: shared, Log: log, workDir: dir}
			stack.rebuildMemoryEngine(settings)
			defer stack.Embedding.Close()
			memory := stack.MemoryEngine
			access := memory.BindL1(engine.ScopeGlobal, "", false)
			if owner == "l2" {
				access, err = memory.BindL2Group("12345678-1234-1234-1234-123456789abc")
				if err != nil {
					t.Fatal(err)
				}
			}
			candidate := engine.MemoryCandidate{Content: "SQLite retained durable fact", MemoryType: engine.MemoryTypeStableFact, SourceType: engine.SourceExplicit}
			if _, err := access.Ingest(context.Background(), candidate); err != nil {
				t.Fatal(err)
			}
			block.Store(true)
			queryDone := make(chan error, 1)
			go func() {
				found, err := access.Search(context.Background(), engine.SearchQuery{Text: "SQLite"})
				if err == nil && len(found.Results) == 0 {
					err = errors.New("admitted query lost retained memory")
				}
				queryDone <- err
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("query did not reach paused embedding")
			}
			settings.Embedding.Enabled = false
			offDone := make(chan error, 1)
			go func() { offDone <- stack.OnSettingsChange(settings) }()
			select {
			case err := <-offDone:
				t.Fatalf("hot reload OFF returned before query drained: %v", err)
			case <-time.After(50 * time.Millisecond):
			}
			unblock()
			select {
			case err := <-queryDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("admitted query deadlocked")
			}
			select {
			case err := <-offDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("hot reload OFF deadlocked")
			}
			before := requests.Load()
			if _, err := access.Search(context.Background(), engine.SearchQuery{Text: "SQLite"}); !errors.Is(err, engine.ErrMemoryDisabled) {
				t.Fatal(err)
			}
			if _, err := access.Ingest(context.Background(), candidate); !errors.Is(err, engine.ErrMemoryDisabled) {
				t.Fatal(err)
			}
			if _, err := access.Timeline(context.Background(), "", "", 10); !errors.Is(err, engine.ErrMemoryDisabled) {
				t.Fatal(err)
			}
			if requests.Load() != before {
				t.Fatal("embedding called after OFF completed")
			}
			settings.Embedding.Enabled = true
			if err := stack.OnSettingsChange(settings); err != nil {
				t.Fatal(err)
			}
			found, err := access.Search(context.Background(), engine.SearchQuery{Text: "SQLite"})
			if err != nil || len(found.Results) == 0 || stack.MemoryEngine != memory {
				t.Fatal("re-enable lost data or replaced stable engine", found, err)
			}
		})
	}
}
