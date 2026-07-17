package cron

import (
	"context"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/logger"
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
	ch, err := s.askWithTaskModel(context.Background(), task, sess)
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	if sess.modelParams == nil || sess.modelParams.ModelID != "superior-model" || sess.modelParams.Level != "L2" {
		t.Fatalf("unexpected model params: %+v", sess.modelParams)
	}
}

func TestScheduler_OnTaskCompleted(t *testing.T) {
	s := newTestScheduler(t)
	var called bool
	s.OnTaskCompleted(func(ctx context.Context, task Task, reply string, mediaFiles []SendFileMedia) {
		called = true
	})
	s.invokeCompletionHook(Task{ID: "t1"}, "done", nil)
	if !called {
		t.Error("completion hook not invoked")
	}
}
