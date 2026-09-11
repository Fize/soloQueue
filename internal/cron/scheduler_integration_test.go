package cron_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
	"github.com/xiaobaitu/soloqueue/internal/cron"
	"github.com/xiaobaitu/soloqueue/internal/infra/db"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/runwatch"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

type toolStallLLM struct {
	calls     atomic.Int32
	failModel bool
	requests  chan agent.LLMRequest
}

func (*toolStallLLM) Chat(context.Context, agent.LLMRequest) (*agent.LLMResponse, error) {
	return nil, errors.New("unused")
}

func (m *toolStallLLM) ChatStream(ctx context.Context, req agent.LLMRequest) (<-chan llm.Event, error) {
	call := m.calls.Add(1)
	if call == 2 && m.failModel {
		<-ctx.Done()
		return nil, context.Cause(ctx)
	}
	name, args := "stalling_tool", `{}`
	if call > 1 {
		name, args = "SubmitCronResult", `{"content":"recovered"}`
		m.requests <- req
	}
	ch := make(chan llm.Event, 2)
	ch <- llm.Event{Type: llm.EventDelta, ToolCallDelta: &llm.ToolCallDelta{Index: 0, ID: fmt.Sprintf("call-%d", call), Name: name, Arguments: args}}
	ch <- llm.Event{Type: llm.EventDone, FinishReason: llm.FinishToolCalls}
	close(ch)
	return ch, nil
}

type stallingTool struct{}

func (stallingTool) Name() string                    { return "stalling_tool" }
func (stallingTool) Description() string             { return "wait for cancellation" }
func (stallingTool) Parameters() json.RawMessage     { return json.RawMessage(`{"type":"object"}`) }
func (stallingTool) PreferredTimeout() time.Duration { return 20 * time.Millisecond }
func (stallingTool) Execute(ctx context.Context, _ string) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

var _ tools.Tool = stallingTool{}

type schedulerSessionManager struct{ sess cron.Session }

func (m schedulerSessionManager) Session() cron.Session { return m.sess }
func (m schedulerSessionManager) GetSession(context.Context, string, string) (cron.Session, bool, func(), error) {
	return m.sess, false, nil, nil
}

func TestScheduler_CronRunRecoversToolAndModelWatchdogs(t *testing.T) {
	for _, failModel := range []bool{false, true} {
		t.Run(fmt.Sprint(failModel), func(t *testing.T) {
			sharedDB, err := db.Open(filepath.Join(t.TempDir(), "db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sharedDB.Close() })
			store := cron.NewDBStore(sharedDB)
			task, err := store.CreateTask(context.Background(), cron.CreateTaskInput{
				Title: "Watchdog integration", TaskType: cron.TaskTypeGeneral,
				Expression:  time.Now().Add(-time.Second).Format("2006-01-02 15:04:05"),
				Instruction: "wait", TargetAgent: "L1", NextRunAt: time.Now().Add(-time.Second),
			})
			if err != nil {
				t.Fatal(err)
			}
			watchdog := runwatch.NewManager(runwatch.Policy{ScanInterval: time.Millisecond, RootIdle: time.Second, TransportIdle: 30 * time.Millisecond})
			t.Cleanup(watchdog.Close)
			model := &toolStallLLM{failModel: failModel, requests: make(chan agent.LLMRequest, 1)}
			selected := []tools.Tool{stallingTool{}}
			for _, tool := range tools.BuildBase(tools.Config{}) {
				if tool.Name() == "SubmitCronResult" {
					selected = append(selected, tool)
				}
			}
			a := agent.NewAgent(agent.Definition{ID: "cron-agent"}, model, nil, agent.WithTools(selected...))
			if err := a.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = a.Stop(time.Second) })
			sess := session.NewSession("cron-session", "L1", a,
				ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer()), nil, nil)
			sess.SetRunWatch(watchdog)
			scheduler := cron.NewSchedulerForRetryTest(store, schedulerSessionManager{sess: sess})
			scheduler.SetWorkDir(t.TempDir())
			started := make(chan string, 1)
			completed := make(chan struct{}, 1)
			scheduler.OnTaskStart = func(_, _ string) { started <- "started" }
			scheduler.OnTaskComplete = func(string, string, bool, string) { completed <- struct{}{} }
			if err := scheduler.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(scheduler.Stop)

			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("scheduler did not start the one-time task")
			}
			select {
			case <-completed:
			case <-time.After(2 * time.Second):
				current, _ := store.GetTask(context.Background(), task.ID)
				records, _ := store.ListExecutionHistory(context.Background(), task.ID, 10, 0)
				t.Fatalf("typed watchdog failure did not complete Cron execution: task=%+v history=%+v", current, records)
			}
			var records []cron.ExecutionRecord
			deadline := time.Now().Add(time.Second)
			for {
				records, err = store.ListExecutionHistory(context.Background(), task.ID, 1, 0)
				if err == nil && len(records) == 1 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("history err=%v records=%v", err, records)
				}
				time.Sleep(time.Millisecond)
			}
			if records[0].Status != "success" || records[0].TerminalCode != "completed" {
				t.Fatalf("record=%+v", records[0])
			}
			wantCalls := int32(2)
			if failModel {
				wantCalls = 3
			}
			if model.calls.Load() != wantCalls {
				t.Fatalf("model calls=%d", model.calls.Load())
			}
			req := <-model.requests
			foundTask, foundTool, foundContinuation := false, false, false
			for _, msg := range req.Messages {
				if msg.Role == "user" && strings.Contains(msg.Content, "wait") {
					foundTask = true
				}
				if msg.Role == "tool" && strings.Contains(msg.Content, "tool timeout") {
					foundTool = true
				}
				if msg.Role == "user" && strings.Contains(msg.Content, "unknown outcome") {
					foundContinuation = true
				}
			}
			if !foundTask || !foundTool || (failModel && !foundContinuation) {
				t.Fatalf("continuation lost history: %+v", req.Messages)
			}
		})
	}
}

