package cron

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	robfig "github.com/robfig/cron/v3"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// Session defines the interface required by the Scheduler to trigger tasks.
type Session interface {
	Idle() bool
	QueueMessage(prompt string)
	AskStream(ctx context.Context, prompt string) (<-chan iface.AgentEvent, error)
	// AskIsolated executes a prompt in a clean context (no conversation history,
	// no writes to the session's ContextWindow or timeline).
	AskIsolated(ctx context.Context, prompt string) (<-chan iface.AgentEvent, error)
}

// SessionManager provides sessions for scheduled task execution.
type SessionManager interface {
	// Session returns the L1 session (may be nil if not initialized).
	Session() Session

	// GetSession returns a session for the given teamID.
	// For "L1": returns the existing L1 session, isNew=false, no-op cleanup.
	// For other teams (e.g. "engineering", "design"): creates a new L2 session,
	// isNew=true, cleanup func must be called after execution.
	// The caller MUST call cleanup() when done with a new session.
	GetSession(ctx context.Context, teamID, taskID string) (sess Session, isNew bool, cleanup func(), err error)
}

// SendFileMedia holds metadata about a file the agent sent via the SendFile tool.
type SendFileMedia struct {
	FileType   int
	URL        string
	Base64Data string
	FileName   string
}

// TaskCompletedHook defines the callback invoked when a scheduled task completes.
type TaskCompletedHook func(ctx context.Context, task Task, reply string, mediaFiles []SendFileMedia)

// MemoryEngine is the interface required for memory recall during cron execution.
type MemoryEngine interface {
	Search(ctx context.Context, query string, limit int) (string, error)
}

// BuildRecalledContextFn is a function type that enriches a prompt with recalled memories.
type BuildRecalledContextFn func(ctx context.Context, prompt string, memEngine interface{}, log *logger.Logger) string

// cronTask wraps a Task with its execution metadata.
type cronTask struct {
	task     Task
	enqueued time.Time
}

// pendingResult holds a completed L2 task result awaiting delivery to L1.
type pendingResult struct {
	task       Task
	reply      string
	mediaFiles []SendFileMedia
	completed  time.Time
}

// Scheduler manages executing scheduled tasks (both cron and timer-based) in the background.
type Scheduler struct {
	dbStore    *DBStore
	sessionMgr SessionManager
	logger     *logger.Logger
	cron       *robfig.Cron

	// Memory engine for recall enrichment of cron prompts
	memoryEngine    interface{}
	buildRecalledFn BuildRecalledContextFn

	// L1 task queue: serializes L1-targeted cron tasks
	l1Queue   []cronTask
	l1Mu      sync.Mutex
	l1Cond    *sync.Cond
	l1Running bool

	// L2 result queue: delivers completed L2 results to L1
	resultQueue []pendingResult
	resultMu    sync.Mutex
	resultCond  *sync.Cond

	mu              sync.Mutex
	entries         map[string]robfig.EntryID
	timers          map[string]*time.Timer
	onTaskCompleted TaskCompletedHook
	stopped         bool
}

// OnTaskCompleted registers a callback for completed tasks.
func (s *Scheduler) OnTaskCompleted(h TaskCompletedHook) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onTaskCompleted = h
}

// NewScheduler constructs a new Scheduler.
func NewScheduler(db *DBStore, sm SessionManager, l *logger.Logger) *Scheduler {
	if l == nil {
		var err error
		l, err = logger.System(os.TempDir(), logger.WithConsole(false), logger.WithFile(false))
		if err != nil {
			panic(err)
		}
	}
	s := &Scheduler{
		dbStore:    db,
		sessionMgr: sm,
		logger:     l,
		cron: robfig.New(
			robfig.WithParser(robfig.NewParser(
				robfig.Minute | robfig.Hour | robfig.Dom | robfig.Month | robfig.Dow,
			)),
			robfig.WithChain(robfig.SkipIfStillRunning(robfig.DiscardLogger)),
		),
		entries: make(map[string]robfig.EntryID),
		timers:  make(map[string]*time.Timer),
	}
	s.l1Cond = sync.NewCond(&s.l1Mu)
	s.resultCond = sync.NewCond(&s.resultMu)
	return s
}

// SetMemoryEngine configures the scheduler to enrich cron prompts with recalled memories.
// buildFn is typically session.BuildRecalledContext.
func (s *Scheduler) SetMemoryEngine(engine interface{}, buildFn BuildRecalledContextFn) {
	s.memoryEngine = engine
	s.buildRecalledFn = buildFn
}

