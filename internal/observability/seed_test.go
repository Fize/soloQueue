package observability_test

import (
	"encoding/json"
	"github.com/xiaobaitu/soloqueue/internal/dispatch"
	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
	. "github.com/xiaobaitu/soloqueue/internal/observability"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSeedObservation is an opt-in local UI fixture. It never writes a user's work directory.
func TestSeedObservation(t *testing.T) {
	root := os.Getenv("SOLOQUEUE_OBSERVABILITY_SEED_DIR")
	if root == "" {
		t.Skip("UI fixture not requested")
	}
	root = filepath.Clean(root)
	if !strings.HasPrefix(root, "/private/tmp/soloqueue-observability-workflow/") {
		t.Fatal("fixture must use isolated task tmp directory")
	}
	s := ForWorkDir(root)
	req := "smoke-observation"
	_ = s.Start("l1", req)
	paths, _ := filepath.Glob(filepath.Join(s.Root(), "requests", "*", "meta.json"))
	for _, path := range paths {
		body, _ := os.ReadFile(path)
		var meta map[string]any
		_ = json.Unmarshal(body, &meta)
		if meta["request_id"] == req {
			meta["started_at"] = time.Now().UTC().Add(-3 * time.Second)
			body, _ = json.Marshal(meta)
			_ = os.WriteFile(path, body, 0600)
		}
	}
	dm, e := dispatch.NewManager(filepath.Join(root, "logs", "timelines", "default"), "l1-session")
	if e != nil {
		t.Fatal(e)
	}
	d, e := dm.Begin(dispatch.BeginInput{TaskName: "smoke-child", Task: "Inspect the isolated fixture", Requester: "main", Executor: "child", RequestID: req, ToolCallID: "tool-1"})
	if e != nil {
		t.Fatal(e)
	}
	_ = dm.AssignExecutorInstance(d.Record.ID, "child-call")
	_ = dm.Append(d.Record.ID, "agent_event:agent.ToolExecStartEvent", map[string]any{"event": map[string]any{"CallID": "read-1", "Name": "read_file", "Args": "{}"}})
	_ = dm.Append(d.Record.ID, "agent_event:agent.ToolExecDoneEvent", map[string]any{"event": map[string]any{"CallID": "read-1", "Name": "read_file", "Result": "fixture content"}})
	_ = dm.Append(d.Record.ID, "agent_event:agent.DoneEvent", map[string]any{"done_content": "Complete child output"})
	_ = dm.Finish(d.Record.ID, dispatch.StatusCompleted, nil)
	for i, id := range []string{"main-call", "child-call"} {
		start := time.Now().UTC().Add(-time.Duration(3-i) * time.Second)
		end := start.Add(time.Second)
		c := Call{CallID: id, SessionID: "l1", RequestID: req, Status: "success", StartedAt: start, FinishedAt: &end, DurationMS: 1000, ProviderID: "test", ModelID: "fake-model", UsageType: "chat", AgentID: id, Content: "Complete call output", AttemptCount: 1, AttemptsReported: true, PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30}
		if i == 1 {
			c.DispatchID = d.Record.ID
			c.ToolCallID = "tool-1"
		}
		if e := s.SaveCall(c, &Prompt{RecordingStatus: "complete", Representation: "messages", Messages: []Instruction{{Role: "system", Content: "You are the isolated smoke-test agent."}}}); e != nil {
			t.Fatal(e)
		}
	}
	_ = s.Finish("l1", req, "completed")
	w, e := timeline.NewWriter(filepath.Join(root, "logs", "timelines", "default"), "timeline", 50<<20, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	if e = w.AppendMessage(&timeline.MessagePayload{Role: "user", Content: "Show execution details", RequestID: req}); e != nil {
		t.Fatal(e)
	}
	if e = w.AppendMessage(&timeline.MessagePayload{Role: "assistant", Content: "Observable answer", RequestID: req}); e != nil {
		t.Fatal(e)
	}
}