type fourthAttemptLLM struct {
	calls      atomic.Int32
	alwaysFail bool
}

func (*fourthAttemptLLM) Chat(context.Context, agent.LLMRequest) (*agent.LLMResponse, error) {
	return nil, errors.New("unused")
}
func (m *fourthAttemptLLM) ChatStream(context.Context, agent.LLMRequest) (<-chan llm.Event, error) {
	if m.calls.Add(1) <= 3 || m.alwaysFail {
		return nil, errors.New("transient provider failure")
	}
	ch := make(chan llm.Event, 2)
	ch <- llm.Event{Type: llm.EventDelta, ToolCallDelta: &llm.ToolCallDelta{Index: 0, ID: "final", Name: "SubmitCronResult", Arguments: `{"content":"recovered on fourth call"}`}}
	ch <- llm.Event{Type: llm.EventDone, FinishReason: llm.FinishToolCalls}
	close(ch)
	return ch, nil
}

func TestScheduler_CronFourthActualAttempt(t *testing.T) {
	for _, alwaysFail := range []bool{false, true} {
		t.Run(fmt.Sprint(alwaysFail), func(t *testing.T) {
			database, err := db.Open(filepath.Join(t.TempDir(), "db"))
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			store := cron.NewDBStore(database)
			task, err := store.CreateTask(context.Background(), cron.CreateTaskInput{Title: "fourth attempt", TaskType: cron.TaskTypeGeneral, Expression: time.Now().Add(-time.Second).Format("2006-01-02 15:04:05"), Instruction: "finish", TargetAgent: "L1", NextRunAt: time.Now().Add(-time.Second)})
			if err != nil {
				t.Fatal(err)
			}
			model := &fourthAttemptLLM{alwaysFail: alwaysFail}
			var selected []tools.Tool
			for _, tool := range tools.BuildBase(tools.Config{}) {
				if tool.Name() == "SubmitCronResult" {
					selected = append(selected, tool)
				}
			}
			a := agent.NewAgent(agent.Definition{ID: "fourth", NotifyChannel: "qq"}, model, nil, agent.WithTools(selected...))
			if err := a.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			defer a.Stop(time.Second)
			sess := session.NewSession("fourth-session", "L1", a, ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer()), nil, nil)
			var notifications atomic.Int32
			notified := make(chan struct{}, 4)
			sess.SetChannelSender("qq", func(context.Context, string) error { notifications.Add(1); notified <- struct{}{}; return nil })
			scheduler := cron.NewSchedulerForRetryTest(store, schedulerSessionManager{sess: sess})
			scheduler.SetWorkDir(t.TempDir())
			defer scheduler.Stop()
			var completions atomic.Int32
			scheduler.OnTaskComplete = func(string, string, bool, string) { completions.Add(1) }
			if err := scheduler.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			select {
			case <-notified:
			case <-time.After(3 * time.Second):
				t.Fatal("not notified")
			}
			scheduler.Stop()
			records, err := store.ListExecutionHistory(context.Background(), task.ID, 10, 0)
			if err != nil || len(records) != 1 {
				t.Fatalf("history=%v err=%v", records, err)
			}
			want := "success"
			if alwaysFail {
				want = "failed"
			}
			if model.calls.Load() != 4 || records[0].Status != want {
				t.Fatalf("actual LLM calls=%d; status=%s; error=%s", model.calls.Load(), records[0].Status, records[0].ErrorMessage)
			}
			if notifications.Load() != 1 || completions.Load() != 1 {
				t.Fatalf("notifications=%d completions=%d", notifications.Load(), completions.Load())
			}
		})
	}
}
