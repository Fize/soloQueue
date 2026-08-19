package cron

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/db"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

// mockSession implements the Session interface for testing.
type mockSession struct {
	idle             bool
	idleFn           func() bool
	queued           []string
	askStreamFn      func(ctx context.Context, prompt string) (<-chan iface.AgentEvent, error)
	modelParams      *iface.ModelOverrideParams
	hasNotifyChannel bool
	sendViaChannelFn func(ctx context.Context, text string) error
}

func (m *mockSession) Idle() bool {
	if m.idleFn != nil {
		return m.idleFn()
	}
	return m.idle
}
func (m *mockSession) QueueMessage(prompt string) { m.queued = append(m.queued, prompt) }
func (m *mockSession) AskStream(ctx context.Context, prompt string) (<-chan iface.AgentEvent, error) {
	if m.askStreamFn != nil {
		return m.askStreamFn(ctx, prompt)
	}
	ch := make(chan iface.AgentEvent)
	close(ch)
	return ch, nil
}
func (m *mockSession) AskIsolated(ctx context.Context, prompt string) (<-chan iface.AgentEvent, error) {
	return m.AskStream(ctx, prompt)
}
func (m *mockSession) AskIsolatedWithModel(ctx context.Context, prompt string, params *iface.ModelOverrideParams) (<-chan iface.AgentEvent, error) {
	m.modelParams = params
	return m.AskStream(ctx, prompt)
}
func (m *mockSession) AskStreamWithModel(ctx context.Context, prompt string, params *iface.ModelOverrideParams) (<-chan iface.AgentEvent, error) {
	m.modelParams = params
	return m.AskStream(ctx, prompt)
}
func (m *mockSession) SendViaChannel(ctx context.Context, text string) error {
	if m.sendViaChannelFn != nil {
		return m.sendViaChannelFn(ctx, text)
	}
	return nil
}
func (m *mockSession) SendMediaViaChannel(context.Context, []channel.OutboundMedia) error { return nil }
func (m *mockSession) HasNotifyChannel() bool                                             { return m.hasNotifyChannel }

type mockSessionManager struct {
	session    Session
	getSession func(ctx context.Context, teamID, taskID string) (Session, bool, func(), error)
}

func (m *mockSessionManager) Session() Session { return m.session }
func (m *mockSessionManager) GetSession(ctx context.Context, teamID, taskID string) (Session, bool, func(), error) {
	if m.getSession != nil {
		return m.getSession(ctx, teamID, taskID)
	}
	return nil, false, nil, nil
}

func newTestScheduler(t *testing.T) *Scheduler {
	t.Helper()
	log, err := logger.System(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	t.Cleanup(func() { log.Close() })
	return NewScheduler(nil, &mockSessionManager{}, log)
}

func TestDeliverL2ResultViaChannel_UsesL2WhenConfigured(t *testing.T) {
	s := newTestScheduler(t)
	var l2Messages, l1Messages []string
	l1 := &mockSession{sendViaChannelFn: func(_ context.Context, text string) error {
		l1Messages = append(l1Messages, text)
		return nil
	}}
	l2 := &mockSession{
		hasNotifyChannel: true,
		sendViaChannelFn: func(_ context.Context, text string) error {
			l2Messages = append(l2Messages, text)
			return nil
		},
	}
	s.sessionMgr = &mockSessionManager{session: l1}

	s.deliverL2ResultViaChannel(context.Background(), Task{ID: "task-l2"}, l2, "completed")

	if len(l2Messages) != 1 || l2Messages[0] != "completed" {
		t.Fatalf("L2 messages = %v, want [completed]", l2Messages)
	}
	if len(l1Messages) != 0 {
		t.Fatalf("L1 messages = %v, want none", l1Messages)
	}
}

func TestDeliverL2ResultViaChannel_FallsBackToL1OnlyWithoutL2Channel(t *testing.T) {
	s := newTestScheduler(t)
	var l2Messages, l1Messages []string
	l1 := &mockSession{sendViaChannelFn: func(_ context.Context, text string) error {
		l1Messages = append(l1Messages, text)
		return nil
	}}
	l2 := &mockSession{sendViaChannelFn: func(_ context.Context, text string) error {
		l2Messages = append(l2Messages, text)
		return nil
	}}
	s.sessionMgr = &mockSessionManager{session: l1}

	s.deliverL2ResultViaChannel(context.Background(), Task{ID: "task-fallback"}, l2, "completed")

	if len(l2Messages) != 0 {
		t.Fatalf("L2 messages = %v, want none", l2Messages)
	}
	if len(l1Messages) != 1 || l1Messages[0] != "completed" {
		t.Fatalf("L1 messages = %v, want [completed]", l1Messages)
	}
}

func TestDeliverL2ResultViaChannel_DoesNotFallbackWhenConfiguredSenderFails(t *testing.T) {
	s := newTestScheduler(t)
	var l1Messages []string
	l1 := &mockSession{sendViaChannelFn: func(_ context.Context, text string) error {
		l1Messages = append(l1Messages, text)
		return nil
	}}
	l2 := &mockSession{
		hasNotifyChannel: true,
		sendViaChannelFn: func(context.Context, string) error {
			return errors.New("channel unavailable")
		},
	}
	s.sessionMgr = &mockSessionManager{session: l1}

	s.deliverL2ResultViaChannel(context.Background(), Task{ID: "task-send-failure"}, l2, "completed")

	if len(l1Messages) != 0 {
		t.Fatalf("L1 messages = %v, want none", l1Messages)
	}
}

func TestNextTrigger(t *testing.T) {
	localZone := time.Local
	now := time.Date(2026, 5, 24, 10, 0, 0, 0, localZone)

	tests := []struct {
		expr     string
		from     time.Time
		want     time.Time
		wantOne  bool
		hasError bool
	}{
		{expr: "2026-05-24 15:30:00", from: now, want: time.Date(2026, 5, 24, 15, 30, 0, 0, localZone), wantOne: true},
		{expr: "2026-05-25", from: now, want: time.Date(2026, 5, 25, 0, 0, 0, 0, localZone), wantOne: true},
		{expr: "daily", from: now, want: time.Date(2026, 5, 25, 0, 0, 0, 0, localZone), wantOne: false},
		{expr: "0 8 * * 1", from: now, want: time.Date(2026, 5, 25, 8, 0, 0, 0, localZone), wantOne: false},
		{expr: "invalid expression", from: now, hasError: true},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			got, err := NextTrigger(tt.expr, tt.from)
			if (err != nil) != tt.hasError {
				t.Fatalf("NextTrigger() error = %v, hasError = %v", err, tt.hasError)
			}
			if !tt.hasError {
				if !got.Equal(tt.want) {
					t.Errorf("NextTrigger() got = %v, want = %v", got, tt.want)
				}
				if IsOneTimeExpression(tt.expr) != tt.wantOne {
					t.Errorf("IsOneTimeExpression() mismatch")
				}
			}
		})
	}
}

