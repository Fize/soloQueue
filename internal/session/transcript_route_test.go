package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetry"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/transcript"
)

func newRouteSession(t *testing.T, id string, a *agent.Agent, route RouteResult) *Session {
	t.Helper()
	s := NewSession(id, "team", a, ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer()), nil, nil)
	s.SetTranscriptRegistry(transcript.NewRegistry(transcript.Config{Grace: time.Hour}))
	s.Router = func(context.Context, string, string, []ctxwin.PayloadMessage) (RouteResult, error) {
		return route, nil
	}
	return s
}

func desktopRequest(id string) context.Context {
	return telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{RequestID: id, Origin: telemetry.OriginDesktop})
}

// routeFrameNow returns the request's route frame without waiting: the route
// must be in the transcript by the time AskStream returns.
func routeFrameNow(t *testing.T, s *Session, requestID string) transcript.Frame {
	t.Helper()
	j := s.Transcript()
	frames, err := j.ReadAfter(j.Epoch(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range frames {
		if f.Kind == transcript.KindRequestRoute && f.RequestID == requestID {
			return f
		}
	}
	t.Fatalf("no route frame for %s when AskStream returned", requestID)
	return transcript.Frame{}
}

func TestRouteFrameCarriesTaskTypeAndAgentModel(t *testing.T) {
	a := agent.NewAgent(agent.Definition{ID: "leader", ModelID: "fallback-model", ProviderID: "fallback-provider"}, &agenttest.FakeLLM{}, nil)
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })
	s := newRouteSession(t, "l2:s1", a, RouteResult{Level: "engineering"})
	stream, err := s.AskStream(desktopRequest("req-1"), "route me")
	if err != nil {
		t.Fatal(err)
	}
	route := routeFrameNow(t, s, "req-1")
	if route.TaskType != "engineering" || route.ModelID != "fallback-model" || route.ProviderID != "fallback-provider" {
		t.Fatalf("route frame = %+v", route)
	}
	if route.AgentInstanceID != a.InstanceID {
		t.Fatalf("agent_instance_id = %q, want %q", route.AgentInstanceID, a.InstanceID)
	}
	for range stream {
	}
}

func TestRouteFramePublishedBeforeQueuedActorStarts(t *testing.T) {
	a := agent.NewAgent(agent.Definition{ID: "leader", ModelID: "fallback"}, &agenttest.FakeLLM{Responses: []string{"ok"}}, nil)
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })
	started := make(chan struct{})
	release := make(chan struct{})
	if err := a.Submit(context.Background(), func(context.Context) error {
		close(started)
		<-release
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	<-started
	s := newRouteSession(t, "l2:queued", a, RouteResult{Level: "research"})
	stream, err := s.AskStream(desktopRequest("req-queued"), "queued")
	if err != nil {
		t.Fatal(err)
	}
	if route := routeFrameNow(t, s, "req-queued"); route.ModelID != "fallback" || route.TaskType != "research" {
		t.Fatalf("route before actor start = %+v", route)
	}
	close(release)
	for range stream {
	}
}

func TestRouteFrameUsesFreshGenerationOnRebuild(t *testing.T) {
	old := agent.NewAgent(agent.Definition{ID: "old", ModelID: "old-default"}, &agenttest.FakeLLM{}, nil)
	if err := old.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	old.Quarantine(errors.New("stuck"))
	fresh := agent.NewAgent(agent.Definition{ID: "fresh", ModelID: "fresh-default"}, &agenttest.FakeLLM{Responses: []string{"ok"}}, nil)
	if err := fresh.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fresh.Stop(time.Second) })
	s := newRouteSession(t, "l2:rebuild", old, RouteResult{Level: "engineering"})
	s.SetAgentRebuilder(func(context.Context) (*agent.Agent, error) { return fresh, nil })
	stream, err := s.AskStream(desktopRequest("req-rebuild"), "rebuild")
	if err != nil {
		t.Fatal(err)
	}
	if route := routeFrameNow(t, s, "req-rebuild"); route.AgentInstanceID != fresh.InstanceID || route.ModelID != "fresh-default" {
		t.Fatalf("rebuilt route frame = %+v", route)
	}
	for range stream {
	}
}
