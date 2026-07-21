package cron

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
)

// mockSession implements the Session interface for testing.
type mockSession struct {
	idle        bool
	queued      []string
	askStreamFn func(ctx context.Context, prompt string) (<-chan iface.AgentEvent, error)
	modelParams *iface.ModelOverrideParams
}

func (m *mockSession) Idle() bool                 { return m.idle }
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

func TestValidateTaskLevelSupportsFiveLevels(t *testing.T) {
	for _, level := range []string{"L0", "L1", "L2", "L3", "L4"} {
		if err := ValidateTaskLevel(level); err != nil {
			t.Errorf("ValidateTaskLevel(%q): %v", level, err)
		}
	}
	if err := ValidateTaskLevel("L5"); err == nil {
		t.Fatal("ValidateTaskLevel accepted unsupported L5")
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
	task := Task{Instruction: "Check health status"}
	prompt := buildCronPrompt(task)
	if prompt == "" {
		t.Error("buildCronPrompt returned empty string")
	}
}

func TestBuildCronContextDetectsQQOrigin(t *testing.T) {
	s := newTestScheduler(t)
	nonQQ := s.buildCronContext(Task{QQSource: -1})
	if iface.IsQBotFromContext(nonQQ) {
		t.Fatal("non-QQ cron task was marked as QQ-originated")
	}
	qq := s.buildCronContext(Task{QQSource: 0, QQTargetOpenID: "user-1"})
	if !iface.IsQBotFromContext(qq) {
		t.Fatal("QQ cron task was not marked as QQ-originated")
	}
}

func TestParseSendFileMedia(t *testing.T) {
	raw := `{"status":"success","file_type":"image","file_name":"test.png","url":"https://example.com/img.png"}`
	result := parseSendFileMedia(raw)
	if result == nil {
		t.Fatal("parseSendFileMedia returned nil")
	}
	if result.FileType != 1 {
		t.Errorf("FileType = %d, want 1", result.FileType)
	}

	if parseSendFileMedia("not json") != nil {
		t.Error("parseSendFileMedia should return nil for invalid JSON")
	}
}

func TestBuildTaskPrompt(t *testing.T) {
	s := newTestScheduler(t)
	s.SetMemoryEngine("fake-engine", func(ctx context.Context, prompt string, memEngine interface{}, log *logger.Logger) string {
		return ""
	})

	task := Task{ID: "t1", Instruction: "Do something important"}
	prompt := s.buildTaskPrompt(task)
	if prompt == "" {
		t.Error("buildTaskPrompt returned empty string")
	}
}

func TestBuildCronContext(t *testing.T) {
	s := newTestScheduler(t)
	task := Task{ID: "t1", QQSource: 1, QQOpenID: "openid-1", QQTargetOpenID: "target-1", QQChatID: "chat-1"}
	ctx := s.buildCronContext(task)
	if ctx == nil {
		t.Error("buildCronContext returned nil")
	}
}

func TestScheduler_NewAndInit(t *testing.T) {
	s := newTestScheduler(t)
	if s == nil {
		t.Fatal("NewScheduler returned nil")
	}
	if s.l1Cond == nil || s.resultCond == nil {
		t.Error("cond not initialized")
	}
	if s.entries == nil || s.timers == nil {
		t.Error("maps not initialized")
	}
}

func TestScheduler_SetMemoryEngine(t *testing.T) {
	s := newTestScheduler(t)
	s.SetMemoryEngine("fake-engine", func(ctx context.Context, prompt string, memEngine interface{}, log *logger.Logger) string {
		return "recalled"
	})
	if s.memoryEngine != "fake-engine" {
		t.Error("memoryEngine not set")
	}
	if s.buildRecalledFn == nil {
		t.Error("buildRecalledFn not set")
	}
}

func TestSchedulerAskWithTaskModel(t *testing.T) {
	s := newTestScheduler(t)
	s.SetModelResolver(func(level string) (ResolvedModel, error) {
		return ResolvedModel{
			Params:        iface.ModelOverrideParams{ModelID: "superior-model", ProviderID: "p", Level: level},
			RequestedRole: "superior",
		}, nil
	})
	sess := &mockSession{}
	task := Task{ID: "t1", Title: "Health check", TaskLevel: "L2", Instruction: "check"}
	_, ch, err := s.askWithTaskModel(context.Background(), task, sess)
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	if sess.modelParams == nil || sess.modelParams.ModelID != "superior-model" || sess.modelParams.Level != "L2" {
		t.Fatalf("unexpected model params: %+v", sess.modelParams)
	}
}

func openTestDB(t *testing.T) *DBStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sqlitedb.Open(path)
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
	rec1 := ExecutionRecord{ID: "e1", TaskID: "t1", ExecutedAt: now.Add(-2 * time.Hour), CompletedAt: now.Add(-2*time.Hour).Add(5 * time.Second), DurationMs: 5000, Status: "success", ResultSummary: "All good 1", TaskLevel: "L1", TargetAgent: "L1", ModelID: "m1", ProviderID: "p1", TimelineDir: "logs/cron/t1/e1"}
	rec2 := ExecutionRecord{ID: "e2", TaskID: "t1", ExecutedAt: now.Add(-1 * time.Hour), CompletedAt: now.Add(-1*time.Hour).Add(3 * time.Second), DurationMs: 3000, Status: "failed", ResultSummary: "", ErrorMessage: "timeout", TaskLevel: "L1", TargetAgent: "L1", ModelID: "m2", ProviderID: "p1", TimelineDir: "logs/cron/t1/e2"}
	rec3 := ExecutionRecord{ID: "e3", TaskID: "t1", ExecutedAt: now, CompletedAt: now.Add(7 * time.Second), DurationMs: 7000, Status: "panic", ResultSummary: "", ErrorMessage: "panic: nil pointer", TaskLevel: "L1", TargetAgent: "L1", TimelineDir: "logs/cron/t1/e3"}

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

	rec := ExecutionRecord{ID: "e1", TaskID: "t1", ExecutedAt: now, CompletedAt: now.Add(time.Second), DurationMs: 1000, Status: "success", ResultSummary: "done", TaskLevel: "L1", TargetAgent: "L1", TimelineDir: "logs/cron/t1/e1"}
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

	rec := ExecutionRecord{ID: "e1", TaskID: "t1", ExecutedAt: now, CompletedAt: now.Add(time.Second), DurationMs: 1000, Status: "success", ResultSummary: "done", TaskLevel: "L1", TargetAgent: "L1", TimelineDir: "logs/cron/t1/e1"}
	store.RecordExecution(ctx, rec)
	rec2 := ExecutionRecord{ID: "e2", TaskID: "t2", ExecutedAt: now, CompletedAt: now.Add(time.Second), DurationMs: 1000, Status: "success", ResultSummary: "done", TaskLevel: "L1", TargetAgent: "L1", TimelineDir: "logs/cron/t2/e2"}
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
			ExecutedAt: now.Add(time.Duration(i) * time.Minute),
			CompletedAt: now.Add(time.Duration(i)*time.Minute + time.Second),
			DurationMs: 1000, Status: "success", ResultSummary: fmt.Sprintf("run %d", i),
			TaskLevel: "L1", TargetAgent: "L1", TimelineDir: fmt.Sprintf("logs/cron/t1/e%d", i),
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

	rec := ExecutionRecord{ID: "e1", TaskID: "t1", ExecutedAt: now, CompletedAt: now.Add(time.Second), DurationMs: 1000, Status: "success", ResultSummary: longSummary, TaskLevel: "L1", TargetAgent: "L1", TimelineDir: "logs/cron/t1/e1"}
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
	ch := make(chan iface.AgentEvent, 2)

	// Simulate a simple content delta + done.
	ch <- &testContentDelta{delta: "hello"}
	ch <- &testDoneEvent{content: " world"}
	close(ch)

	result, err := s.drainEventsWithTimeline(ch, task, "exec-1")
	if err != nil {
		t.Fatalf("drainEventsWithTimeline: %v", err)
	}
	if result.replyText != "hello world" {
		t.Errorf("expected 'hello world', got %q", result.replyText)
	}
	if result.timelineDir == "" {
		t.Error("timelineDir should not be empty")
	}

	// Verify timeline file was created.
	files, err := filepath.Glob(filepath.Join(tmpDir, "logs", "cron", "test-task", "exec-1", "timeline-*.jsonl"))
	if err != nil || len(files) == 0 {
		t.Errorf("timeline file was not created (err=%v, files=%v)", err, files)
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

// ── Mock event types that implement iface.AgentEvent + iface.EventConsumer ──

type testContentDelta struct {
	delta string
}

func (e *testContentDelta) IsAgentEvent()                    {}
func (e *testContentDelta) ContentDelta() (string, bool)     { return e.delta, true }
func (e *testContentDelta) DoneContent() (string, bool)      { return "", false }
func (e *testContentDelta) Error() (error, bool)             { return nil, false }
func (e *testContentDelta) ConfirmRequest() (string, bool)   { return "", false }

type testDoneEvent struct {
	content string
}

func (e *testDoneEvent) IsAgentEvent()                    {}
func (e *testDoneEvent) ContentDelta() (string, bool)     { return "", false }
func (e *testDoneEvent) DoneContent() (string, bool)      { return e.content, true }
func (e *testDoneEvent) Error() (error, bool)             { return nil, false }
func (e *testDoneEvent) ConfirmRequest() (string, bool)   { return "", false }

type testErrorEvent struct {
	err error
}

func (e *testErrorEvent) IsAgentEvent()                    {}
func (e *testErrorEvent) ContentDelta() (string, bool)     { return "", false }
func (e *testErrorEvent) DoneContent() (string, bool)      { return "", false }
func (e *testErrorEvent) Error() (error, bool)             { return e.err, true }
func (e *testErrorEvent) ConfirmRequest() (string, bool)   { return "", false }