func TestValidateTaskTypeSupportsThreeTypes(t *testing.T) {
	for _, taskType := range []string{"general", "engineering", "research"} {
		if err := ValidateTaskType(taskType); err != nil {
			t.Errorf("ValidateTaskType(%q): %v", taskType, err)
		}
	}
	if err := ValidateTaskType("other"); err == nil {
		t.Fatal("ValidateTaskType accepted unsupported type")
	}
}

func TestIsL1Target(t *testing.T) {
	tests := []struct {
		task Task
		want bool
	}{
		{Task{TargetAgent: "L1"}, true},
		{Task{TargetAgent: "l1"}, true},
		{Task{TargetAgent: ""}, true},
		{Task{TargetAgent: "engineering"}, false},
		{Task{TargetAgent: "L2"}, false},
	}
	for _, tt := range tests {
		if got := isL1Target(tt.task); got != tt.want {
			t.Errorf("isL1Target(%q) = %v, want %v", tt.task.TargetAgent, got, tt.want)
		}
	}
}

func TestClaimOneTimeRunDeduplicatesSameSchedule(t *testing.T) {
	s := newTestScheduler(t)
	task := Task{
		ID:         "one-time-task",
		Expression: "2026-07-22 19:21:00",
		NextRunAt:  time.Date(2026, 7, 22, 19, 21, 0, 0, time.Local),
	}
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.claimOneTimeRun(task) {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := accepted.Load(); got != 1 {
		t.Fatalf("accepted triggers = %d, want exactly 1", got)
	}

	task.NextRunAt = task.NextRunAt.Add(time.Minute)
	if !s.claimOneTimeRun(task) {
		t.Fatal("updated scheduled instant should be accepted")
	}
}

func TestL1QueueExecutesDeferredTaskOnlyOnce(t *testing.T) {
	store := openTestDB(t)
	task, err := store.CreateTask(context.Background(), CreateTaskInput{
		Title:       "Deferred L1 task",
		TaskType:    "general",
		Expression:  "0 19 * * *",
		Instruction: "run once after L1 is idle",
		TargetAgent: "L1",
		NextRunAt:   time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	var idle atomic.Bool
	var asks atomic.Int32
	completed := make(chan struct{}, 1)
	session := &mockSession{
		idleFn: idle.Load,
		askStreamFn: func(context.Context, string) (<-chan iface.AgentEvent, error) {
			if asks.Add(1) == 1 {
				completed <- struct{}{}
			}
			ch := make(chan iface.AgentEvent)
			close(ch)
			return ch, nil
		},
	}
	s := NewScheduler(store, &mockSessionManager{session: session}, nil)
	s.SetWorkDir(t.TempDir())
	go s.l1QueueLoop()
	t.Cleanup(s.Stop)

	// A busy L1 queues the task instead of running it immediately.
	s.executeTask(*task)
	deadline := time.Now().Add(time.Second)
	for {
		s.l1Mu.Lock()
		queued := len(s.l1Queue)
		s.l1Mu.Unlock()
		if queued == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("deferred task was not queued")
		}
		time.Sleep(time.Millisecond)
	}
	if got := asks.Load(); got != 0 {
		t.Fatalf("Ask count while L1 is busy = %d, want 0", got)
	}

	idle.Store(true)
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("queued task did not execute after L1 became idle")
	}

	// The queue entry must be removed after it is consumed, so it cannot run again.
	time.Sleep(25 * time.Millisecond)
	if got := asks.Load(); got != 1 {
		t.Fatalf("Ask count after deferred task completed = %d, want 1", got)
	}
	s.l1Mu.Lock()
	queued := len(s.l1Queue)
	s.l1Mu.Unlock()
	if queued != 0 {
		t.Fatalf("L1 queue length after execution = %d, want 0", queued)
	}
}

func TestDrainEvents(t *testing.T) {
	ch := make(chan iface.AgentEvent)
	close(ch)
	content, media := drainEvents(ch)
	if content != "" {
		t.Errorf("drainEvents on empty channel got content = %q", content)
	}
	if len(media) != 0 {
		t.Errorf("drainEvents on empty channel got %d media", len(media))
	}
}

func TestBuildCronPrompt(t *testing.T) {
	task := Task{ID: "task-1", Title: "Health check", Instruction: "Check health status"}
	prompt := buildCronPrompt(task)
	for _, want := range []string{
		"<TASK_INSTRUCTION>\nCheck health status\n</TASK_INSTRUCTION>",
		"<FINAL_OUTPUT_CONTRACT>",
		"Call SubmitCronResult exactly once",
		"only tool call in the final model response",
		"Do not put report content in ordinary assistant text",
		"Escape double quotes, newlines, and backslashes",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("buildCronPrompt missing %q:\n%s", want, prompt)
		}
	}
	for _, forbidden := range []string{`"version"`, `"status"`, `"task_id"`, `"error"`, "previous report"} {
		if strings.Contains(prompt, forbidden) {
			t.Errorf("buildCronPrompt must not ask the model for %q:\n%s", forbidden, prompt)
		}
	}
	if !strings.Contains(continuationPrompt, "SubmitCronResult") {
		t.Fatalf("retry continuation prompt must retain final output contract: %s", continuationPrompt)
	}
}

func TestNormalizeCronResult(t *testing.T) {
	task := Task{ID: "task-1", Title: "Daily report"}
	now := time.Date(2026, 8, 19, 15, 30, 0, 0, time.Local)

	t.Run("valid strict payload becomes success", func(t *testing.T) {
		result, err := normalizeCronResult(task, "run-1", `{"summary":"Ready","sections":[{"title":"Details","content":"All systems green."}]}`, now)
		if err != nil {
			t.Fatalf("normalizeCronResult returned error: %v", err)
		}
		if result.Version != "v1" || result.TaskID != task.ID || result.RunID != "run-1" || result.Title != task.Title {
			t.Fatalf("project-owned fields not populated: %+v", result)
		}
		if result.Status != "success" || result.Error != nil || result.Summary != "Ready" || len(result.Sections) != 1 {
			t.Fatalf("unexpected success result: %+v", result)
		}
		if result.GeneratedAt != now.Format(time.RFC3339) {
			t.Fatalf("GeneratedAt = %q, want %q", result.GeneratedAt, now.Format(time.RFC3339))
		}
	})

	t.Run("malformed non-empty payload becomes safe partial", func(t *testing.T) {
		raw := `{"summary":"完成","sections":[{"title":"纪律自评","content":"午间即时降级"震荡市下沿、全面防守"，执行到位"}]}`
		result, err := normalizeCronResult(task, "run-2", raw, now)
		if err == nil || !strings.Contains(err.Error(), "decode final output") {
			t.Fatalf("expected strict parse error, got %v", err)
		}
		if result.Status != "partial" || result.Summary == "" || result.Summary == raw || len(result.Sections) != 0 || result.Error == nil || !strings.HasPrefix(*result.Error, "invalid_structured_output:") {
			t.Fatalf("unexpected partial result: %+v", result)
		}
		formatted := formatCronResult(result)
		for _, forbidden := range []string{raw, "震荡市下沿、全面防守", "invalid_structured_output", "decode final output"} {
			if strings.Contains(formatted, forbidden) {
				t.Fatalf("private malformed output or diagnostic leaked into formatted result: %q", formatted)
			}
		}
		if !strings.Contains(formatted, "[部分完成] Daily report") || !strings.Contains(formatted, result.Summary) {
			t.Fatalf("safe partial wrapper missing: %q", formatted)
		}
	})

	t.Run("empty final output becomes failed", func(t *testing.T) {
		result, err := normalizeCronResult(task, "run-3", "  \n", now)
		if err == nil || result.Status != "failed" || result.Error == nil || !strings.HasPrefix(*result.Error, "empty_structured_output:") {
			t.Fatalf("unexpected empty-output result=%+v err=%v", result, err)
		}
	})
}