// Start loads all active tasks from DB, resets any stale 'running' tasks
// (crash recovery), schedules them, and starts the cron runner.
// Also starts the L1 result delivery goroutine.
func (s *Scheduler) Start(ctx context.Context) error {
	resetCount, err := s.dbStore.ResetStaleRunning(ctx, time.Now().Add(-1*time.Minute))
	if err != nil {
		s.logger.Error(logger.CatApp, "cron: failed to reset stale running tasks", "err", err)
	}
	if resetCount > 0 {
		s.logger.Info(logger.CatApp, "cron: reset stale running tasks", "count", resetCount)
	}

	tasks, err := s.dbStore.GetActiveTasks(ctx)
	if err != nil {
		s.logger.Error(logger.CatApp, "cron: failed to load active tasks on startup", "err", err)
		return err
	}

	for _, task := range tasks {
		s.Schedule(task)
	}

	// Start background goroutines for L1 task queue and L2 result delivery.
	go s.l1QueueLoop()
	go s.l1ResultLoop()

	s.cron.Start()
	s.logger.InfoContext(ctx, logger.CatApp, "cron: scheduler daemon started successfully")
	return nil
}

// Stop stops the background cron runner, cancels all active timers, and
// signals the background loops to exit.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()

	s.cron.Stop()
	s.mu.Lock()
	for _, timer := range s.timers {
		timer.Stop()
	}
	s.entries = make(map[string]robfig.EntryID)
	s.timers = make(map[string]*time.Timer)
	s.mu.Unlock()

	// Wake up background loops so they can exit.
	s.l1Cond.Broadcast()
	s.resultCond.Broadcast()

	s.logger.Info(logger.CatApp, "cron: scheduler daemon stopped")
}

// Schedule dynamically schedules (or updates) a task.
func (s *Scheduler) Schedule(t Task) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.unscheduleLocked(t.ID)

	if t.IsOneTime() {
		delay := time.Until(t.NextRunAt)
		if delay <= 0 {
			go s.executeTask(t)
			return
		}

		timer := time.AfterFunc(delay, func() {
			s.executeTask(t)
			s.mu.Lock()
			delete(s.timers, t.ID)
			s.mu.Unlock()
		})
		s.timers[t.ID] = timer
		s.logger.Info(logger.CatApp, "cron: scheduled one-time task", "task_id", t.ID, "run_at", t.NextRunAt.Format("2006-01-02 15:04:05"))
	} else {
		entryID, err := s.cron.AddFunc(t.Expression, func() {
			s.executeTask(t)
		})
		if err != nil {
			s.logger.Error(logger.CatApp, "cron: failed to add cron task", "task_id", t.ID, "err", err)
			return
		}
		s.entries[t.ID] = entryID
		s.logger.Info(logger.CatApp, "cron: scheduled recurring task", "task_id", t.ID, "expr", t.Expression)
	}
}

// Unschedule dynamically removes a task by ID.
func (s *Scheduler) Unschedule(taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unscheduleLocked(taskID)
}

func (s *Scheduler) unscheduleLocked(taskID string) {
	if entryID, exists := s.entries[taskID]; exists {
		s.cron.Remove(entryID)
		delete(s.entries, taskID)
		s.logger.Info(logger.CatApp, "cron: unscheduled cron task", "task_id", taskID)
	}

	if timer, exists := s.timers[taskID]; exists {
		timer.Stop()
		delete(s.timers, taskID)
		s.logger.Info(logger.CatApp, "cron: cancelled timer task", "task_id", taskID)
	}
}

// isL1Target returns true if the task targets L1.
func isL1Target(task Task) bool {
	target := strings.TrimSpace(task.TargetAgent)
	return target == "" || strings.EqualFold(target, "L1")
}

// executeTask is the entry point for all task executions.
// It dispatches to the appropriate execution path based on TargetAgent.
func (s *Scheduler) executeTask(t Task) {
	s.logger.Info(logger.CatApp, "cron: task execution triggered", "task_id", t.ID,
		"instruction", t.Instruction, "target_agent", t.TargetAgent)

	if isL1Target(t) {
		s.executeL1Task(t)
	} else {
		s.executeL2Task(t)
	}
}

