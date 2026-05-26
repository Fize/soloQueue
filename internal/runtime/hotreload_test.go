package runtime

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
)

func TestRegisterPromptHotReload(t *testing.T) {
	tempDir := t.TempDir()
	rolesDir := filepath.Join(tempDir, "prompts", "roles")
	globalDir := filepath.Join(tempDir, "prompts", "global")
	if err := os.MkdirAll(rolesDir, 0o755); err != nil {
		t.Fatalf("failed to create temp roles dir: %v", err)
	}

	// Create initial rules.md and soul.md
	soulFile := filepath.Join(rolesDir, "soul.md")
	rulesFile := filepath.Join(rolesDir, "rules.md")
	if err := os.WriteFile(soulFile, []byte("original soul"), 0o644); err != nil {
		t.Fatalf("failed to write soul: %v", err)
	}
	if err := os.WriteFile(rulesFile, []byte("original rules"), 0o644); err != nil {
		t.Fatalf("failed to write rules: %v", err)
	}

	rt := &Stack{
		PromptCfg: &prompt.PromptConfig{
			RolesDir:  rolesDir,
			GlobalDir: globalDir,
		},
	}

	log, err := logger.System(tempDir, logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatalf("failed to init logger: %v", err)
	}
	defer log.Close()

	var rebuildCalled int
	var rebuildMu sync.Mutex
	rebuildCond := sync.NewCond(&rebuildMu)

	rt.OnPromptRebuild(func() error {
		rebuildMu.Lock()
		rebuildCalled++
		rebuildCond.Broadcast()
		rebuildMu.Unlock()
		return nil
	})

	registerPromptHotReload(rt, log, "", "")
	defer rt.Shutdown()

	// Modify soul.md
	if err := os.WriteFile(soulFile, []byte("modified soul"), 0o644); err != nil {
		t.Fatalf("failed to modify soul: %v", err)
	}

	// Wait for rebuild to be called (with timeout)
	rebuildMu.Lock()
	done := make(chan struct{})
	go func() {
		rebuildMu.Lock()
		defer rebuildMu.Unlock()
		for rebuildCalled == 0 {
			rebuildCond.Wait()
		}
		close(done)
	}()
	rebuildMu.Unlock()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for prompt hot-reload on soul.md change")
	}

	// Reset counter and modify rules.md
	rebuildMu.Lock()
	rebuildCalled = 0
	rebuildMu.Unlock()

	if err := os.WriteFile(rulesFile, []byte("modified rules"), 0o644); err != nil {
		t.Fatalf("failed to modify rules: %v", err)
	}

	rebuildMu.Lock()
	done2 := make(chan struct{})
	go func() {
		rebuildMu.Lock()
		defer rebuildMu.Unlock()
		for rebuildCalled == 0 {
			rebuildCond.Wait()
		}
		close(done2)
	}()
	rebuildMu.Unlock()

	select {
	case <-done2:
		// success
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for prompt hot-reload on rules.md change")
	}
}