func TestParseSendFileMedia(t *testing.T) {
	raw := `{"status":"success","file_type":"image","file_name":"test.png","url":"https://example.com/img.png"}`
	result := parseSendFileMedia(raw)
	if result == nil {
		t.Fatal("parseSendFileMedia returned nil")
	}
	if result.Kind != channel.MediaImage {
		t.Errorf("Kind = %q, want image", result.Kind)
	}

	if parseSendFileMedia("not json") != nil {
		t.Error("parseSendFileMedia should return nil for invalid JSON")
	}
}

func TestBuildTaskPrompt(t *testing.T) {
	s := newTestScheduler(t)

	task := Task{ID: "t1", Instruction: "Do something important"}
	prompt := s.buildTaskPrompt(task)
	if prompt == "" {
		t.Error("buildTaskPrompt returned empty string")
	}
	if strings.Contains(prompt, "<recalled_memories>") {
		t.Error("buildTaskPrompt must not inject recalled memories")
	}
}

func TestBuildCronContext(t *testing.T) {
	s := newTestScheduler(t)
	task := Task{ID: "t1"}
	ctx := s.buildCronContext(task, "run-1")
	if ctx == nil {
		t.Error("buildCronContext returned nil")
	}
}

func TestScheduler_NewAndInit(t *testing.T) {
	s := newTestScheduler(t)
	if s == nil {
		t.Fatal("NewScheduler returned nil")
	}
	if s.l1Cond == nil {
		t.Error("cond not initialized")
	}
	if s.entries == nil || s.timers == nil {
		t.Error("maps not initialized")
	}
}

func TestSchedulerAskWithTaskModel(t *testing.T) {
	s := newTestScheduler(t)
	s.SetModelResolver(func(taskType string) (ResolvedModel, error) {
		return ResolvedModel{
			Params:            iface.ModelOverrideParams{ModelID: "engineering-model", ProviderID: "p", TaskType: taskType},
			RequestedTaskType: "engineering",
		}, nil
	})
	sess := &mockSession{}
	task := Task{ID: "t1", Title: "Health check", TaskType: "engineering", Instruction: "check"}
	_, ch, err := s.askWithTaskModel(context.Background(), task, sess)
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	if sess.modelParams == nil || sess.modelParams.ModelID != "engineering-model" || sess.modelParams.TaskType != "engineering" {
		t.Fatalf("unexpected model params: %+v", sess.modelParams)
	}
}

func openTestDB(t *testing.T) *DBStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := db.Open(path)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewDBStore(db)
}