// executeL1Task handles tasks targeting L1. If L1 is busy, the task is queued
// and executed later by l1QueueLoop.
func (s *Scheduler) executeL1Task(t Task) {
	ctx := context.Background()

	// Two-phase commit: claim the task.
	claimed, err := s.dbStore.ClaimTask(ctx, t.ID)
	if err != nil {
		s.logger.Error(logger.CatApp, "cron: failed to claim L1 task", "task_id", t.ID, "err", err)
		return
	}
	if !claimed {
		s.logger.Debug(logger.CatApp, "cron: L1 task already claimed by another instance, skipping", "task_id", t.ID)
		return
	}

	// Panic recovery: catch panic, log, return to 'active' for retry.
	defer func() {
		if panicValue := recover(); panicValue != nil {
			s.logger.Error(logger.CatApp, "cron: L1 task execution panicked", "task_id", t.ID, "panic", panicValue)
			_ = s.dbStore.UpdateTaskStatus(ctx, t.ID, "active")
		}
	}()

	l1Session := s.sessionMgr.Session()
	if l1Session == nil {
		s.logger.Warn(logger.CatApp, "cron: L1 task skipped, no active session", "task_id", t.ID)
		_ = s.dbStore.UpdateTaskStatus(ctx, t.ID, "active")
		return
	}

	if !l1Session.Idle() {
		// L1 is busy with user conversation — queue the task for later.
		s.logger.Info(logger.CatApp, "cron: L1 busy, queuing L1 task", "task_id", t.ID)
		_ = s.dbStore.UpdateTaskStatus(ctx, t.ID, "active") // release claim
		s.l1Mu.Lock()
		s.l1Queue = append(s.l1Queue, cronTask{task: t, enqueued: time.Now()})
		s.l1Mu.Unlock()
		s.l1Cond.Signal()
		return
	}

	s.runL1Task(ctx, t, l1Session)
}

// runL1Task executes a single L1 task on the given session.
func (s *Scheduler) runL1Task(ctx context.Context, t Task, l1Session Session) {
	start := time.Now()

	prompt := s.buildTaskPrompt(t)
	cronCtx := s.buildCronContext(t)
	ch, err := l1Session.AskIsolated(cronCtx, prompt)
	if err != nil {
		s.logger.Error(logger.CatApp, "cron: L1 task execution failed to start", "task_id", t.ID, "err", err)
		_ = s.dbStore.UpdateTaskStatus(ctx, t.ID, "active")
		return
	}

	replyText, mediaFiles := drainEvents(ch)
	duration := time.Since(start)
	s.logger.Info(logger.CatApp, "cron: L1 task completed", "task_id", t.ID, "duration_ms", duration.Milliseconds())

	// Invoke QQ bot callback if applicable.
	s.invokeCompletionHook(t, replyText, mediaFiles)

	s.updateTaskAfterExecution(ctx, t)
}

// executeL2Task handles tasks targeting an L2 team. Creates a temporary
// L2 session, executes the task, and queues the result for L1 delivery.
func (s *Scheduler) executeL2Task(t Task) {
	ctx := context.Background()

	// Two-phase commit: claim the task.
	claimed, err := s.dbStore.ClaimTask(ctx, t.ID)
	if err != nil {
		s.logger.Error(logger.CatApp, "cron: failed to claim L2 task", "task_id", t.ID, "err", err)
		return
	}
	if !claimed {
		s.logger.Debug(logger.CatApp, "cron: L2 task already claimed, skipping", "task_id", t.ID)
		return
	}

	// Panic recovery.
	defer func() {
		if panicValue := recover(); panicValue != nil {
			s.logger.Error(logger.CatApp, "cron: L2 task execution panicked", "task_id", t.ID, "panic", panicValue)
			_ = s.dbStore.UpdateTaskStatus(ctx, t.ID, "active")
		}
	}()

	// Get session for this L2 team.
	l2Session, isNew, cleanup, err := s.sessionMgr.GetSession(ctx, t.TargetAgent, t.ID)
	if err != nil {
		s.logger.Error(logger.CatApp, "cron: failed to get L2 session", "task_id", t.ID, "target", t.TargetAgent, "err", err)
		_ = s.dbStore.UpdateTaskStatus(ctx, t.ID, "active")
		return
	}
	if isNew && cleanup != nil {
		defer cleanup()
	}

	if l2Session == nil {
		s.logger.Warn(logger.CatApp, "cron: L2 session is nil", "task_id", t.ID, "target", t.TargetAgent)
		_ = s.dbStore.UpdateTaskStatus(ctx, t.ID, "active")
		return
	}

	start := time.Now()

	prompt := s.buildTaskPrompt(t)
	cronCtx := s.buildCronContext(t)
	ch, err := l2Session.AskIsolated(cronCtx, prompt)
	if err != nil {
		s.logger.Error(logger.CatApp, "cron: L2 task execution failed to start", "task_id", t.ID, "err", err)
		_ = s.dbStore.UpdateTaskStatus(ctx, t.ID, "active")
		return
	}

	replyText, mediaFiles := drainEvents(ch)
	duration := time.Since(start)
	s.logger.Info(logger.CatApp, "cron: L2 task completed", "task_id", t.ID,
		"target", t.TargetAgent, "duration_ms", duration.Milliseconds())

	// Invoke QQ bot callback if applicable.
	s.invokeCompletionHook(t, replyText, mediaFiles)

	// Queue the result for L1 delivery.
	s.enqueueL2Result(t, replyText, mediaFiles)

	s.updateTaskAfterExecution(ctx, t)
}

