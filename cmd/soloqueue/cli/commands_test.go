package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
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