func TestRecordAndListExecutionHistory(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()
	now := time.Now()

	// Record some executions.
	rec1 := ExecutionRecord{ID: "e1", TaskID: "t1", ExecutedAt: now.Add(-2 * time.Hour), CompletedAt: now.Add(-2 * time.Hour).Add(5 * time.Second), DurationMs: 5000, Status: "success", ResultSummary: "All good 1", TaskType: "general", TargetAgent: "L1", ModelID: "m1", ProviderID: "p1", TimelineDir: "logs/cron/t1/e1"}
	rec2 := ExecutionRecord{ID: "e2", TaskID: "t1", ExecutedAt: now.Add(-1 * time.Hour), CompletedAt: now.Add(-1 * time.Hour).Add(3 * time.Second), DurationMs: 3000, Status: "failed", ResultSummary: "", ErrorMessage: "timeout", TaskType: "general", TargetAgent: "L1", ModelID: "m2", ProviderID: "p1", TimelineDir: "logs/cron/t1/e2"}
	rec3 := ExecutionRecord{ID: "e3", TaskID: "t1", ExecutedAt: now, CompletedAt: now.Add(7 * time.Second), DurationMs: 7000, Status: "panic", ResultSummary: "", ErrorMessage: "panic: nil pointer", TaskType: "general", TargetAgent: "L1", TimelineDir: "logs/cron/t1/e3"}

	if err := store.RecordExecution(ctx, rec1); err != nil {
		t.Fatalf("RecordExecution 1: %v", err)
	}
	if err := store.RecordExecution(ctx, rec2); err != nil {
		t.Fatalf("RecordExecution 2: %v", err)
	}
	if err := store.RecordExecution(ctx, rec3); err != nil {
		t.Fatalf("RecordExecution 3: %v", err)
	}

	// List all 3 (newest first).
	records, err := store.ListExecutionHistory(ctx, "t1", 10, 0)
	if err != nil {
		t.Fatalf("ListExecutionHistory: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}
	// Newest first: e3, e2, e1.
	if records[0].ID != "e3" || records[1].ID != "e2" || records[2].ID != "e1" {
		t.Errorf("unexpected order: %v", records)
	}
	if records[0].Status != "panic" {
		t.Errorf("expected panic status, got %s", records[0].Status)
	}
	if records[0].ErrorMessage != "panic: nil pointer" {
		t.Errorf("expected error message, got %s", records[0].ErrorMessage)
	}

	// Pagination: limit 2, offset 0.
	records, err = store.ListExecutionHistory(ctx, "t1", 2, 0)
	if err != nil {
		t.Fatalf("ListExecutionHistory page 1: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0].ID != "e3" || records[1].ID != "e2" {
		t.Errorf("unexpected page 1: %v", records)
	}

	// Pagination: limit 2, offset 2.
	records, err = store.ListExecutionHistory(ctx, "t1", 2, 2)
	if err != nil {
		t.Fatalf("ListExecutionHistory page 2: %v", err)
	}
	if len(records) != 1 || records[0].ID != "e1" {
		t.Errorf("unexpected page 2: %v", records)
	}

	// Empty list for unknown task.
	records, err = store.ListExecutionHistory(ctx, "unknown", 10, 0)
	if err != nil {
		t.Fatalf("ListExecutionHistory unknown: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records for unknown task, got %d", len(records))
	}
}

func TestGetExecutionHistory(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()
	now := time.Now()

	rec := ExecutionRecord{ID: "e1", TaskID: "t1", ExecutedAt: now, CompletedAt: now.Add(time.Second), DurationMs: 1000, Status: "success", ResultSummary: "done", TaskType: "general", TargetAgent: "L1", TimelineDir: "logs/cron/t1/e1"}
	if err := store.RecordExecution(ctx, rec); err != nil {
		t.Fatalf("RecordExecution: %v", err)
	}

	got, err := store.GetExecutionHistory(ctx, "t1", "e1")
	if err != nil {
		t.Fatalf("GetExecutionHistory: %v", err)
	}
	if got.ID != "e1" || got.Status != "success" || got.ResultSummary != "done" {
		t.Errorf("unexpected record: %+v", got)
	}

	// Not found.
	_, err = store.GetExecutionHistory(ctx, "t1", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent record")
	}
}

func TestDeleteExecutionHistory(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()
	now := time.Now()

	rec := ExecutionRecord{ID: "e1", TaskID: "t1", ExecutedAt: now, CompletedAt: now.Add(time.Second), DurationMs: 1000, Status: "success", ResultSummary: "done", TaskType: "general", TargetAgent: "L1", TimelineDir: "logs/cron/t1/e1"}
	store.RecordExecution(ctx, rec)
	rec2 := ExecutionRecord{ID: "e2", TaskID: "t2", ExecutedAt: now, CompletedAt: now.Add(time.Second), DurationMs: 1000, Status: "success", ResultSummary: "done", TaskType: "general", TargetAgent: "L1", TimelineDir: "logs/cron/t2/e2"}
	store.RecordExecution(ctx, rec2)

	// Delete history for t1 only.
	if err := store.DeleteExecutionHistory(ctx, "t1"); err != nil {
		t.Fatalf("DeleteExecutionHistory: %v", err)
	}

	records, _ := store.ListExecutionHistory(ctx, "t1", 10, 0)
	if len(records) != 0 {
		t.Errorf("expected 0 records for t1, got %d", len(records))
	}
	records, _ = store.ListExecutionHistory(ctx, "t2", 10, 0)
	if len(records) != 1 {
		t.Errorf("expected 1 record for t2, got %d", len(records))
	}
}

func TestPruneExecutionHistory(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()
	now := time.Now()

	for i := 0; i < 5; i++ {
		rec := ExecutionRecord{
			ID: fmt.Sprintf("e%d", i), TaskID: "t1",
			ExecutedAt:  now.Add(time.Duration(i) * time.Minute),
			CompletedAt: now.Add(time.Duration(i)*time.Minute + time.Second),
			DurationMs:  1000, Status: "success", ResultSummary: fmt.Sprintf("run %d", i),
			TaskType: "general", TargetAgent: "L1", TimelineDir: fmt.Sprintf("logs/cron/t1/e%d", i),
		}
		store.RecordExecution(ctx, rec)
	}

	// Keep only 2 most recent.
	n, err := store.PruneExecutionHistory(ctx, "t1", 2)
	if err != nil {
		t.Fatalf("PruneExecutionHistory: %v", err)
	}
	if n != 3 {
		t.Errorf("expected 3 pruned, got %d", n)
	}

	records, _ := store.ListExecutionHistory(ctx, "t1", 10, 0)
	if len(records) != 2 {
		t.Fatalf("expected 2 records after prune, got %d", len(records))
	}
	// Newest first: e4, e3.
	if records[0].ID != "e4" || records[1].ID != "e3" {
		t.Errorf("unexpected after prune: %v", records)
	}
}

func TestDeleteExecutionHistory_EmptyTask(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()
	// Deleting history for a task with no records should not error.
	if err := store.DeleteExecutionHistory(ctx, "nonexistent"); err != nil {
		t.Fatalf("DeleteExecutionHistory on empty task: %v", err)
	}
}

func TestResultSummaryTruncation(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()
	now := time.Now()

	longSummary := ""
	for i := 0; i < 600; i++ {
		longSummary += "x"
	}

	rec := ExecutionRecord{ID: "e1", TaskID: "t1", ExecutedAt: now, CompletedAt: now.Add(time.Second), DurationMs: 1000, Status: "success", ResultSummary: longSummary, TaskType: "general", TargetAgent: "L1", TimelineDir: "logs/cron/t1/e1"}
	if err := store.RecordExecution(ctx, rec); err != nil {
		t.Fatalf("RecordExecution: %v", err)
	}

	got, _ := store.GetExecutionHistory(ctx, "t1", "e1")
	if len(got.ResultSummary) > maxSummaryLen {
		t.Errorf("summary was not truncated: len=%d > max=%d", len(got.ResultSummary), maxSummaryLen)
	}
}

// ============== Scheduler drainEventsWithTimeline Tests ==============

func TestDrainEventsWithTimeline_Basic(t *testing.T) {
	s := newTestScheduler(t)
	tmpDir := t.TempDir()
	s.SetWorkDir(tmpDir)

	task := Task{ID: "test-task", Title: "test", Instruction: "do something"}
	ch := make(chan iface.AgentEvent, 4)

	// Intermediate content remains replayable, but only the final Done payload
	// may be normalized and delivered.
	ch <- &testContentDelta{delta: "private intermediate analysis"}
	ch <- &ToolExecStartEvent{CallID: "submit-1", Name: "SubmitCronResult", Args: `{"summary":"Ready","sections":[{"title":"Details","content":"All systems green."}]}`}
	ch <- &ToolExecDoneEvent{CallID: "submit-1", Name: "SubmitCronResult", Result: `{"summary":"Ready","sections":[{"title":"Details","content":"All systems green."}]}`}
	ch <- &testDoneEvent{content: `{"summary":"misleading done content","sections":[]}`}
	close(ch)

	result, err := s.drainEventsWithTimeline(ch, task, "exec-1")
	if err != nil {
		t.Fatalf("drainEventsWithTimeline: %v", err)
	}
	if result.canonical.Status != "success" {
		t.Fatalf("expected success result, got %+v", result.canonical)
	}
	if strings.Contains(result.replyText, "private intermediate analysis") || strings.Contains(result.replyText, "misleading done content") {
		t.Fatalf("non-authoritative content leaked into delivery: %q", result.replyText)
	}
	for _, want := range []string{"[成功] test", "Ready", "## Details", "All systems green."} {
		if !strings.Contains(result.replyText, want) {
			t.Errorf("standardized reply missing %q: %q", want, result.replyText)
		}
	}
	if result.timelineDir == "" {
		t.Error("timelineDir should not be empty")
	}

	// Verify timeline file was created.
	files, err := filepath.Glob(filepath.Join(tmpDir, "logs", "cron", "test-task", "exec-1", "timeline-*.jsonl"))
	if err != nil || len(files) == 0 {
		t.Errorf("timeline file was not created (err=%v, files=%v)", err, files)
		return
	}
	timelineBytes, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read timeline: %v", err)
	}
	if !strings.Contains(string(timelineBytes), "private intermediate analysis") {
		t.Fatalf("timeline must retain intermediate content:\n%s", timelineBytes)
	}
}

func TestDrainEventsWithTimeline_InvalidFinalOutputIsLoggedAndStandardized(t *testing.T) {
	s := newTestScheduler(t)
	tmpDir := t.TempDir()
	s.SetWorkDir(tmpDir)

	task := Task{ID: "test-invalid", Title: "Invalid output", Instruction: "do something"}
	raw := `{"summary":"完成","sections":[{"title":"纪律自评","content":"午间即时降级"震荡市下沿、全面防守"，执行到位"}]}`
	ch := make(chan iface.AgentEvent, 2)
	ch <- &testContentDelta{delta: "do not deliver this process output"}
	ch <- &testDoneEvent{content: raw}
	close(ch)

	result, err := s.drainEventsWithTimeline(ch, task, "exec-invalid")
	if err != nil {
		t.Fatalf("drainEventsWithTimeline: %v", err)
	}
	if result.canonical.Status != "failed" || result.canonical.Error == nil || !strings.HasPrefix(*result.canonical.Error, "missing_structured_submission:") {
		t.Fatalf("missing tool submission must fail closed: %+v", result.canonical)
	}
	if strings.Contains(result.replyText, "do not deliver this process output") || strings.Contains(result.replyText, raw) || strings.Contains(result.replyText, "震荡市下沿、全面防守") || !strings.Contains(result.replyText, "[失败] Invalid output") {
		t.Fatalf("unexpected standardized reply: %q", result.replyText)
	}
	if !strings.Contains(result.diagnosticError, "missing_structured_submission:") {
		t.Fatalf("private diagnostic marker was not retained: %q", result.diagnosticError)
	}

	files, err := filepath.Glob(filepath.Join(tmpDir, "logs", "cron", "test-invalid", "exec-invalid", "timeline-*.jsonl"))
	if err != nil || len(files) == 0 {
		t.Fatalf("timeline file was not created (err=%v, files=%v)", err, files)
	}
	timelineBytes, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read timeline: %v", err)
	}
	if !strings.Contains(string(timelineBytes), "missing_structured_submission") || !strings.Contains(string(timelineBytes), "震荡市下沿、全面防守") {
		t.Fatalf("timeline must retain raw Done content and the scheduler failure:\n%s", timelineBytes)
	}
}

func TestDrainEventsWithTimelineRejectsInvalidAndDuplicateSubmissions(t *testing.T) {
	tests := []struct {
		name      string
		events    []iface.AgentEvent
		want      string
		forbidden []string
	}{
		{
			name: "invalid submission result",
			events: []iface.AgentEvent{
				&ToolExecStartEvent{CallID: "submit-1", Name: "SubmitCronResult", Args: `{"summary":"bad"}`},
				&ToolExecDoneEvent{CallID: "submit-1", Name: "SubmitCronResult", Result: `{"summary":"bad"}`},
				&testDoneEvent{content: "private done"},
			},
			want:      "invalid_structured_submission:",
			forbidden: []string{"private done"},
		},
		{
			name: "tool execution error",
			events: []iface.AgentEvent{
				&ToolExecStartEvent{CallID: "submit-1", Name: "SubmitCronResult", Args: `{"summary":"bad"}`},
				&ToolExecDoneEvent{CallID: "submit-1", Name: "SubmitCronResult", Err: errors.New("private decoder detail at /secret/result")},
				&testDoneEvent{content: "private done"},
			},
			want:      "invalid_structured_submission:",
			forbidden: []string{"/secret/result", "private done"},
		},
		{
			name: "orphan valid done",
			events: []iface.AgentEvent{
				&ToolExecDoneEvent{CallID: "orphan", Name: "SubmitCronResult", Result: `{"summary":"orphan secret","sections":[]}`},
			},
			want:      "invalid_structured_submission:",
			forbidden: []string{"orphan secret"},
		},
		{
			name: "mismatched call IDs",
			events: []iface.AgentEvent{
				&ToolExecStartEvent{CallID: "submit-start", Name: "SubmitCronResult", Args: `{"summary":"start secret","sections":[]}`},
				&ToolExecDoneEvent{CallID: "submit-done", Name: "SubmitCronResult", Result: `{"summary":"mismatch secret","sections":[]}`},
			},
			want:      "invalid_structured_submission:",
			forbidden: []string{"start secret", "mismatch secret"},
		},
		{
			name: "start without done",
			events: []iface.AgentEvent{
				&ToolExecStartEvent{CallID: "unfinished", Name: "SubmitCronResult", Args: `{"summary":"unfinished secret","sections":[]}`},
			},
			want:      "invalid_structured_submission:",
			forbidden: []string{"unfinished secret"},
		},
		{
			name: "duplicate done",
			events: []iface.AgentEvent{
				&ToolExecStartEvent{CallID: "submit-1", Name: "SubmitCronResult", Args: `{"summary":"first secret","sections":[]}`},
				&ToolExecDoneEvent{CallID: "submit-1", Name: "SubmitCronResult", Result: `{"summary":"first secret","sections":[]}`},
				&ToolExecDoneEvent{CallID: "submit-1", Name: "SubmitCronResult", Result: `{"summary":"duplicate done secret","sections":[]}`},
			},
			want:      "invalid_structured_submission:",
			forbidden: []string{"first secret", "duplicate done secret"},
		},
		{
			name: "duplicate start",
			events: []iface.AgentEvent{
				&ToolExecStartEvent{CallID: "submit-1", Name: "SubmitCronResult", Args: `{"summary":"first start secret","sections":[]}`},
				&ToolExecStartEvent{CallID: "submit-1", Name: "SubmitCronResult", Args: `{"summary":"duplicate start secret","sections":[]}`},
				&ToolExecDoneEvent{CallID: "submit-1", Name: "SubmitCronResult", Result: `{"summary":"otherwise valid secret","sections":[]}`},
			},
			want:      "invalid_structured_submission:",
			forbidden: []string{"first start secret", "duplicate start secret", "otherwise valid secret"},
		},
		{
			name: "missing call IDs",
			events: []iface.AgentEvent{
				&ToolExecStartEvent{Name: "SubmitCronResult", Args: `{"summary":"missing start ID secret","sections":[]}`},
				&ToolExecDoneEvent{Name: "SubmitCronResult", Result: `{"summary":"missing done ID secret","sections":[]}`},
			},
			want:      "invalid_structured_submission:",
			forbidden: []string{"missing start ID secret", "missing done ID secret"},
		},
		{
			name: "two valid successes",
			events: []iface.AgentEvent{
				&ToolExecStartEvent{CallID: "submit-1", Name: "SubmitCronResult", Args: `{"summary":"first","sections":[]}`},
				&ToolExecDoneEvent{CallID: "submit-1", Name: "SubmitCronResult", Result: `{"summary":"first","sections":[]}`},
				&ToolExecStartEvent{CallID: "submit-2", Name: "SubmitCronResult", Args: `{"summary":"second","sections":[]}`},
				&ToolExecDoneEvent{CallID: "submit-2", Name: "SubmitCronResult", Result: `{"summary":"second","sections":[]}`},
				&testDoneEvent{content: "private done"},
			},
			want:      "invalid_structured_submission:",
			forbidden: []string{"first", "second", "private done"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestScheduler(t)
			s.SetWorkDir(t.TempDir())
			ch := make(chan iface.AgentEvent, len(tt.events))
			for _, event := range tt.events {
				ch <- event
			}
			close(ch)
			result, err := s.drainEventsWithTimeline(ch, Task{ID: "submission-failure", Title: "Submission failure"}, "run-1")
			if err != nil {
				t.Fatalf("drainEventsWithTimeline: %v", err)
			}
			if result.canonical.Status != "failed" || result.canonical.Error == nil || !strings.HasPrefix(*result.canonical.Error, tt.want) {
				t.Fatalf("unexpected canonical result: %+v", result.canonical)
			}
			for _, forbidden := range tt.forbidden {
				if strings.Contains(result.replyText, forbidden) {
					t.Fatalf("private submission data %q leaked publicly: %q", forbidden, result.replyText)
				}
			}
			if !strings.Contains(result.diagnosticError, strings.TrimSuffix(tt.want, ":")) {
				t.Fatalf("private diagnostic missing: %q", result.diagnosticError)
			}
		})
	}
}

func TestDrainEventsWithTimelineAcceptsInvalidThenCorrectedSubmission(t *testing.T) {
	s := newTestScheduler(t)
	s.SetWorkDir(t.TempDir())
	ch := make(chan iface.AgentEvent, 5)
	ch <- &ToolExecStartEvent{CallID: "submit-invalid", Name: "SubmitCronResult", Args: `{"summary":"private invalid attempt"}`}
	ch <- &ToolExecDoneEvent{CallID: "submit-invalid", Name: "SubmitCronResult", Err: errors.New("private validation detail")}
	ch <- &ToolExecStartEvent{CallID: "submit-corrected", Name: "SubmitCronResult", Args: `{"summary":"Corrected result","sections":[]}`}
	ch <- &ToolExecDoneEvent{CallID: "submit-corrected", Name: "SubmitCronResult", Result: `{"summary":"Corrected result","sections":[]}`}
	ch <- &testDoneEvent{content: "private misleading done"}
	close(ch)

	result, err := s.drainEventsWithTimeline(ch, Task{ID: "corrected-submission", Title: "Corrected submission"}, "run-corrected")
	if err != nil {
		t.Fatalf("drainEventsWithTimeline: %v", err)
	}
	if result.canonical.Status != "success" || result.canonical.Summary != "Corrected result" || result.diagnosticError != "" {
		t.Fatalf("corrected submission must succeed: %+v diagnostic=%q", result.canonical, result.diagnosticError)
	}
	for _, forbidden := range []string{"private invalid attempt", "private validation detail", "private misleading done"} {
		if strings.Contains(result.replyText, forbidden) {
			t.Fatalf("private correction detail %q leaked publicly: %q", forbidden, result.replyText)
		}
	}
	if !strings.Contains(result.replyText, "Corrected result") {
		t.Fatalf("corrected result missing from public reply: %q", result.replyText)
	}
	files, err := filepath.Glob(filepath.Join(s.workDir, "logs", "cron", "corrected-submission", "run-corrected", "timeline-*.jsonl"))
	if err != nil || len(files) == 0 {
		t.Fatalf("find correction timeline: files=%v err=%v", files, err)
	}
	timelineBytes, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read correction timeline: %v", err)
	}
	if !strings.Contains(string(timelineBytes), "private validation detail") {
		t.Fatalf("private validation diagnostic was not retained in timeline:\n%s", timelineBytes)
	}
}

