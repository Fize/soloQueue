package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/dispatch"
	"github.com/xiaobaitu/soloqueue/internal/infra/db"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetry"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
	"github.com/xiaobaitu/soloqueue/internal/observability"
	"github.com/xiaobaitu/soloqueue/internal/session"
	"github.com/xiaobaitu/soloqueue/internal/storage"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestObservationRequestMergesMetricsAndScopesPrompt(t *testing.T) {
	root := t.TempDir()
	database, e := db.Open(filepath.Join(root, "db.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer database.Close()
	store := observability.ForWorkDir(root)
	now := time.Now().UTC()
	_ = store.Start("l1", "r")
	_ = store.SaveCall(observability.Call{CallID: "file", SessionID: "l1", RequestID: "r", StartedAt: now, Status: "success"}, &observability.Prompt{RecordingStatus: "complete", Representation: "messages", Messages: []observability.Instruction{{Role: "system", Content: "actual"}}})
	for _, id := range []string{"file", "metric-only"} {
		if e = database.InsertLLMCallMetric(context.Background(), db.LLMCallMetric{CallID: id, SessionID: "l1", RequestID: "r", StartedAt: now, FinishedAt: now, Status: "success"}); e != nil {
			t.Fatal(e)
		}
	}
	m := &Mux{workDir: root, sharedDB: database}
	w := httptest.NewRecorder()
	m.handleObservationRequest(w, httptest.NewRequest("GET", "/api/observability/request?session_id=l1&request_id=r&limit=1", nil))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var r struct {
		CallCount int                  `json:"call_count"`
		Calls     []observability.Call `json:"calls"`
		Next      string               `json:"next_cursor"`
	}
	if e = json.Unmarshal(w.Body.Bytes(), &r); e != nil || r.CallCount != 2 || len(r.Calls) != 1 || r.Next == "" {
		t.Fatalf("%s %v", w.Body.String(), e)
	}
	w = httptest.NewRecorder()
	m.handleObservationPrompt(w, httptest.NewRequest("GET", "/api/observability/prompt?session_id=l1&request_id=other&call_id=file", nil))
	if w.Code != 200 || contains(w.Body.String(), "actual") {
		t.Fatal("prompt crossed request boundary", w.Body.String())
	}
	w = httptest.NewRecorder()
	m.handleObservationPrompt(w, httptest.NewRequest("GET", "/api/observability/prompt?session_id=l1&request_id=r&call_id=..%2Fescape", nil))
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
func TestObservationRefusesPromptParentSymlink(t *testing.T) {
	root := t.TempDir()
	store := observability.ForWorkDir(root)
	_ = store.SaveCall(observability.Call{CallID: "call", SessionID: "l1", RequestID: "r", StartedAt: time.Now()}, &observability.Prompt{RecordingStatus: "complete", Messages: []observability.Instruction{{Role: "system", Content: "safe"}}})
	prompts := filepath.Join(store.Root(), "prompts")
	other := t.TempDir()
	entries, _ := os.ReadDir(prompts)
	for _, p := range entries {
		b, _ := os.ReadFile(filepath.Join(prompts, p.Name()))
		_ = os.WriteFile(filepath.Join(other, p.Name()), b, 0600)
	}
	_ = os.RemoveAll(prompts)
	_ = os.Symlink(other, prompts)
	m := &Mux{workDir: root}
	w := httptest.NewRecorder()
	m.handleObservationPrompt(w, httptest.NewRequest("GET", "/?session_id=l1&request_id=r&call_id=call", nil))
	if w.Code != 500 {
		t.Fatalf("accepted symlink: %d %s", w.Code, w.Body.String())
	}
}

func TestObservationDispatchUsesAuthorizedDirectoryAndRejectsUnsafeChildren(t *testing.T) {
	for _, mode := range []string{"duplicate", "stream_symlink", "state_symlink", "stream_identity", "state_identity"} {
		t.Run(mode, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			id := "dlg_same"
			owner := "l1-session"
			dir := filepath.Join(root, "logs", "timelines", "z-authorized", "delegations", id)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			rec := dispatch.Record{ID: id, OwnerSessionID: owner, RequestID: "authorized-request", Status: dispatch.StatusCompleted}
			write := func(path string, v any) {
				t.Helper()
				b, err := json.Marshal(v)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(path, b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join(dir, "meta.json"), rec)
			ev := dispatch.Event{Sequence: 1, Record: rec, Type: "authorized"}
			name := "stream-2026-01-01.jsonl"
			if strings.HasPrefix(mode, "state_") {
				name = "state.json"
			}
			if mode == "duplicate" {
				other := filepath.Join(root, "logs", "timelines", "a-other", "delegations", id)
				if err := os.MkdirAll(other, 0700); err != nil {
					t.Fatal(err)
				}
				bad := rec
				bad.OwnerSessionID = "l2-other-session"
				write(filepath.Join(other, "meta.json"), bad)
				write(filepath.Join(other, name), dispatch.Event{Sequence: 1, Record: bad, Type: "private-other"})
			}
			if strings.HasSuffix(mode, "identity") {
				ev.Record.RequestID = "wrong-request"
			}
			if strings.HasSuffix(mode, "symlink") {
				path := filepath.Join(t.TempDir(), name)
				write(path, ev)
				if err := os.Symlink(path, filepath.Join(dir, name)); err != nil {
					t.Fatal(err)
				}
			} else {
				write(filepath.Join(dir, name), ev)
			}
			m := &Mux{workDir: root}
			w := httptest.NewRecorder()
			m.handleObservationDispatch(w, httptest.NewRequest("GET", "/?session_id=l1&dispatch_id="+id, nil))
			if mode == "duplicate" {
				if w.Code != 200 || strings.Contains(w.Body.String(), "private-other") || !strings.Contains(w.Body.String(), `"type":"authorized"`) {
					t.Fatalf("wrong dispatch journal: %d %s", w.Code, w.Body.String())
				}
			} else if w.Code != 500 {
				t.Fatalf("unsafe child accepted: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestObservationReuseThroughRegistryAdmissionAndCleanup(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprint(expired), func(t *testing.T) {
			root := t.TempDir()
			store := observability.ForWorkDir(root)
			database, err := db.Open(filepath.Join(root, "db.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			started := make(chan struct{})
			release := make(chan struct{})
			var calls atomic.Int32
			fake := &agenttest.FakeLLM{Responses: []string{"old answer", "new answer"}, Hook: func(agent.LLMRequest) {
				if calls.Add(1) == 2 {
					close(started)
					<-release
				}
			}}
			a := agent.NewAgent(agent.Definition{ID: "main"}, telemetry.NewTelemetryClient(fake, database, store), nil)
			if err := a.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			tw, err := timeline.NewWriter(filepath.Join(root, "logs", "timelines", "default"), "timeline", 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			mgr := session.NewSessionManager(func(context.Context, string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
				return a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), tw, nil
			}, nil)
			defer mgr.Shutdown(time.Second)
			sess, err := mgr.Init(context.Background(), "default")
			if err != nil {
				t.Fatal(err)
			}
			m := &Mux{workDir: root, sharedDB: database, sessionMgr: mgr}
			h := NewHub(m)
			defer session.SetDefaultRequestLifecycleHooks(session.RequestLifecycleHooks{})
			ctx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{SessionID: "l1", RequestID: "reused"})
			first, err := sess.AskStream(ctx, "first")
			if err != nil {
				t.Fatal(err)
			}
			for range first {
			}
			firstDetail, err := store.Request("l1", "reused")
			if err != nil || firstDetail.CallCount != 1 {
				t.Fatalf("first: %#v %v", firstDetail, err)
			}
			if h.requests.ActiveCount() != 0 {
				t.Fatal("old execution not finalized")
			}
			if expired {
				units, err := store.RetentionUnits()
				if err != nil || len(units) != 1 {
					t.Fatal(err)
				}
				if err := store.Expire(units[0].ID); err != nil {
					t.Fatal(err)
				}
			}
			second, err := sess.AskStream(ctx, "second")
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				close(release)
				for range second {
				}
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("second execution blocked before provider")
			}
			if _, err := h.requests.Validate("l1", "reused"); err != nil {
				t.Fatal("reused request missing active admission", err)
			}
			p := config.DefaultStorageConfig()
			p.ExecutionDetails.MaxFiles = 1
			p.ExecutionDetails.RetentionDays = 1
			sm := storage.New(root, func() config.StorageConfig { return p }, database)
			defer sm.Close()
			preview, err := sm.Preview(p, storage.Revision(p), false)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = sm.Execute(preview.ID, preview.Revision, true); err != nil {
				t.Fatal(err)
			}
			units, err := store.RetentionUnits()
			if err != nil || len(units) != 1 || !units[0].Active {
				t.Fatalf("active generation unprotected: %#v %v", units, err)
			}
			w := httptest.NewRecorder()
			m.handleObservationRequest(w, httptest.NewRequest("GET", "/?session_id=l1&request_id=reused", nil))
			var detail observability.Request
			if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 || detail.Status != "running" || detail.RecordingStatus == "expired" || detail.CallCount != 1 || len(detail.Calls) != 1 || detail.Calls[0].CallID == firstDetail.Calls[0].CallID || detail.FinishedAt != nil {
				t.Fatalf("reused API mixed generations: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestObservationDispatchBoundedPaginationAndOversizedCheckpoint(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := "dlg_paged"
	dir := filepath.Join(root, "logs", "timelines", "default", "delegations", id)
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	record := dispatch.Record{ID: id, OwnerSessionID: "l1-session", RequestID: "r", Status: dispatch.StatusCompleted}
	data, _ := json.Marshal(record)
	os.WriteFile(filepath.Join(dir, "meta.json"), data, 0600)
	f, err := os.Create(filepath.Join(dir, "stream-2026-01-01.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 23; i++ {
		if err = json.NewEncoder(f).Encode(dispatch.Event{Sequence: uint64(i), Record: record, Type: "progress"}); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()
	m := &Mux{workDir: root}
	cursor := ""
	total := 0
	for {
		w := httptest.NewRecorder()
		m.handleObservationDispatch(w, httptest.NewRequest("GET", "/?session_id=l1&dispatch_id="+id+"&limit=7&cursor="+cursor, nil))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var page observationDispatch
		if err = json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		for _, event := range page.Events {
			total++
			if event.Sequence != uint64(total) {
				t.Fatal("pagination lost position")
			}
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if total != 23 {
		t.Fatal(total)
	}
	f, err = os.Create(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	f.Truncate(1 << 30)
	f.Close()
	w := httptest.NewRecorder()
	m.handleObservationDispatch(w, httptest.NewRequest("GET", "/?session_id=l1&dispatch_id="+id+"&limit=100", nil))
	if w.Code != 500 || !strings.Contains(w.Body.String(), "read limit") {
		t.Fatalf("oversized checkpoint must be explicit: %d %s", w.Code, w.Body.String())
	}
}

func TestObservationLegacyCallSpanBeforePagination(t *testing.T) {
	root := t.TempDir()
	database, err := db.Open(filepath.Join(root, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	start := time.Date(2026, 10, 26, 3, 48, 36, 865964000, time.UTC)
	for i := 0; i < 5; i++ {
		end := start.Add(time.Duration(i)*20*time.Second + 8445*time.Millisecond)
		if err := database.InsertLLMCallMetric(context.Background(), db.LLMCallMetric{CallID: fmt.Sprint(i), SessionID: "l2:11111111-1111-4111-8111-111111111111", RequestID: "legacy", StartedAt: start.Add(time.Duration(i) * 20 * time.Second), FinishedAt: end, Status: "success"}); err != nil {
			t.Fatal(err)
		}
	}
	m := &Mux{workDir: root, sharedDB: database}
	for _, cursor := range []string{"0", "4"} {
		w := httptest.NewRecorder()
		m.handleObservationRequest(w, httptest.NewRequest("GET", "/?session_id=l2:11111111-1111-4111-8111-111111111111&request_id=legacy&limit=1&cursor="+cursor, nil))
		var got map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got["duration_source"] != "call_span" || got["duration_ms"] != float64(88445) || got["attempts_reported"] != false || got["metrics_only"] != true {
			t.Fatalf("misleading historical summary: %s", w.Body.String())
		}
	}
}

func TestObservationExpiredSummaryRetainsAttemptsWithPartialMetrics(t *testing.T) {
	root := t.TempDir()
	database, err := db.Open(filepath.Join(root, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := observability.ForWorkDir(root)
	if err := store.Start("l1", "expired-subset"); err != nil {
		t.Fatal(err)
	}
	for i, attempts := range []int{3, 1, 1, 1, 1} {
		now := time.Now().UTC()
		call := observability.Call{CallID: fmt.Sprint(i), SessionID: "l1", RequestID: "expired-subset", StartedAt: now, FinishedAt: &now, Status: "success", AttemptCount: attempts, AttemptsReported: true}
		if err := store.SaveCall(call, nil); err != nil {
			t.Fatal(err)
		}
		if i == 1 || i == 2 {
			if err := database.InsertLLMCallMetric(context.Background(), db.LLMCallMetric{CallID: call.CallID, SessionID: call.SessionID, RequestID: call.RequestID, StartedAt: now, FinishedAt: now, Status: "success", AttemptCount: attempts, AttemptsReported: true}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := store.Finish("l1", "expired-subset", "completed"); err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(store.Root(), "requests", "*", "meta.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("request metadata: %v %v", paths, err)
	}
	if err := store.Expire(filepath.Base(filepath.Dir(paths[0]))); err != nil {
		t.Fatal(err)
	}
	m := &Mux{workDir: root, sharedDB: database}
	for _, cursor := range []string{"0", "1"} {
		w := httptest.NewRecorder()
		m.handleObservationRequest(w, httptest.NewRequest("GET", "/?session_id=l1&request_id=expired-subset&limit=1&cursor="+cursor, nil))
		var got observability.Request
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || got.CallCount != 5 || got.AttemptCount != 7 || got.AttemptsReported || got.TimingComplete || got.RecordingStatus != "expired" || len(got.Calls) != 1 || !got.Calls[0].AttemptsReported || got.Calls[0].AttemptCount != 1 || got.DurationSource != "request" {
			t.Fatalf("partial fallback lost retained total or invented coverage: %s", w.Body.String())
		}
	}
}

func TestObservationPagesToolsWithoutInflatingLLMCount(t *testing.T) {
	root := t.TempDir()
	store := observability.ForWorkDir(root)
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		if err := store.SaveCall(observability.Call{Kind: "tool", CallID: fmt.Sprintf("tool-%d", i), ToolCallID: "same", ToolName: "Bash", Arguments: `{"command":"printf test"}`, Content: "test", SessionID: "l1", RequestID: "r", StartedAt: now, Status: "success"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	m := &Mux{workDir: root}
	for _, q := range []string{"request_id=r&limit=2", "request_id=r&limit=2&cursor=2", "request_id=other"} {
		w := httptest.NewRecorder()
		m.handleObservationRequest(w, httptest.NewRequest("GET", "/api/observability/request?session_id=l1&"+q, nil))
		var r observability.Request
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &r) != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		if r.CallCount != 0 || len(r.Calls) != 0 {
			t.Fatalf("tools counted as LLM: %s", w.Body.String())
		}
		if strings.Contains(q, "other") {
			if len(r.Tools) != 0 {
				t.Fatal("crossed request")
			}
		} else if strings.Contains(q, "cursor") {
			if len(r.Tools) != 1 || r.NextCursor != "" {
				t.Fatal(w.Body.String())
			}
		} else if len(r.Tools) != 2 || r.NextCursor != "2" {
			t.Fatal(w.Body.String())
		}
	}
}
