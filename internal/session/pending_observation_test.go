package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetry"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
	"github.com/xiaobaitu/soloqueue/internal/observability"
	"github.com/xiaobaitu/soloqueue/internal/runwatch"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSessionObservationLifecycleAndNoPromptInTimeline(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "logs", "timelines", "default")
	tw, e := timeline.NewWriter(dir, "timeline", 0, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer tw.Close()
	store := observability.ForWorkDir(root)
	client := telemetry.NewTelemetryClient(&agenttest.FakeLLM{Responses: []string{"answer"}}, nil, store)
	a := agent.NewAgent(agent.Definition{ID: "main", SystemPrompt: "actual-secret-free-instruction"}, client, nil)
	if e = a.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	defer a.Stop(time.Second)
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	cw.SetReplayMode(true)
	cw.Push(ctxwin.RoleSystem, "actual-secret-free-instruction")
	cw.SetReplayMode(false)
	s := NewSession("l1", "default", a, cw, tw, nil)
	defer s.Close()
	ctx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{SessionID: "l1", RequestID: "round"})
	out, e := s.AskStream(ctx, "hello")
	if e != nil {
		t.Fatal(e)
	}
	for range out {
	}
	r, e := store.Request("l1", "round")
	if e != nil || r.Status != "completed" || r.CallCount != 1 || r.Calls[0].AgentID != a.InstanceID {
		t.Fatalf("%#v %v", r, e)
	}
	p, e := store.CallPrompt("l1", "round", r.Calls[0].CallID)
	if e != nil || len(p.Messages) != 1 || p.Messages[0].Content != "actual-secret-free-instruction" {
		t.Fatalf("%#v %v", p, e)
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "timeline-*.jsonl"))
	for _, p := range paths {
		b, _ := os.ReadFile(p)
		if strings.Contains(string(b), "actual-secret-free-instruction") {
			t.Fatal("prompt entered timeline")
		}
	}
}
func TestSessionObservationPersistsRunningBeforeCompletion(t *testing.T) {
	root := t.TempDir()
	tw, e := timeline.NewWriter(filepath.Join(root, "logs", "timelines", "default"), "timeline", 0, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer tw.Close()
	started := make(chan struct{})
	release := make(chan struct{})
	var once atomic.Bool
	fake := &agenttest.FakeLLM{Responses: []string{"done"}, Hook: func(agent.LLMRequest) {
		if once.CompareAndSwap(false, true) {
			close(started)
			<-release
		}
	}}
	store := observability.ForWorkDir(root)
	a := agent.NewAgent(agent.Definition{ID: "main"}, telemetry.NewTelemetryClient(fake, nil, store), nil)
	_ = a.Start(context.Background())
	defer a.Stop(time.Second)
	s := NewSession("l1", "default", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), tw, nil)
	defer s.Close()
	ctx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{SessionID: "l1", RequestID: "round"})
	out, e := s.AskStream(ctx, "hello")
	if e != nil {
		t.Fatal(e)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("did not start")
	}
	r, e := store.Request("l1", "round")
	if e != nil || r.Status != "running" || r.CallCount != 1 {
		t.Fatalf("%#v %v", r, e)
	}
	close(release)
	for range out {
	}
	r, _ = store.Request("l1", "round")
	if r.Status != "completed" {
		t.Fatal(r.Status)
	}
}

func TestSessionObservationQueuedInputsShareCallWithoutDoubleCounting(t *testing.T) {
	root := t.TempDir()
	tw, e := timeline.NewWriter(filepath.Join(root, "logs", "timelines", "default"), "timeline", 0, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer tw.Close()
	store := observability.ForWorkDir(root)
	started := make(chan struct{})
	release := make(chan struct{})
	var once atomic.Bool
	fake := &agenttest.FakeLLM{ToolCallDeltasByTurn: [][]llm.ToolCallDelta{{{Index: 0, ID: "echo", Name: "echo", Arguments: "{}"}}}, StreamDeltas: [][]string{nil, {"shared reply"}}, Hook: func(agent.LLMRequest) {
		if once.CompareAndSwap(false, true) {
			close(started)
			<-release
		}
	}}
	a := agent.NewAgent(agent.Definition{ID: "main"}, telemetry.NewTelemetryClient(fake, nil, store), nil, agent.WithTools(syncEchoTool{}))
	_ = a.Start(context.Background())
	defer a.Stop(time.Second)
	s := NewSession("l1", "default", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), tw, nil)
	defer s.Close()
	ctx := func(id string) context.Context {
		return telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{SessionID: "l1", RequestID: id})
	}
	out, e := s.AskStream(ctx("first"), "first")
	if e != nil {
		t.Fatal(e)
	}
	<-started
	for _, id := range []string{"second", "third"} {
		if _, e = s.AskStream(ctx(id), id); !errors.Is(e, ErrQueued) {
			t.Fatalf("enqueue %s: %v", id, e)
		}
	}
	close(release)
	for range out {
	}
	r2, e := store.Request("l1", "second")
	if e != nil {
		t.Fatal(e)
	}
	r3, e := store.Request("l1", "third")
	if e != nil {
		t.Fatal(e)
	}
	if r2.Status != "completed" || r3.Status != "completed" || len(r2.Calls) != 1 || len(r3.Calls) != 1 || r2.Calls[0].CallID != r3.Calls[0].CallID {
		t.Fatalf("queued calls second=%#v third=%#v", r2, r3)
	}
}