func TestRunL1TaskDeliversCanonicalResults(t *testing.T) {
	tests := []struct {
		name            string
		final           string
		startErr        error
		wantStatus      string
		wantMessage     string
		wantCallbackOK  bool
		wantDiagnostic  string
		forbidInMessage string
	}{
		{
			name:           "valid structured output",
			final:          `{"summary":"Ready","sections":[]}`,
			wantStatus:     "success",
			wantMessage:    "[成功] Cron delivery",
			wantCallbackOK: true,
		},
		{
			name:            "invalid final output",
			final:           `{"summary":"完成","sections":[{"title":"纪律自评","content":"午间即时降级"震荡市下沿、全面防守"，执行到位"}]}`,
			wantStatus:      "failed",
			wantMessage:     "[失败] Cron delivery",
			wantDiagnostic:  "missing_structured_submission:",
			forbidInMessage: "震荡市下沿、全面防守",
		},
		{
			name:        "empty final output",
			final:       "",
			wantStatus:  "failed",
			wantMessage: "[失败] Cron delivery",
		},
		{
			name:            "execution fails to start",
			startErr:        errors.New("provider unavailable at /private/provider.sock?token=secret"),
			wantStatus:      "failed",
			wantMessage:     "[失败] Cron delivery",
			wantDiagnostic:  "provider unavailable at /private/provider.sock?token=secret",
			forbidInMessage: "/private/provider.sock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openTestDB(t)
			task, err := store.CreateTask(context.Background(), CreateTaskInput{
				Title:       "Cron delivery",
				TaskType:    "general",
				Expression:  "0 8 * * *",
				Instruction: "produce a report",
				TargetAgent: "L1",
				NextRunAt:   time.Now().Add(time.Hour),
			})
			if err != nil {
				t.Fatalf("CreateTask: %v", err)
			}

			var messages []string
			session := &mockSession{
				idle: true,
				askStreamFn: func(context.Context, string) (<-chan iface.AgentEvent, error) {
					if tt.startErr != nil {
						return nil, tt.startErr
					}
					ch := make(chan iface.AgentEvent, 4)
					ch <- &testContentDelta{delta: "private process output"}
					if tt.name == "valid structured output" {
						ch <- &ToolExecStartEvent{CallID: "submit-1", Name: "SubmitCronResult", Args: tt.final}
						ch <- &ToolExecDoneEvent{CallID: "submit-1", Name: "SubmitCronResult", Result: tt.final}
					}
					ch <- &testDoneEvent{content: tt.final}
					close(ch)
					return ch, nil
				},
				sendViaChannelFn: func(_ context.Context, text string) error {
					messages = append(messages, text)
					return nil
				},
			}
			s := NewScheduler(store, &mockSessionManager{session: session}, nil)
			s.SetWorkDir(t.TempDir())
			t.Cleanup(s.Stop)
			callbackCalled := false
			callbackOK := false
			s.OnTaskComplete = func(_, _ string, success bool, _ string) {
				callbackCalled = true
				callbackOK = success
			}

			s.runL1Task(context.Background(), *task, session)

			if len(messages) != 1 || !strings.Contains(messages[0], tt.wantMessage) {
				t.Fatalf("messages = %q, want one standardized message containing %q", messages, tt.wantMessage)
			}
			if strings.Contains(messages[0], "private process output") {
				t.Fatalf("intermediate process output leaked: %q", messages[0])
			}
			if tt.forbidInMessage != "" && strings.Contains(messages[0], tt.forbidInMessage) {
				t.Fatalf("private diagnostic leaked into public message: %q", messages[0])
			}
			if !callbackCalled || callbackOK != tt.wantCallbackOK {
				t.Fatalf("completion callback called=%v success=%v, want called=true success=%v", callbackCalled, callbackOK, tt.wantCallbackOK)
			}
			records, err := store.ListExecutionHistory(context.Background(), task.ID, 1, 0)
			if err != nil || len(records) != 1 {
				t.Fatalf("ListExecutionHistory err=%v records=%v", err, records)
			}
			if records[0].Status != tt.wantStatus {
				t.Fatalf("execution status = %q, want %q", records[0].Status, tt.wantStatus)
			}
			if tt.wantDiagnostic != "" && !strings.Contains(records[0].ErrorMessage, tt.wantDiagnostic) {
				t.Fatalf("raw diagnostic was not persisted: %+v", records[0])
			}
		})
	}
}

