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
	if os.Getenv("SOLOQUEUE_DECIDE_WORKER") == "" {
		t.Fatal("classifier worker path is required")
	}
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatal(err)
	}
	manager := New(filepath.Clean(workDir), nil)
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
			label, err := manager.Decide(ctx, "Please inspect this code and fix the failing unit test.")
			if err != nil || !label.Valid() {
				t.Fatalf("classification = %q, err = %v", label, err)
			}
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("classifier preparation timed out: %+v", manager.Status())
}
