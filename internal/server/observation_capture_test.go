package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/infra/db"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetry"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
	"github.com/xiaobaitu/soloqueue/internal/observability"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

// The deterministic provider reports exactly the instructions sent in this fixture.
// FakeLLM still supplies real streaming deltas through Session and Telemetry.
type capturedProvider struct {
	*agenttest.FakeLLM
	calls  int
	worker agent.LLMClient
}

func (f *capturedProvider) ChatStream(ctx context.Context, req agent.LLMRequest) (<-chan llm.Event, error) {
	f.calls++
	llm.ReportAttempt(ctx)
	text := "Captured instructions A <safe-text>"
	if f.calls == 3 {
		text = "Captured instructions B: changed"
	}
	llm.ReportInstructions(ctx, text)
	if f.calls == 1 && f.worker != nil {
		meta := telemetry.MetadataFromContext(ctx)
		meta.AgentID = "isolated-worker"
		stream, err := f.worker.ChatStream(telemetry.WithTelemetryMetadata(ctx, meta), agent.LLMRequest{Model: "fixture-worker", Messages: []agent.LLMMessage{{Role: "system", Content: "Captured instructions A <safe-text>"}}})
		if err != nil {
			return nil, err
		}
		for range stream {
		}
	}
	return f.FakeLLM.ChatStream(ctx, req)
}