func TestCronPanicsUseCanonicalFailureAndKeepRawDiagnosticsPrivate(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Scheduler, *Task, *mockSession)
	}{
		{
			name: "L1 idle panic",
			run: func(s *Scheduler, task *Task, session *mockSession) {
				session.idleFn = func() bool { panic("private L1 panic at /secret/l1") }
				s.executeL1Task(*task)
			},
		},
		{
			name: "L2 execution panic",
			run: func(s *Scheduler, task *Task, session *mockSession) {
				task.TargetAgent = "engineering"
				session.hasNotifyChannel = true
				session.askStreamFn = func(context.Context, string) (<-chan iface.AgentEvent, error) {
					panic("private L2 panic at /secret/l2")
				}
				s.sessionMgr = &mockSessionManager{getSession: func(context.Context, string, string) (Session, bool, func(), error) {
					return session, false, nil, nil
				}}
				s.executeL2Task(*task)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openTestDB(t)
			task, err := store.CreateTask(context.Background(), CreateTaskInput{
				Title:       "Panic task",
				TaskType:    "general",
				Expression:  "0 8 * * *",
				Instruction: "panic",
				TargetAgent: "L1",
				NextRunAt:   time.Now().Add(time.Hour),
			})
			if err != nil {
				t.Fatal(err)
			}
			var messages []string
			session := &mockSession{idle: true, sendViaChannelFn: func(_ context.Context, text string) error {
				messages = append(messages, text)
				return nil
			}}
			s := NewScheduler(store, &mockSessionManager{session: session}, nil)
			s.SetWorkDir(t.TempDir())
			callbackOK := true
			s.OnTaskComplete = func(_, _ string, success bool, _ string) { callbackOK = success }

			tt.run(s, task, session)

			if callbackOK {
				t.Fatal("panic completion callback must not report success")
			}
			if len(messages) != 1 || !strings.Contains(messages[0], "[失败] Panic task") {
				t.Fatalf("panic message = %q, want one canonical failure", messages)
			}
			if strings.Contains(messages[0], "/secret/") || strings.Contains(messages[0], "private L") {
				t.Fatalf("panic diagnostic leaked publicly: %q", messages[0])
			}
			records, err := store.ListExecutionHistory(context.Background(), task.ID, 1, 0)
			if err != nil || len(records) != 1 {
				t.Fatalf("history err=%v records=%v", err, records)
			}
			if records[0].Status != "failed" || !strings.Contains(records[0].ErrorMessage, "private L") {
				t.Fatalf("panic history did not retain raw diagnostic: %+v", records[0])
			}
		})
	}
}

