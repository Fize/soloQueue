package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/runtime"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

func TestInstallChannelConfigReloadUsesAcceptedFileWatchCandidate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(path, []byte("log:\n  level: info\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Load(); err != nil {
		t.Fatal(err)
	}
	cfg.SetOnChange(func(candidate config.Settings) error {
		if candidate.Log.Level == "invalid" {
			return context.Canceled
		}
		return nil
	})
	reloadErrors := make(chan error, 1)
	cfg.SetOnError(func(err error) { reloadErrors <- err })
	qqReloaded := make(chan config.Settings, 2)
	wechatReloaded := make(chan config.Settings, 2)
	installChannelConfigReload(cfg,
		func(candidate config.Settings) { qqReloaded <- candidate },
		func(candidate config.Settings) { wechatReloaded <- candidate },
	)
	if err := cfg.Watch(); err != nil {
		t.Fatal(err)
	}
	defer cfg.StopWatch()

	if err := os.WriteFile(path, []byte("log:\n  level: debug\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, reloaded := range map[string]<-chan config.Settings{"QQ": qqReloaded, "WeChat": wechatReloaded} {
		select {
		case candidate := <-reloaded:
			if candidate.Log.Level != "debug" || cfg.Get().Log.Level != "debug" {
				t.Fatalf("%s reload candidate/current = %q/%q, want debug/debug", name, candidate.Log.Level, cfg.Get().Log.Level)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s did not receive accepted file-watch candidate", name)
		}
	}

	if err := os.WriteFile(path, []byte("log:\n  level: invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reloadErrors:
	case <-time.After(2 * time.Second):
		t.Fatal("rejected candidate error was not reported")
	}
	for name, reloaded := range map[string]<-chan config.Settings{"QQ": qqReloaded, "WeChat": wechatReloaded} {
		select {
		case candidate := <-reloaded:
			t.Fatalf("%s reloaded rejected candidate %q", name, candidate.Log.Level)
		default:
		}
	}
	if got := cfg.Get().Log.Level; got != "debug" {
		t.Fatalf("rejected candidate published: %q", got)
	}
}

func TestInstallChannelConfigReloadUsesProgrammaticSetCandidate(t *testing.T) {
	cfg, err := config.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Load(); err != nil {
		t.Fatal(err)
	}
	qqReloaded := make(chan config.Settings, 2)
	wechatReloaded := make(chan config.Settings, 2)
	installChannelConfigReload(cfg,
		func(candidate config.Settings) { qqReloaded <- candidate },
		func(candidate config.Settings) { wechatReloaded <- candidate },
	)

	if err := cfg.UpdateQQBots([]config.QQBotConfig{{ID: "qq-new", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	for name, reloaded := range map[string]<-chan config.Settings{"QQ": qqReloaded, "WeChat": wechatReloaded} {
		select {
		case candidate := <-reloaded:
			if len(candidate.QQBots) != 1 || candidate.QQBots[0].ID != "qq-new" {
				t.Fatalf("%s received candidate = %#v, want qq-new", name, candidate.QQBots)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s did not receive programmatic QQ candidate", name)
		}
	}

	if err := cfg.UpdateWechatBots([]config.WechatBotConfig{{ID: "wechat-new", Enabled: true}}); err != nil {
		t.Fatal(err)
	}

	for name, reloaded := range map[string]<-chan config.Settings{"QQ": qqReloaded, "WeChat": wechatReloaded} {
		select {
		case candidate := <-reloaded:
			if len(candidate.WechatBots) != 1 || candidate.WechatBots[0].ID != "wechat-new" {
				t.Fatalf("%s received candidate = %#v, want wechat-new", name, candidate.WechatBots)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s did not receive programmatic Set candidate", name)
		}
	}
}

func TestCronSessionCleanupStopsAndUnregistersAgents(t *testing.T) {
	registry := agent.NewRegistry(nil)
	leader := agent.NewAgent(
		agent.Definition{ID: "cron-leader", Name: "Cron Leader"},
		&agenttest.FakeLLM{},
		nil,
	)
	child := agent.NewAgent(
		agent.Definition{ID: "cron-worker", Name: "Cron Worker"},
		&agenttest.FakeLLM{},
		nil,
	)
	for _, a := range []*agent.Agent{leader, child} {
		if err := registry.Register(a); err != nil {
			t.Fatalf("register %s: %v", a.Def.ID, err)
		}
		if err := a.Start(context.Background()); err != nil {
			t.Fatalf("start %s: %v", a.Def.ID, err)
		}
	}

	supervisor := agent.NewSupervisor(leader, &cronCleanupTestFactory{registry: registry}, nil)
	supervisor.AdoptChild(child)
	cronSession := session.NewSession("cron-task", "cron-team", leader, nil, nil, nil)
	cronSession.SetSupervisor(supervisor, nil)

	cleanup := newCronSessionCleanup(cronSession, registry)
	cleanup()
	cleanup()

	if got := registry.Len(); got != 0 {
		t.Fatalf("registry Len = %d, want 0", got)
	}
	if got := leader.State(); got != agent.StateStopped {
		t.Fatalf("leader state = %s, want stopped", got)
	}
	if got := child.State(); got != agent.StateStopped {
		t.Fatalf("child state = %s, want stopped", got)
	}
	if got := supervisor.ChildCount(); got != 0 {
		t.Fatalf("supervisor child count = %d, want 0", got)
	}
}

type cronCleanupTestFactory struct {
	registry *agent.Registry
}

func (f *cronCleanupTestFactory) Create(context.Context, agent.AgentTemplate, string) (*agent.Agent, *ctxwin.ContextWindow, error) {
	panic("unexpected Create call")
}

func (f *cronCleanupTestFactory) CreateWithOptions(
	context.Context,
	agent.AgentTemplate,
	string,
	agent.CreateOptions,
) (*agent.Agent, *ctxwin.ContextWindow, error) {
	panic("unexpected CreateWithOptions call")
}

func (f *cronCleanupTestFactory) Registry() *agent.Registry {
	return f.registry
}

func (f *cronCleanupTestFactory) RebuildLeaderPrompt(tmpl agent.AgentTemplate, _ string) (string, error) {
	return tmpl.SystemPrompt, nil
}

func (f *cronCleanupTestFactory) ResolveTemplate(_ context.Context, _ string) (agent.AgentTemplate, bool) {
	return agent.AgentTemplate{}, false
}

func TestL1PromptReloadUpdatesResidentAndSkipsBusyTurn(t *testing.T) {
	requests := make(chan agent.LLMRequest, 2)
	release := make(chan struct{})
	fake := &agenttest.FakeLLM{Responses: []string{"done", "done"}, Hook: func(req agent.LLMRequest) {
		requests <- req
		<-release
	}}
	a := agent.NewAgent(agent.Definition{ID: "resident", SystemPrompt: "old soul"}, fake, nil)
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(time.Second)
	cw := ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer())
	cw.Push(ctxwin.RoleSystem, "old soul")
	cw.Push(ctxwin.RoleSystem, "[persona_state] preserved")
	cw.Push(ctxwin.RoleUser, "past conversation")
	resident := session.NewSession("resident", "L1", a, cw, nil, nil)
	rt := &runtime.Stack{SystemPrompt: "old soul"}
	next := "profile update"
	reload := installL1PromptReload(rt, &session.Builder{}, func() *session.Session { return resident }, func() error { rt.SetSystemPrompt(next); return nil })
	// Profile-save callback uses the same resident reconciliation as file reload.
	if err := reload(); err != nil {
		t.Fatal(err)
	}
	if msg, _ := cw.MessageAt(0); msg.Content != next {
		t.Fatalf("profile did not replace resident prompt: %s", msg.Content)
	}
	if msg, _ := cw.MessageAt(1); msg.Content != "[persona_state] preserved" {
		t.Fatal("state changed")
	}
	if msg, _ := cw.MessageAt(2); msg.Content != "past conversation" {
		t.Fatal("history changed")
	}
	next = "file watcher update"
	if err := rt.RebuildPrompt(); err != nil {
		t.Fatal(err)
	}
	if msg, _ := cw.MessageAt(0); msg.Content != next {
		t.Fatal("watcher callback did not update resident")
	}
	events, err := resident.AskStream(context.Background(), "running task")
	if err != nil {
		t.Fatal(err)
	}
	<-requests // The first model request is active and blocked until reload finishes.
	next = "saved while busy"
	if err := reload(); !errors.Is(err, session.ErrSessionBusy) {
		t.Fatalf("busy reload=%v", err)
	}
	if msg, _ := cw.MessageAt(0); msg.Content != "file watcher update" {
		t.Fatal("busy resident mutated")
	}
	if rt.SystemPrompt != next {
		t.Fatal("runtime did not retain saved prompt")
	}
	close(release)
	for range events {
	}
	// No second reload: the next request must consume the pending saved prompt.
	events, err = resident.AskStream(context.Background(), "next task")
	if err != nil {
		t.Fatal(err)
	}
	req := <-requests
	if len(req.Messages) == 0 || req.Messages[0].Content != next {
		t.Errorf("next request used stale prompt: %+v", req.Messages)
	}
	for range events {
	}
	if msg, _ := cw.MessageAt(0); msg.Content != next {
		t.Fatal("next turn did not apply saved prompt")
	}
}
