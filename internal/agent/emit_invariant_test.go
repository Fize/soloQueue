package agent

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/llm"
)

// A session's transcript is fed by (*Agent).emit alone. An event written to a
// request's output channel by any other path still reaches CLI and channel
// consumers, but the transcript never sees it, so the Web Console would
// silently miss it. These two tests keep emit the only way out.

// recordingSink records everything emit publishes to the transcript.
type recordingSink struct {
	mu     sync.Mutex
	owners []string
	events []iface.AgentEvent
}

func (s *recordingSink) PublishAgentEvent(owner string, ev iface.AgentEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.owners = append(s.owners, owner)
	s.events = append(s.events, ev)
}

func (s *recordingSink) snapshot() ([]string, []iface.AgentEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.owners...), append([]iface.AgentEvent(nil), s.events...)
}

func eventTypeNames[E any](events []E) []string {
	names := make([]string, len(events))
	for i, ev := range events {
		names[i] = fmt.Sprintf("%T", ev)
	}
	return names
}

// eventChannelParams returns the names of the parameters of fn that carry
// events out of the agent: any channel of AgentEvent that can be sent on.
func eventChannelParams(fn *ast.FuncDecl) map[string]bool {
	names := map[string]bool{}
	if fn.Type.Params == nil {
		return names
	}
	for _, field := range fn.Type.Params.List {
		channel, ok := field.Type.(*ast.ChanType)
		if !ok || channel.Dir&ast.SEND == 0 {
			continue
		}
		isEvent := false
		switch element := channel.Value.(type) {
		case *ast.Ident:
			isEvent = element.Name == "AgentEvent"
		case *ast.SelectorExpr:
			isEvent = element.Sel.Name == "AgentEvent"
		}
		if !isEvent {
			continue
		}
		for _, name := range field.Names {
			names[name.Name] = true
		}
	}
	return names
}

func TestRequestEventsAreSentOnlyThroughEmit(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	sawEmit := false
	checked := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			outputs := eventChannelParams(fn)
			if len(outputs) == 0 {
				continue
			}
			if fn.Name.Name == "emit" {
				sawEmit = true
				continue
			}
			checked++
			// Closures started by the function are part of its body.
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				send, ok := node.(*ast.SendStmt)
				if !ok {
					return true
				}
				if ident, ok := send.Chan.(*ast.Ident); ok && outputs[ident.Name] {
					t.Errorf("%s: %s sends on %q directly; route every event through (*Agent).emit so the session transcript sees it",
						fset.Position(send.Pos()), fn.Name.Name, ident.Name)
				}
				return true
			})
		}
	}
	// Guard against a refactor that renames the channel type and leaves this
	// test checking nothing.
	if !sawEmit {
		t.Fatal("(*Agent).emit not found: the invariant check is vacuous")
	}
	if checked < 6 {
		t.Fatalf("only %d functions take an event channel: the invariant check no longer sees the stream code", checked)
	}
}

func TestEveryDeliveredEventIsPublishedToTheTranscript(t *testing.T) {
	echo := newFakeTool("echo")
	echo.result = `{"ok":true}`
	cases := []struct {
		name string
		fake *agenttest.FakeLLM
		want []string // event types that must occur, so the case cannot pass vacuously
	}{
		{
			name: "tool loop with reasoning",
			fake: &agenttest.FakeLLM{
				ToolCallDeltasByTurn: [][]llm.ToolCallDelta{{{
					Index: 0, ID: "c1", Name: "echo", Arguments: `{"m":"hi"}`,
				}}},
				StreamDeltas:          [][]string{nil, {"fi", "nal"}},
				ReasoningDeltasByTurn: [][]string{{"think"}, {"more"}},
			},
			want: []string{"agent.ReasoningDeltaEvent", "agent.ContentDeltaEvent", "agent.ToolCallDeltaEvent", "agent.IterationDoneEvent", "agent.ToolExecStartEvent", "agent.ToolExecDoneEvent", "agent.DoneEvent"},
		},
		{
			name: "plain answer",
			fake: &agenttest.FakeLLM{StreamDeltas: [][]string{{"hello"}}},
			want: []string{"agent.ContentDeltaEvent", "agent.IterationDoneEvent", "agent.DoneEvent"},
		},
		{
			name: "model failure",
			fake: &agenttest.FakeLLM{Err: errors.New("model down")},
			want: []string{"agent.ErrorEvent"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := startedAgentWithTools(t, tc.fake, echo)
			sink := &recordingSink{}
			ctx := iface.ContextWithTranscriptSink(context.Background(), sink)
			stream, err := a.AskStream(ctx, "go")
			if err != nil {
				t.Fatal(err)
			}
			delivered := drainEvents(t, stream, 3*time.Second)
			owners, published := sink.snapshot()

			seen := map[string]bool{}
			for _, name := range eventTypeNames(delivered) {
				seen[name] = true
			}
			for _, name := range tc.want {
				if !seen[name] {
					t.Errorf("scenario did not produce %s; delivered %v", name, eventTypeNames(delivered))
				}
			}
			if len(published) != len(delivered) {
				t.Fatalf("transcript saw %d events but the consumer received %d\n published: %v\n delivered: %v",
					len(published), len(delivered), eventTypeNames(published), eventTypeNames(delivered))
			}
			for i := range delivered {
				if !reflect.DeepEqual(delivered[i], published[i]) {
					t.Errorf("event %d differs between the consumer and the transcript:\n delivered: %#v\n published: %#v", i, delivered[i], published[i])
				}
				if owners[i] != a.InstanceID {
					t.Errorf("event %d published for owner %q, want the emitting agent %q", i, owners[i], a.InstanceID)
				}
			}
		})
	}
}