func TestFailedRetriesUseTerminalAttemptProvenance(t *testing.T) {
	for _, target := range []string{"L1", "engineering"} {
		t.Run(target, func(t *testing.T) {
			store := openTestDB(t)
			task, err := store.CreateTask(context.Background(), CreateTaskInput{
				Title:       "Retry task",
				TaskType:    "general",
				Expression:  "0 8 * * *",
				Instruction: "retry",
				TargetAgent: target,
				NextRunAt:   time.Now().Add(time.Hour),
			})
			if err != nil {
				t.Fatal(err)
			}

			var attempts int
			var messages []string
			session := &mockSession{
				idle:             true,
				hasNotifyChannel: true,
				askStreamFn: func(context.Context, string) (<-chan iface.AgentEvent, error) {
					attempts++
					ch := make(chan iface.AgentEvent, 1)
					if attempts == 1 {
						ch <- &testErrorEvent{err: errors.New("initial private failure")}
					} else {
						ch <- &testErrorEvent{err: errors.New("terminal private retry failure")}
					}
					close(ch)
					return ch, nil
				},
				sendViaChannelFn: func(_ context.Context, text string) error {
					messages = append(messages, text)
					return nil
				},
			}
			manager := &mockSessionManager{session: session}
			if target != "L1" {
				manager.getSession = func(context.Context, string, string) (Session, bool, func(), error) {
					return session, false, nil, nil
				}
			}
			s := NewScheduler(store, manager, nil)
			s.SetWorkDir(t.TempDir())
			s.retryDelay = 0
			if target == "L1" {
				s.runL1Task(context.Background(), *task, session)
			} else {
				s.executeL2Task(*task)
			}

			if attempts != 2 {
				t.Fatalf("attempts = %d, want 2", attempts)
			}
			if len(messages) != 1 || strings.Contains(messages[0], "private") {
				t.Fatalf("public failure must be canonical and private: %q", messages)
			}
			records, err := store.ListExecutionHistory(context.Background(), task.ID, 1, 0)
			if err != nil || len(records) != 1 {
				t.Fatalf("history err=%v records=%v", err, records)
			}
			if !strings.Contains(records[0].TimelineDir, "-retry") {
				t.Fatalf("timeline provenance = %q, want terminal retry timeline", records[0].TimelineDir)
			}
			if !strings.Contains(records[0].ErrorMessage, "terminal private retry failure") || strings.Contains(records[0].ErrorMessage, "initial private failure") {
				t.Fatalf("diagnostic provenance did not use terminal retry: %+v", records[0])
			}
		})
	}
}

