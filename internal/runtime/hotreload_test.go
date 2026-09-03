package runtime

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/skill"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
)

func TestRegisterSkillHotReload_DelayedEntrypointWrite(t *testing.T) {
	root := t.TempDir()
	log, err := logger.System(root, logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatalf("logger.System: %v", err)
	}
	defer log.Close()

	reg := skill.NewSkillRegistry()
	closeWatcher := registerSkillHotReload(reg, map[string]string{"user": root}, log)
	defer closeWatcher()

	skillDir := filepath.Join(root, "delayed")
	if err := os.Mkdir(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	time.Sleep(350 * time.Millisecond)
	content := "---\nname: delayed\ndescription: loaded after extraction\n---\n\nInstructions.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := reg.GetSkill("delayed"); ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("delayed SKILL.md write was not hot-reloaded")
}

func TestRegisterSkillHotReload_UpdatesExistingSkill(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "versioned")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	path := filepath.Join(skillDir, "SKILL.md")
	writeSkillMD(t, path, "versioned", "first")

	log, err := logger.System(root, logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatalf("logger.System: %v", err)
	}
	defer log.Close()
	reg := skill.NewSkillRegistry()
	if err := reg.Rebuild(map[string]string{"user": root}); err != nil {
		t.Fatalf("initial rebuild: %v", err)
	}
	closeWatcher := registerSkillHotReload(reg, map[string]string{"user": root}, log)
	defer closeWatcher()

	writeSkillMD(t, path, "versioned", "second")
	waitForSkill(t, reg, "versioned", "second")
}

func TestRegisterSkillHotReload_RemovesDeletedSkill(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "removable")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	writeSkillMD(t, filepath.Join(skillDir, "SKILL.md"), "removable", "present")

	log, err := logger.System(root, logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatalf("logger.System: %v", err)
	}
	defer log.Close()
	reg := skill.NewSkillRegistry()
	if err := reg.Rebuild(map[string]string{"user": root}); err != nil {
		t.Fatalf("initial rebuild: %v", err)
	}
	closeWatcher := registerSkillHotReload(reg, map[string]string{"user": root}, log)
	defer closeWatcher()

	if err := os.RemoveAll(skillDir); err != nil {
		t.Fatalf("remove skill: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := reg.GetSkill("removable"); !ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("deleted skill remained in registry")
}

func TestStackShutdown_ClosesSkillHotReload(t *testing.T) {
	root := t.TempDir()
	log, err := logger.System(root, logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatalf("logger.System: %v", err)
	}
	defer log.Close()

	reg := skill.NewSkillRegistry()
	rt := &Stack{SkillRegistry: reg}
	rt.skillWatcherClose = registerSkillHotReload(reg, map[string]string{"user": root}, log)
	rt.Shutdown()

	skillDir := filepath.Join(root, "after-shutdown")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	writeSkillMD(t, filepath.Join(skillDir, "SKILL.md"), "after-shutdown", "must not load")
	time.Sleep(400 * time.Millisecond)
	if _, ok := reg.GetSkill("after-shutdown"); ok {
		t.Fatal("skill watcher rebuilt registry after Stack.Shutdown")
	}
}

func TestSkillWatcher_SerializesRebuildAndWaitsOnClose(t *testing.T) {
	root := t.TempDir()
	log, err := logger.System(root, logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatalf("logger.System: %v", err)
	}
	defer log.Close()

	sw, err := newSkillWatcher(skill.NewSkillRegistry(), map[string]string{"user": root}, log)
	if err != nil {
		t.Fatalf("newSkillWatcher: %v", err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	calls, active, maxActive := 0, 0, 0
	sw.rebuildFn = func() error {
		mu.Lock()
		calls++
		active++
		if active > maxActive {
			maxActive = active
		}
		call := calls
		mu.Unlock()

		if call == 1 {
			close(started)
			<-release
		}

		mu.Lock()
		active--
		mu.Unlock()
		return nil
	}
	go sw.run()

	sw.scheduleRebuild()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		sw.Close()
		t.Fatal("first rebuild did not start")
	}
	sw.scheduleRebuild()

	closeDone := make(chan struct{})
	go func() {
		sw.Close()
		close(closeDone)
	}()
	select {
	case <-closeDone:
		t.Fatal("Close returned while rebuild callback was active")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	select {
	case <-closeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not wait for skill watcher shutdown")
	}

	mu.Lock()
	defer mu.Unlock()
	if maxActive != 1 {
		t.Fatalf("max concurrent rebuilds = %d, want 1", maxActive)
	}
}

func TestRegisterSkillHotReload_IgnoresSymlinkDirectory(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	externalSkillDir := filepath.Join(external, "outside")
	if err := os.MkdirAll(externalSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkillMD(t, filepath.Join(externalSkillDir, "SKILL.md"), "outside", "must not load")
	log, err := logger.System(root, logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatalf("logger.System: %v", err)
	}
	defer log.Close()
	reg := skill.NewSkillRegistry()
	closeWatcher := registerSkillHotReload(reg, map[string]string{"user": root}, log)
	defer closeWatcher()

	if err := os.Symlink(externalSkillDir, filepath.Join(root, "outside")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if _, ok := reg.GetSkill("outside"); ok {
		t.Fatal("watcher loaded a Skill through a symlink directory")
	}
}

func writeSkillMD(t *testing.T, path, name, instruction string) {
	t.Helper()
	content := "---\nname: " + name + "\ndescription: " + instruction + "\n---\n\n" + instruction + " instructions.\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func waitForSkill(t *testing.T, reg *skill.SkillRegistry, id, description string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if loaded, ok := reg.GetSkill(id); ok && loaded.Description == description {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("skill %q did not reach description %q", id, description)
}

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
