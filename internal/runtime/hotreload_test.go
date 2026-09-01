package runtime

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
)

func TestRegisterPromptHotReload(t *testing.T) {
	tempDir := t.TempDir()
	rolesDir := filepath.Join(tempDir, "persona", "roles")
	globalDir := filepath.Join(tempDir, "persona", "global")
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

	// Reset counter and modify user.md
	rebuildMu.Lock()
	rebuildCalled = 0
	rebuildMu.Unlock()

	userFile := filepath.Join(globalDir, "user.md")
	if err := os.WriteFile(userFile, []byte("original user context"), 0o644); err != nil {
		t.Fatalf("failed to write user.md: %v", err)
	}

	rebuildMu.Lock()
	done3 := make(chan struct{})
	go func() {
		rebuildMu.Lock()
		defer rebuildMu.Unlock()
		for rebuildCalled == 0 {
			rebuildCond.Wait()
		}
		close(done3)
	}()
	rebuildMu.Unlock()

	// Modify user.md to trigger change
	if err := os.WriteFile(userFile, []byte("modified user context"), 0o644); err != nil {
		t.Fatalf("failed to modify user.md: %v", err)
	}

	select {
	case <-done3:
		// success
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for prompt hot-reload on user.md change")
	}
}

func TestReloadAgentTemplates_UpdatesFactoryCache(t *testing.T) {
	dir := t.TempDir()
	log, err := logger.System(dir, logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.System: %v", err)
	}
	defer log.Close()

	agentsDir := filepath.Join(dir, "agents")
	groupsDir := filepath.Join(dir, "groups")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}
	if err := os.MkdirAll(groupsDir, 0o755); err != nil {
		t.Fatalf("mkdir groups: %v", err)
	}

	// Write initial leader template (note: YAML field is is_leader, not isLeader)
	leaderFile := filepath.Join(agentsDir, "test-leader.md")
	leaderContent := `---
name: test-leader
description: Test leader for hot-reload
is_leader: true
group: test-group
model: ""
---

# Test Leader

ORIGINAL_LEADER_PROMPT
`
	if err := os.WriteFile(leaderFile, []byte(leaderContent), 0o644); err != nil {
		t.Fatalf("write leader: %v", err)
	}

	// Write group file
	groupFile := filepath.Join(groupsDir, "test-group.md")
	groupContent := `---
name: test-group
---

Test group for engineering team.
`
	if err := os.WriteFile(groupFile, []byte(groupContent), 0o644); err != nil {
		t.Fatalf("write group: %v", err)
	}

	// Load templates
	templates, err := agent.LoadAgentTemplates(agentsDir)
	if err != nil || len(templates) == 0 {
		t.Fatalf("LoadAgentTemplates: %v", err)
	}

	// Create factory
	registry := agent.NewRegistry(log)
	factory := agent.NewDefaultFactory(
		registry,
		nil,
		tools.Config{},
		log,
		agent.WithTemplates(templates),
		agent.WithWorkDir(dir),
	)

	// Create a leader agent + supervisor (start not needed for Def.SystemPrompt check)
	var leaderTmpl agent.AgentTemplate
	for _, tmpl := range templates {
		if tmpl.IsLeader {
			leaderTmpl = tmpl
			break
		}
	}
	if leaderTmpl.ID == "" {
		t.Fatal("no leader template found")
	}

	l2Agent := agent.NewAgent(
		agent.Definition{ID: leaderTmpl.ID, SystemPrompt: "ORIGINAL"},
		nil, nil,
	)
	sv := agent.NewSupervisor(l2Agent, factory, log)

	rt := &Stack{
		AgentFactory: factory,
		Supervisors:  []*agent.Supervisor{sv},
	}

	// Now update the template file on disk
	updatedLeader := `---
name: test-leader
description: Test leader for hot-reload
is_leader: true
group: test-group
model: ""
---

# Test Leader

UPDATED_LEADER_PROMPT
`
	if err := os.WriteFile(leaderFile, []byte(updatedLeader), 0o644); err != nil {
		t.Fatalf("update leader: %v", err)
	}

	// Call ReloadAgentTemplates — this is what the hot-reload watcher does
	if err := rt.ReloadAgentTemplates(log, agentsDir, groupsDir); err != nil {
		t.Fatalf("ReloadAgentTemplates: %v", err)
	}

	// 1. Verify factory cache was updated
	if freshTmpl, ok := factory.ResolveTemplate(context.Background(), "test-leader"); !ok {
		t.Fatal("factory.ResolveTemplate: template not found")
	} else if !contains(freshTmpl.SystemPrompt, "UPDATED_LEADER_PROMPT") {
		t.Fatalf("factory cache: SystemPrompt = %q, want UPDATED_LEADER_PROMPT", freshTmpl.SystemPrompt)
	}

	// 2. Verify Supervisor's agent prompt was updated via RebuildLeaderPrompt
	if !contains(l2Agent.Def.SystemPrompt, "UPDATED_LEADER_PROMPT") {
		t.Fatalf("agent.Def.SystemPrompt = %q, want UPDATED_LEADER_PROMPT", l2Agent.Def.SystemPrompt)
	}
}

func contains(s, substr string) bool {
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
