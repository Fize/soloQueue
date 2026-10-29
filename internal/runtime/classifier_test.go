package runtime

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/router"
	"github.com/xiaobaitu/soloqueue/internal/tasktype"
)

type runtimeDecider struct{ calls atomic.Int32 }

func (d *runtimeDecider) Decide(context.Context, string) (tasktype.TaskType, error) {
	d.calls.Add(1)
	return tasktype.Research, nil
}

func TestClassifierHotReloadSwitchesActualRouter(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	log, err := logger.System(dir, logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	client := &agenttest.FakeLLM{Responses: []string{`{"task_type":"general"}`}}
	decider := &runtimeDecider{}
	classifier := router.NewDefaultClassifier(router.DefaultClassifierConfig(), client, "provider", "model", decider, nil)
	r := router.NewRouter(classifier, cfg, nil)
	stack := &Stack{TaskRouter: r, Log: log, workDir: dir}
	settings := config.DefaultSettings()
	for _, tt := range []struct {
		engine string
		source router.ClassificationSource
	}{
		{"llm", router.SourceLLM}, {"laya", router.SourceLaya}, {"llm", router.SourceLLM}, {"laya", router.SourceLaya}, {"", router.SourceLLM},
	} {
		settings.Classifier.Engine = tt.engine
		if err := stack.OnSettingsChange(settings); err != nil {
			t.Fatal(err)
		}
		decision, err := r.Route(context.Background(), router.ClassifyInput{Text: "continue"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if decision.Classification.Source != tt.source {
			t.Fatalf("engine %q: source %s, want %s", tt.engine, decision.Classification.Source, tt.source)
		}
	}
	if client.CallCount() != 3 || decider.calls.Load() != 2 {
		t.Fatalf("llm calls=%d local calls=%d", client.CallCount(), decider.calls.Load())
	}
}

func TestModelResolverDistinguishesProviders(t *testing.T) {
	cfg, err := config.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = cfg.Set(func(s *config.Settings) {
		s.Models = []config.LLMModel{{ID: "same", ProviderID: "first", Enabled: false}, {ID: "same", ProviderID: "second", Enabled: true}}
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := BuildModelResolver(cfg)("second:same")
	if err != nil {
		t.Fatal(err)
	}
	if got.ProviderID != "second" || got.APIModel != "same" {
		t.Fatalf("resolved %+v", got)
	}
}