// buildTaskPrompt builds the prompt for a task, optionally enriched with recalled memories.
func (s *Scheduler) buildTaskPrompt(t Task) string {
	basePrompt := buildCronPrompt(t)

	if s.memoryEngine != nil && s.buildRecalledFn != nil {
		recalled := s.buildRecalledFn(context.Background(), t.Instruction, s.memoryEngine, s.logger)
		if recalled != "" {
			// Prepend recalled memories to the base prompt.
			basePrompt = recalled + "\n\n" + basePrompt
		}
	}

	return basePrompt
}

// buildCronContext creates a context with bypass-confirm and QBot flags.
func (s *Scheduler) buildCronContext(t Task) context.Context {
	isQBot := t.QQOpenID != "" || t.QQChatID != "" || t.QQSource != 0
	cronCtx := iface.ContextWithBypassConfirm(context.Background())
	cronCtx = iface.ContextWithIsQBot(cronCtx, isQBot)
	return cronCtx
}

// drainEvents drains an AgentEvent channel, collecting reply text and SendFile media.
func drainEvents(ch <-chan iface.AgentEvent) (string, []SendFileMedia) {
	var contentBuf strings.Builder
	var mediaFiles []SendFileMedia
	for ev := range ch {
		if consumer, ok := ev.(iface.EventConsumer); ok {
			if delta, ok := consumer.ContentDelta(); ok {
				contentBuf.WriteString(delta)
			}
		}

		rv := reflect.ValueOf(ev)
		if rv.Type().Name() == "ToolExecDoneEvent" {
			name := rv.FieldByName("Name").String()
			result := rv.FieldByName("Result").String()
			if name == "SendFile" && result != "" {
				if m := parseSendFileMedia(result); m != nil {
					mediaFiles = append(mediaFiles, *m)
				}
			}
		}
	}
	return contentBuf.String(), mediaFiles
}

// enqueueL2Result adds a completed L2 task result to the delivery queue.
func (s *Scheduler) enqueueL2Result(t Task, reply string, mediaFiles []SendFileMedia) {
	s.resultMu.Lock()
	s.resultQueue = append(s.resultQueue, pendingResult{
		task:       t,
		reply:      reply,
		mediaFiles: mediaFiles,
		completed:  time.Now(),
	})
	s.resultMu.Unlock()
	s.resultCond.Signal()
}

// l1QueueLoop runs in a background goroutine. It processes queued L1 tasks
// one at a time when L1 becomes idle.
func (s *Scheduler) l1QueueLoop() {
	s.l1Mu.Lock()
	defer s.l1Mu.Unlock()

	for {
		// Check if stopped.
		s.mu.Lock()
		stopped := s.stopped
		s.mu.Unlock()
		if stopped {
			return
		}

		for len(s.l1Queue) == 0 && !stopped {
			s.l1Cond.Wait()
			s.mu.Lock()
			stopped = s.stopped
			s.mu.Unlock()
		}
		if stopped {
			return
		}

		ct := s.l1Queue[0]
		s.l1Queue = s.l1Queue[0:]
		s.l1Mu.Unlock()

		// Wait for L1 to be idle.
		l1Session := s.sessionMgr.Session()
		if l1Session != nil {
			for !l1Session.Idle() {
				time.Sleep(100 * time.Millisecond)
				s.mu.Lock()
				stopped = s.stopped
				s.mu.Unlock()
				if stopped {
					s.l1Mu.Lock()
					return
				}
			}
			s.runL1Task(context.Background(), ct.task, l1Session)
		}

		s.l1Mu.Lock()
	}
}

