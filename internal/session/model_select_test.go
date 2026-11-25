package session

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/transcript"
)

func stubModelLookup(providerID, modelID string) (ModelSpec, bool) {
	if providerID != "p" || modelID != "big" {
		return ModelSpec{}, false
	}
	return ModelSpec{
		ProviderID:       "p",
		APIModel:         "big-api",
		Name:             "Big",
		ContextWindow:    4096,
		Vision:           true,
		ThinkingEnabled:  true,
		ReasoningEfforts: []string{"low", "medium", "high", "xhigh", "max"},
	}, true
}

func TestSessionModelSelectionPersistsAndHydrates(t *testing.T) {
	metaPath := filepath.Join(t.TempDir(), "l2-x", MetaFileName)

	s := NewSession("l2:x", "team", nil, nil, nil, nil)
	s.usageMetaPath = metaPath
	s.modelLookup = stubModelLookup

	if err := s.SetModelSelection(ModelSelection{ProviderID: "p", ModelID: "big", ReasoningEffort: "max"}); err != nil {
		t.Fatalf("SetModelSelection: %v", err)
	}

	meta, err := LoadMetaPath(metaPath)
	if err != nil {
		t.Fatalf("LoadMetaPath: %v", err)
	}
	if meta.Model.ModelID != "big" || meta.Model.ReasoningEffort != "max" {
		t.Fatalf("persisted selection = %+v", meta.Model)
	}

	fresh := NewSession("l2:x", "team", nil, nil, nil, nil)
	fresh.usageMetaPath = metaPath
	fresh.hydrateModelSelection(meta)
	if got := fresh.ModelSelection(); got.ModelID != "big" || got.ReasoningEffort != "max" {
		t.Fatalf("hydrated selection = %+v", got)
	}

	if err := fresh.SetModelSelection(ModelSelection{}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	meta, err = LoadMetaPath(metaPath)
	if err != nil {
		t.Fatalf("LoadMetaPath after clear: %v", err)
	}
	if !meta.Model.IsAuto() {
		t.Fatalf("cleared selection persisted as %+v", meta.Model)
	}
}

func TestSetModelSelectionRejectsUnavailableModel(t *testing.T) {
	s := NewSession("l2:x", "team", nil, nil, nil, nil)
	s.modelLookup = stubModelLookup

	if err := s.SetModelSelection(ModelSelection{ProviderID: "p", ModelID: "missing"}); err == nil {
		t.Fatal("expected an error for a model that is not enabled")
	}
	if err := s.SetModelSelection(ModelSelection{ProviderID: "p", ModelID: "big", ReasoningEffort: "extreme"}); err == nil {
		t.Fatal("expected an error for an unsupported reasoning effort")
	}
	if !s.ModelSelection().IsAuto() {
		t.Fatalf("rejected selection was stored: %+v", s.ModelSelection())
	}
}

func TestSessionModelSelectionOverridesAskModel(t *testing.T) {
	var gotModel, gotProvider string
	fake := &agenttest.FakeLLM{
		StreamDeltas: [][]string{{"ok"}},
		Hook: func(req agent.LLMRequest) {
			gotModel, gotProvider = req.Model, req.ProviderID
		},
	}
	a := startAgent(t, fake)
	cw := ctxwin.NewContextWindow(8192, 1000, 0, ctxwin.NewTokenizer())
	s := NewSession("l2:x", "team", a, cw, nil, nil)
	s.SetTranscriptRegistry(transcript.NewRegistry(transcript.Config{Grace: time.Hour}))
	s.modelLookup = stubModelLookup

	if err := s.SetModelSelection(ModelSelection{ProviderID: "p", ModelID: "big"}); err != nil {
		t.Fatalf("SetModelSelection: %v", err)
	}

	stream, err := s.AskStream(desktopRequest("req-model"), "hello")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	route := routeFrameNow(t, s, "req-model")
	if route.ModelID != "big-api" || route.ProviderID != "p" {
		t.Fatalf("route frame = %+v, want the session model", route)
	}
	for range stream {
	}

	if gotModel != "big-api" || gotProvider != "p" {
		t.Fatalf("ask used %q/%q, want p/big-api", gotProvider, gotModel)
	}
	if _, max, _ := cw.TokenUsage(); max != 4096 {
		t.Fatalf("context window = %d, want the selected model's 4096", max)
	}
}

func TestSessionModelSelectionDegradesWhenModelBecomesUnavailable(t *testing.T) {
	var gotModel, gotProvider string
	fake := &agenttest.FakeLLM{
		StreamDeltas: [][]string{{"ok"}},
		Hook: func(req agent.LLMRequest) {
			gotModel, gotProvider = req.Model, req.ProviderID
		},
	}
	a := agent.NewAgent(agent.Definition{ID: "test-agent", ModelID: "default-model", ProviderID: "default-provider"}, fake, nil)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("agent Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(0) })

	s := NewSession("l2:x", "team", a, ctxwin.NewContextWindow(8192, 1000, 0, ctxwin.NewTokenizer()), nil, nil)
	s.modelLookup = func(providerID, modelID string) (ModelSpec, bool) {
		return ModelSpec{}, false
	}
	// Bypass validation so the disabled-model path is exercised at ask time.
	s.modelSelMu.Lock()
	s.modelSelection = ModelSelection{ProviderID: "p", ModelID: "big"}
	s.modelSelMu.Unlock()

	stream, err := s.AskStream(context.Background(), "hello")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	for range stream {
	}

	if gotModel != "default-model" || gotProvider != "default-provider" {
		t.Fatalf("ask used %q/%q, want the definition model", gotProvider, gotModel)
	}
}

func TestModelCommand(t *testing.T) {
	s := NewSession("l2:x", "team", nil, nil, nil, nil)
	s.modelLookup = stubModelLookup

	if got := s.ModelCommand(""); got != "Model: auto (configured default)" {
		t.Fatalf("empty command = %q", got)
	}
	if got := s.ModelCommand("p:big max"); got != "Model: p:big (max)" {
		t.Fatalf("set command = %q", got)
	}
	if got := s.ModelSelection(); got.ModelID != "big" || got.ReasoningEffort != "max" {
		t.Fatalf("selection = %+v", got)
	}
	if got := s.ModelCommand("show"); got != "Model: p:big (max)" {
		t.Fatalf("show command = %q", got)
	}
	if got := s.ModelCommand("p:missing"); got != "Model not changed: model p:missing is not enabled" {
		t.Fatalf("reject command = %q", got)
	}
	if got := s.ModelCommand("auto"); got != "Model: auto (configured default)" {
		t.Fatalf("auto command = %q", got)
	}
	if !s.ModelSelection().IsAuto() {
		t.Fatalf("selection after auto = %+v", s.ModelSelection())
	}
	if got := s.ModelCommand("nonsense"); got == "" {
		t.Fatal("expected a usage hint")
	}
}

// effortModelLookup declares only the efforts each test needs.
func effortModelLookup(efforts ...string) ModelLookup {
	return func(providerID, modelID string) (ModelSpec, bool) {
		if providerID != "p" || modelID != "big" {
			return ModelSpec{}, false
		}
		return ModelSpec{
			ProviderID:       "p",
			APIModel:         "big-api",
			Name:             "Big",
			ContextWindow:    4096,
			ThinkingEnabled:  true,
			ReasoningEfforts: efforts,
		}, true
	}
}

func TestSetModelSelectionValidatesDeclaredCapabilities(t *testing.T) {
	s := NewSession("l2:x", "team", nil, nil, nil, nil)
	s.modelLookup = effortModelLookup("low", "medium", "high", "xhigh", "max")

	// xhigh used to be rejected by a hardcoded whitelist; a model declaring it
	// accepts it.
	if err := s.SetModelSelection(ModelSelection{ProviderID: "p", ModelID: "big", ReasoningEffort: "xhigh"}); err != nil {
		t.Fatalf("SetModelSelection(xhigh): %v", err)
	}
	if got := s.ModelSelection().ReasoningEffort; got != "xhigh" {
		t.Fatalf("selection = %q", got)
	}

	err := s.SetModelSelection(ModelSelection{ProviderID: "p", ModelID: "big", ReasoningEffort: "ultra"})
	if err == nil {
		t.Fatal("expected an undeclared effort to be rejected")
	}
	if !strings.Contains(err.Error(), "available: low, medium, high, xhigh, max") {
		t.Fatalf("error does not list available efforts: %v", err)
	}

	// A model that declares nothing accepts no effort.
	narrow := NewSession("l2:y", "team", nil, nil, nil, nil)
	narrow.modelLookup = effortModelLookup()
	if err := narrow.SetModelSelection(ModelSelection{ProviderID: "p", ModelID: "big", ReasoningEffort: "low"}); err == nil {
		t.Fatal("expected an effort on a model without capabilities to be rejected")
	}
	if err := narrow.SetModelSelection(ModelSelection{ProviderID: "p", ModelID: "big"}); err != nil {
		t.Fatalf("effort-less selection should pass: %v", err)
	}
}

func TestHydrateDropsUnsupportedReasoningEffort(t *testing.T) {
	s := NewSession("l2:x", "team", nil, nil, nil, nil)
	s.modelLookup = effortModelLookup("low", "medium")

	s.hydrateModelSelection(&SessionMeta{Model: ModelSelection{
		ProviderID: "p", ModelID: "big", ReasoningEffort: "max",
	}})

	got := s.ModelSelection()
	if got.ModelID != "big" {
		t.Fatalf("model should survive: %+v", got)
	}
	if got.ReasoningEffort != "" {
		t.Fatalf("unsupported effort not dropped: %+v", got)
	}
}

func TestModelOverrideSendsOnlySelectedEffort(t *testing.T) {
	s := NewSession("l2:x", "team", nil, nil, nil, nil)
	s.modelLookup = effortModelLookup("low", "medium", "high")

	if err := s.SetModelSelection(ModelSelection{ProviderID: "p", ModelID: "big"}); err != nil {
		t.Fatalf("SetModelSelection: %v", err)
	}
	params, ok := s.modelOverrideParams()
	if !ok || !params.ThinkingEnabled {
		t.Fatalf("override = %+v ok=%v", params, ok)
	}
	if params.ReasoningEffort != "" {
		t.Fatalf("no selection must send no effort, got %q", params.ReasoningEffort)
	}

	if err := s.SetModelSelection(ModelSelection{ProviderID: "p", ModelID: "big", ReasoningEffort: "medium"}); err != nil {
		t.Fatalf("SetModelSelection(medium): %v", err)
	}
	params, _ = s.modelOverrideParams()
	if params.ReasoningEffort != "medium" {
		t.Fatalf("effort = %q, want medium", params.ReasoningEffort)
	}
}
