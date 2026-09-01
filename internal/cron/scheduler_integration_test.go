package cron_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
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

type toolStallLLM struct{}

func (toolStallLLM) Chat(context.Context, agent.LLMRequest) (*agent.LLMResponse, error) {
	return nil, errors.New("unused")
}

func (toolStallLLM) ChatStream(context.Context, agent.LLMRequest) (<-chan llm.Event, error) {
	ch := make(chan llm.Event, 2)
	ch <- llm.Event{Type: llm.EventDelta, ToolCallDelta: &llm.ToolCallDelta{
		Index: 0, ID: "stall-call", Name: "stalling_tool", Arguments: `{}`,
	}}
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

func TestScheduler_CronRunPersistsTypedWatchdogHistory(t *testing.T) {
	sharedDB, err := db.Open(filepath.Join(t.TempDir(), "cron.db"))
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
	watchdog := runwatch.NewManager(runwatch.Policy{ScanInterval: time.Millisecond, RootIdle: time.Second})
	t.Cleanup(watchdog.Close)
	a := agent.NewAgent(agent.Definition{ID: "cron-agent"}, toolStallLLM{}, nil, agent.WithTools(stallingTool{}))
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })
	sess := session.NewSession("cron-session", "L1", a,
		ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer()), nil, nil)
	sess.SetRunWatch(watchdog)
	scheduler := cron.NewScheduler(store, schedulerSessionManager{sess: sess}, nil)
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
	if got := records[0].TerminalCode; got != string(runwatch.CodeToolStalled) {
		t.Fatalf("terminal_code = %q", got)
	}
}
