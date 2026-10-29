package laya

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// This opt-in test provisions the real pinned model and runtime and runs one classification.
func TestWindowsRealClassifier(t *testing.T) {
	workDir := os.Getenv("SOLOQUEUE_LAYA_INTEGRATION_WORKDIR")
	if workDir == "" {
		t.Skip("requires a Windows native classifier integration environment")
	}
	binary := os.Getenv("SOLOQUEUE_TEST_MAIN_BINARY")
	if binary == "" {
		t.Fatal("main binary path is required")
	}
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatal(err)
	}
	manager := New(filepath.Clean(workDir), nil)
	manager.workerPath = func() (string, error) { return binary, nil }
	manager.spawnFn = func(ctx context.Context, path, modelDir, _ string) (proc, error) {
		runtimePath, err := manager.runtimePath(ctx, 0)
		if err != nil {
			return nil, err
		}
		return startWorker(ctx, path, modelDir, runtimePath, true, testWorkerBuildID(t, path))
	}
	defer manager.Close()
	manager.Apply(Config{Enabled: true})
	deadline := time.Now().Add(15 * time.Minute)
	for time.Now().Before(deadline) {
		status := manager.Status()
		if status.State == "error" {
			t.Fatalf("classifier preparation failed: %s", status.Error)
		}
		if status.State == "ready" {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			label, err := manager.Decide(ctx, `[{"role":"user","content":"Please inspect this code and fix the failing unit test."}]`)
			if err != nil || !label.Valid() {
				t.Fatalf("classification = %q, err = %v", label, err)
			}
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("classifier preparation timed out: %+v", manager.Status())
}