func TestObservationSessionCaptureThroughREST(t *testing.T) {
	root := t.TempDir()
	if seed := os.Getenv("SOLOQUEUE_TRACE_CONTENT_SEED_DIR"); seed != "" {
		if filepath.Clean(seed) != "/private/tmp/soloqueue-trace-content/runtime" {
			t.Fatal("fixture path must be isolated task runtime")
		}
		root = seed
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
	}
	database, err := db.Open(filepath.Join(root, "soloqueue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := observability.ForWorkDir(root)
	for index, sid := range []string{"l1", "l2:11111111-1111-4111-8111-111111111111"} {
		t.Run(sid, func(t *testing.T) {
			dir := filepath.Join(root, "logs", "timelines", "default")
			if index == 1 {
				dir = filepath.Join(root, "logs", "timelines", "l2-11111111-1111-4111-8111-111111111111")
			}
			tw, err := timeline.NewWriter(dir, "timeline", 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer tw.Close()
			fake := &agenttest.FakeLLM{StreamDeltas: [][]string{{"Captured first output"}, {"Captured second output"}, {"Captured final answer"}}, ReasoningDeltasByTurn: [][]string{{"Captured first reasoning"}, {"Captured second reasoning"}, {"Captured final reasoning"}}, ToolCallDeltasByTurn: [][]llm.ToolCallDelta{{{Index: 0, ID: "echo1", Name: "echo", Arguments: "{}"}}, {{Index: 0, ID: "echo2", Name: "echo", Arguments: "{}"}}}}
			provider := &capturedProvider{FakeLLM: fake, worker: telemetry.NewTelemetryClient(&agenttest.FakeLLM{StreamDeltas: [][]string{{"Captured worker output"}}, ReasoningDeltasByTurn: [][]string{{"Captured worker reasoning"}}}, database, store)}
			a := agent.NewAgent(agent.Definition{ID: "fixture-main", SystemPrompt: "Captured original system message"}, telemetry.NewTelemetryClient(provider, database, store), nil, agent.WithTools(requestOwnershipEchoTool{}))
			if err := a.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			defer a.Stop(time.Second)
			cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
			cw.SetReplayMode(true)
			cw.Push(ctxwin.RoleSystem, "Captured original system message")
			cw.SetReplayMode(false)
			sess := session.NewSession(sid, "default", a, cw, tw, nil)
			defer sess.Close()
			req := "captured-" + fmt.Sprint(index)
			ctx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{SessionID: sid, RequestID: req})
			stream, err := sess.AskStream(ctx, "Inspect captured output and changing instructions")
			if err != nil {
				t.Fatal(err)
			}
			for range stream {
			}
			deadline := time.Now().Add(5 * time.Second)
			for {
				rows, err := database.ObservationCalls(context.Background(), sid, req)
				if err != nil {
					t.Fatal(err)
				}
				if len(rows) == 4 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("SQLite captured %d calls", len(rows))
				}
				time.Sleep(5 * time.Millisecond)
			}
			m := &Mux{workDir: root, sharedDB: database}
			read := func(cursor string, limit int) observability.Request {
				t.Helper()
				w := httptest.NewRecorder()
				m.handleObservationRequest(w, httptest.NewRequest("GET", "/?session_id="+url.QueryEscape(sid)+"&request_id="+req+"&cursor="+cursor+"&limit="+fmt.Sprint(limit), nil))
				var r observability.Request
				if w.Code != 200 {
					t.Fatal(w.Body.String())
				}
				if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(w.Body.String(), "prompt_hash") || strings.Contains(w.Body.String(), root) {
					t.Fatal("private snapshot identity exposed")
				}
				return r
			}
			got := read("", 100)
			if got.DurationSource != "request" || got.MetricsOnly || got.CallCount != 4 || len(got.Calls) != 4 {
				t.Fatalf("capture summary: %+v", got)
			}
			main := []observability.Call{}
			for _, c := range got.Calls {
				if !c.PromptAvailable || c.Content == "" || c.Reasoning == "" || c.MetricsOnly {
					t.Fatalf("capture lost content: %+v", c)
				}
				if c.AgentID == a.InstanceID {
					main = append(main, c)
				}
				w := httptest.NewRecorder()
				m.handleObservationPrompt(w, httptest.NewRequest("GET", "/?session_id="+url.QueryEscape(sid)+"&request_id="+req+"&call_id="+c.CallID, nil))
				if w.Code != 200 || !strings.Contains(w.Body.String(), "Captured instructions") {
					t.Fatalf("prompt inaccessible: %s", w.Body.String())
				}
			}
			if len(main) != 3 || main[0].PromptSourceCallID != "" || main[1].PromptSourceCallID != main[0].CallID || main[2].PromptSourceCallID != "" {
				t.Fatalf("A-A-B mismatch: %+v", main)
			}
			// Tools now share the span cursor; find the repeated LLM call
			// by stable identity rather than its former LLM-only offset.
			found := false
			cursor := ""
			for {
				page := read(cursor, 1)
				if len(page.Calls)+len(page.Tools) != 1 || page.DurationMS != got.DurationMS || page.CallCount != 4 {
					t.Fatalf("page changed summary: %+v", page)
				}
				for _, c := range page.Calls {
					if c.CallID == main[1].CallID {
						found = true
						if c.PromptSourceCallID != main[0].CallID {
							t.Fatalf("page changed prompt identity: %+v", c)
						}
					}
				}
				if page.NextCursor == "" {
					break
				}
				cursor = page.NextCursor
			}
			if !found {
				t.Fatal("paged LLM call missing")
			}
			paths, _ := filepath.Glob(filepath.Join(dir, "timeline-*.jsonl"))
			for _, p := range paths {
				body, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(body), "Captured original system message") || strings.Contains(string(body), "Captured instructions") {
					t.Fatal("system prompt leaked to replay timeline")
				}
			}
		})
	}
	// A second request deliberately has only old call metrics, matching the reported failure.
	start := time.Date(2026, 10, 26, 3, 48, 36, 865964000, time.UTC)
	for i := 0; i < 5; i++ {
		end := start.Add(time.Duration(i)*20*time.Second + 8445636*time.Microsecond)
		if err := database.InsertLLMCallMetric(context.Background(), db.LLMCallMetric{CallID: fmt.Sprintf("legacy-%d", i), SessionID: "l1", RequestID: "legacy-metrics", AgentID: "historical-agent", ProviderID: "historical", ModelID: "metrics-only", Status: "success", StartedAt: start.Add(time.Duration(i) * 20 * time.Second), FinishedAt: end, DurationMS: 8445}); err != nil {
			t.Fatal(err)
		}
	}
	tw, err := timeline.NewWriter(filepath.Join(root, "logs", "timelines", "default"), "timeline", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer tw.Close()
	if err := tw.AppendMessage(&timeline.MessagePayload{Role: "user", Content: "Inspect historical metrics-only trace", RequestID: "legacy-metrics"}); err != nil {
		t.Fatal(err)
	}
	if err := tw.AppendMessage(&timeline.MessagePayload{Role: "assistant", Content: "Historical answer; only call metrics remain for this turn.", RequestID: "legacy-metrics"}); err != nil {
		t.Fatal(err)
	}
	t.Logf("isolated capture fixture: %s (l1 captured-0 + legacy-metrics; L2 captured-1)", root)
}