func TestSessionObservationRemainsRunningAcrossAsyncDelegation(t *testing.T) {
	root := t.TempDir()
	tw, e := timeline.NewWriter(filepath.Join(root, "logs", "timelines", "default"), "timeline", 0, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer tw.Close()
	store := observability.ForWorkDir(root)
	target := &channelBlockingTarget{started: make(chan struct{}), release: make(chan struct{})}
	fake := &agenttest.FakeLLM{ToolCallDeltasByTurn: [][]llm.ToolCallDelta{{{Index: 0, ID: "delegate-call", Name: "delegate", Arguments: `{"task":"blocked"}`}}}, StreamDeltas: [][]string{nil, {"final"}}}
	a := agent.NewAgent(agent.Definition{ID: "main"}, telemetry.NewTelemetryClient(fake, nil, store), nil, agent.WithTools(&channelDelegationTool{target: target}), agent.WithPriorityMailbox())
	_ = a.Start(context.Background())
	defer a.Stop(time.Second)
	s := NewSession("l1", "default", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), tw, nil)
	defer s.Close()
	ctx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{SessionID: "l1", RequestID: "async"})
	out, e := s.AskStream(ctx, "delegate")
	if e != nil {
		t.Fatal(e)
	}
	select {
	case <-target.started:
	case <-time.After(time.Second):
		t.Fatal("delegation did not start")
	}
	r, e := store.Request("l1", "async")
	if e != nil || r.Status != "running" || r.CallCount != 1 {
		t.Fatalf("premature completion %#v %v", r, e)
	}
	if e = store.Expire(keyForObservationTest("l1", "async")); e != observability.ErrActive {
		t.Fatalf("active expiry %v", e)
	}
	close(target.release)
	for range out {
	}
	r, e = store.Request("l1", "async")
	if e != nil || r.Status != "completed" || r.CallCount != 2 {
		t.Fatalf("%#v %v", r, e)
	}
}
func keyForObservationTest(session, request string) string {
	h := sha256.Sum256([]byte(session + "\x00" + request))
	return hex.EncodeToString(h[:])
}

func TestAskIsolatedObservationSynchronousFailuresFinish(t *testing.T) {
	for _, kind := range []string{"no_agent", "begin_failed", "tracked_failed"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			tw, e := timeline.NewWriter(filepath.Join(root, "logs", "timelines", "default"), "timeline", 0, 0)
			if e != nil {
				t.Fatal(e)
			}
			defer tw.Close()
			var a *agent.Agent
			if kind != "no_agent" {
				a = agent.NewAgent(agent.Definition{ID: "main"}, &agenttest.FakeLLM{}, nil)
			}
			s := NewSession("l1", "default", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), tw, nil)
			defer s.Close()
			if kind == "begin_failed" {
				watch := runwatch.NewManager(runwatch.DefaultPolicy())
				watch.Close()
				s.SetRunWatch(watch)
			}
			ctx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{SessionID: "l1", RequestID: kind})
			if _, e = s.AskIsolated(ctx, "test"); e == nil {
				t.Fatal("expected synchronous error")
			}
			store := observability.ForWorkDir(root)
			r, e := store.Request("l1", kind)
			if e != nil || r.Status != "error" || r.FinishedAt == nil {
				t.Fatalf("%#v %v", r, e)
			}
			units, e := store.RetentionUnits()
			if e != nil || len(units) != 1 || units[0].Active {
				t.Fatalf("%#v %v", units, e)
			}
		})
	}
}

func TestSessionObservationReusedIDProtectedDuringNewExecution(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprint(expired), func(t *testing.T) {
			root := t.TempDir()
			store := observability.ForWorkDir(root)
			_ = store.Start("l1", "reused")
			_ = store.SaveCall(observability.Call{CallID: "previous", SessionID: "l1", RequestID: "reused", StartedAt: time.Now().Add(-time.Hour), Status: "success", AttemptCount: 3}, nil)
			_ = store.Finish("l1", "reused", "completed")
			if expired {
				if err := store.Expire(keyForObservationTest("l1", "reused")); err != nil {
					t.Fatal(err)
				}
			}
			tw, err := timeline.NewWriter(filepath.Join(root, "logs", "timelines", "default"), "timeline", 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer tw.Close()
			started := make(chan struct{})
			release := make(chan struct{})
			var once atomic.Bool
			fake := &agenttest.FakeLLM{Responses: []string{"done"}, Hook: func(agent.LLMRequest) {
				if once.CompareAndSwap(false, true) {
					close(started)
					<-release
				}
			}}
			a := agent.NewAgent(agent.Definition{ID: "main"}, telemetry.NewTelemetryClient(fake, nil, store), nil)
			if err := a.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			defer a.Stop(time.Second)
			s := NewSession("l1", "default", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), tw, nil)
			defer s.Close()
			ctx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{SessionID: "l1", RequestID: "reused"})
			out, err := s.AskStream(ctx, "new execution")
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				close(release)
				for range out {
				}
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("execution did not start")
			}
			r, err := store.Request("l1", "reused")
			if err != nil || r.Status != "running" || r.FinishedAt != nil || r.CallCount != 1 || len(r.Calls) != 1 || r.Calls[0].CallID == "previous" || r.AttemptCount != 0 {
				t.Errorf("old generation exposed: %#v %v", r, err)
			}
			units, err := store.RetentionUnits()
			if err != nil || len(units) != 1 || !units[0].Active {
				t.Errorf("new execution unprotected: %#v %v", units, err)
			}
			if err := store.Expire(keyForObservationTest("l1", "reused")); !errors.Is(err, observability.ErrActive) {
				t.Errorf("cleanup accepted active reuse: %v", err)
			}
		})
	}
}
