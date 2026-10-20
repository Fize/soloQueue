package runtime

import (
	"context"
	"path/filepath"
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

type runtimeTestLocalWorker struct{}

func (*runtimeTestLocalWorker) Embed(context.Context, []string, string) (local.Batch, error) {
	return local.Batch{}, nil
}
func (*runtimeTestLocalWorker) Status() local.Status {
	return local.Status{Model: "intfloat/multilingual-e5-small", Dimension: 384, Backend: "cpu"}
}
func (*runtimeTestLocalWorker) Close() error { return nil }
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