func TestRetryStartFailureKeepsRawErrorOutOfPublicResult(t *testing.T) {
	store := openTestDB(t)
	task, err := store.CreateTask(context.Background(), CreateTaskInput{
		Title:       "Retry start task",
		TaskType:    "general",
		Expression:  "0 8 * * *",
		Instruction: "retry",
		TargetAgent: "L1",
		NextRunAt:   time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	var attempts int
	var messages []string
	session := &mockSession{
		idle: true,
		askStreamFn: func(context.Context, string) (<-chan iface.AgentEvent, error) {
			attempts++
			if attempts == 2 {
				return nil, errors.New("retry start private endpoint /secret/retry")
			}
			ch := make(chan iface.AgentEvent, 1)
			ch <- &testErrorEvent{err: errors.New("initial failure")}
			close(ch)
			return ch, nil
		},
		sendViaChannelFn: func(_ context.Context, text string) error {
			messages = append(messages, text)
			return nil
		},
	}
	s := NewScheduler(store, &mockSessionManager{session: session}, nil)
	s.SetWorkDir(t.TempDir())
	s.retryDelay = 0
	s.runL1Task(context.Background(), *task, session)

	if len(messages) != 1 || strings.Contains(messages[0], "/secret/retry") {
		t.Fatalf("retry start detail leaked publicly: %q", messages)
	}
	records, err := store.ListExecutionHistory(context.Background(), task.ID, 1, 0)
	if err != nil || len(records) != 1 || !strings.Contains(records[0].ErrorMessage, "/secret/retry") {
		t.Fatalf("retry start diagnostic not retained: err=%v records=%v", err, records)
	}
	if !strings.Contains(records[0].TimelineDir, "-retry") {
		t.Fatalf("retry start provenance = %q, want terminal retry timeline", records[0].TimelineDir)
	}
}

func TestDrainEventsWithTimeline_Error(t *testing.T) {
	s := newTestScheduler(t)
	s.SetWorkDir(t.TempDir())

	task := Task{ID: "test-task", Instruction: "do something"}
	ch := make(chan iface.AgentEvent, 1)
	ch <- &testErrorEvent{err: errors.New("something went wrong")}
	close(ch)

	_, err := s.drainEventsWithTimeline(ch, task, "exec-err")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "agent error: something went wrong" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExecuteTask_UpdatesNextRunOnDrainError(t *testing.T) {
	store := openTestDB(t)

	ctx := context.Background()
	task, err := store.CreateTask(ctx, CreateTaskInput{
		Title:       "Test Drain Error Task",
		TaskType:    "general",
		Expression:  "0 8 * * *",
		Instruction: "do something",
		NextRunAt:   time.Now().Add(-1 * time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	s := NewScheduler(store, nil, nil)
	s.updateTaskAfterExecution(ctx, *task)

	updated, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if updated.LastRunAt == nil {
		t.Error("expected LastRunAt to be non-nil after updateTaskAfterExecution")
	}
	if updated.NextRunAt.Before(time.Now()) {
		t.Errorf("expected NextRunAt to be in the future, got %v", updated.NextRunAt)
	}
}

type testToolExecStart struct {
	callID string
	name   string
	args   string
}

type ToolExecStartEvent struct {
	CallID string
	Name   string
	Args   string
}

func (e *ToolExecStartEvent) IsAgentEvent()                  {}
func (e *ToolExecStartEvent) ContentDelta() (string, bool)   { return "", false }
func (e *ToolExecStartEvent) DoneContent() (string, bool)    { return "", false }
func (e *ToolExecStartEvent) Error() (error, bool)           { return nil, false }
func (e *ToolExecStartEvent) ConfirmRequest() (string, bool) { return "", false }

type ToolExecDoneEvent struct {
	CallID string
	Name   string
	Result string
	Err    error
}

func (e *ToolExecDoneEvent) IsAgentEvent()                  {}
func (e *ToolExecDoneEvent) ContentDelta() (string, bool)   { return "", false }
func (e *ToolExecDoneEvent) DoneContent() (string, bool)    { return "", false }
func (e *ToolExecDoneEvent) Error() (error, bool)           { return nil, false }
func (e *ToolExecDoneEvent) ConfirmRequest() (string, bool) { return "", false }

func (e *testToolExecStart) IsAgentEvent()                  {}
func (e *testToolExecStart) ContentDelta() (string, bool)   { return "", false }
func (e *testToolExecStart) DoneContent() (string, bool)    { return "", false }
func (e *testToolExecStart) Error() (error, bool)           { return nil, false }
func (e *testToolExecStart) ConfirmRequest() (string, bool) { return "", false }

func TestDrainEventsWithTimeline_ToolCallCount(t *testing.T) {
	s := newTestScheduler(t)
	s.SetWorkDir(t.TempDir())

	task := Task{ID: "test-task-tool", Instruction: "run tool task"}
	ch := make(chan iface.AgentEvent, 2)
	ch <- &testToolExecStart{callID: "call-1", name: "pre_market", args: "{}"}
	ch <- &testDoneEvent{content: "done"}
	close(ch)

	res, err := s.drainEventsWithTimeline(ch, task, "exec-tool-count")
	if err != nil {
		t.Fatalf("drainEventsWithTimeline error: %v", err)
	}
	if res.toolCallCount != 1 {
		t.Errorf("expected toolCallCount == 1, got %d", res.toolCallCount)
	}
}

// ── Mock event types that implement iface.AgentEvent + iface.EventConsumer ──

type testContentDelta struct {
	delta string
}

func (e *testContentDelta) IsAgentEvent()                  {}
func (e *testContentDelta) ContentDelta() (string, bool)   { return e.delta, true }
func (e *testContentDelta) DoneContent() (string, bool)    { return "", false }
func (e *testContentDelta) Error() (error, bool)           { return nil, false }
func (e *testContentDelta) ConfirmRequest() (string, bool) { return "", false }

type testDoneEvent struct {
	content string
}

func (e *testDoneEvent) IsAgentEvent()                  {}
func (e *testDoneEvent) ContentDelta() (string, bool)   { return "", false }
func (e *testDoneEvent) DoneContent() (string, bool)    { return e.content, true }
func (e *testDoneEvent) Error() (error, bool)           { return nil, false }
func (e *testDoneEvent) ConfirmRequest() (string, bool) { return "", false }

type testErrorEvent struct {
	err error
}

func (e *testErrorEvent) IsAgentEvent()                  {}
func (e *testErrorEvent) ContentDelta() (string, bool)   { return "", false }
func (e *testErrorEvent) DoneContent() (string, bool)    { return "", false }
func (e *testErrorEvent) Error() (error, bool)           { return e.err, true }
func (e *testErrorEvent) ConfirmRequest() (string, bool) { return "", false }