// l1ResultLoop runs in a background goroutine. It drains the L2 result queue
// and delivers each result to L1 via AskStream (so the user sees it).
func (s *Scheduler) l1ResultLoop() {
	s.resultMu.Lock()
	defer s.resultMu.Unlock()

	for {
		s.mu.Lock()
		stopped := s.stopped
		s.mu.Unlock()
		if stopped {
			return
		}

		for len(s.resultQueue) == 0 && !stopped {
			s.resultCond.Wait()
			s.mu.Lock()
			stopped = s.stopped
			s.mu.Unlock()
		}
		if stopped {
			return
		}

		pr := s.resultQueue[0]
		s.resultQueue = s.resultQueue[1:]
		s.resultMu.Unlock()

		// Deliver to L1 when idle.
		l1Session := s.sessionMgr.Session()
		if l1Session != nil {
			for !l1Session.Idle() {
				time.Sleep(100 * time.Millisecond)
				s.mu.Lock()
				stopped = s.stopped
				s.mu.Unlock()
				if stopped {
					s.resultMu.Lock()
					return
				}
			}
			s.deliverResultToL1(l1Session, pr)
		}

		s.resultMu.Lock()
	}
}

// deliverResultToL1 sends a completed L2 task result to L1 via AskStream
// so the result appears in the conversation and the user can see it.
func (s *Scheduler) deliverResultToL1(l1Session Session, pr pendingResult) {
	prompt := fmt.Sprintf(
		"[Scheduled Task Completed]\n"+
			"Task ID: %s\n"+
			"Target Agent: %s\n"+
			"Schedule: %s\n"+
			"Completed at: %s\n"+
			"\nThe following scheduled task has been executed. Please review and present the result to the user:\n\n%s",
		pr.task.ID, pr.task.TargetAgent, pr.task.Expression,
		pr.completed.Format("2006-01-02 15:04:05"), pr.reply,
	)

	ctx := iface.ContextWithBypassConfirm(context.Background())
	ch, err := l1Session.AskStream(ctx, prompt)
	if err != nil {
		s.logger.Warn(logger.CatApp, "cron: failed to deliver L2 result to L1", "task_id", pr.task.ID, "err", err)
		return
	}
	// Drain events silently (the result is already in CW/WS via AskStream).
	for range ch {
	}
}

// invokeCompletionHook calls the QQ bot callback if registered.
func (s *Scheduler) invokeCompletionHook(t Task, reply string, mediaFiles []SendFileMedia) {
	s.mu.Lock()
	hook := s.onTaskCompleted
	s.mu.Unlock()
	if hook != nil && t.QQSource != -1 {
		hook(context.Background(), t, reply, mediaFiles)
	}
}

// updateTaskAfterExecution updates DB timestamps after successful execution.
func (s *Scheduler) updateTaskAfterExecution(ctx context.Context, t Task) {
	if t.IsOneTime() {
		_ = s.dbStore.MarkCompleted(ctx, t.ID)
	} else {
		next, _ := NextTrigger(t.Expression, time.Now())
		_ = s.dbStore.UpdateNextRun(ctx, t.ID, time.Now(), next)
	}
}

// buildCronPrompt wraps a task's instruction with a scheduler-context header.
func buildCronPrompt(t Task) string {
	triggerTime := time.Now().Format("2006-01-02 15:04:05")
	scheduleDesc := t.Expression
	if t.IsOneTime() {
		scheduleDesc = "one-time task"
	}
	return fmt.Sprintf(
		"[SCHEDULED TASK EXECUTION]\n"+
			"Task ID: %s\n"+
			"Schedule: %s\n"+
			"Triggered at: %s\n"+
			"\nIMPORTANT: This message is automatically triggered by the scheduler — NOT a user request. "+
			"Do NOT call schedule_task or create any new scheduled tasks. "+
			"Simply execute the following instruction directly:\n\n%s",
		t.ID, scheduleDesc, triggerTime, t.Instruction,
	)
}

// parseSendFileMedia extracts media metadata from a SendFile tool result JSON.
func parseSendFileMedia(raw string) *SendFileMedia {
	var r struct {
		Status   string `json:"status"`
		FileName string `json:"file_name"`
		FileType string `json:"file_type"`
		Path     string `json:"path"`
		URL      string `json:"url"`
	}
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return nil
	}
	if r.Status != "success" {
		return nil
	}

	ftype := 4 // default file
	switch r.FileType {
	case "image":
		ftype = 1
	case "video":
		ftype = 2
	case "voice":
		ftype = 3
	}

	b64 := ""
	if r.Path != "" {
		if data, err := os.ReadFile(r.Path); err == nil {
			b64 = base64.StdEncoding.EncodeToString(data)
		}
	}

	return &SendFileMedia{
		FileType:   ftype,
		URL:        r.URL,
		Base64Data: b64,
		FileName:   r.FileName,
	}
}
