package cron

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	robfig "github.com/robfig/cron/v3"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
)

// Session defines the interface required by the Scheduler to trigger tasks.
type Session interface {
	Idle() bool
	QueueMessage(prompt string)
	AskStream(ctx context.Context, prompt string) (<-chan iface.AgentEvent, error)
	// AskIsolated executes a prompt in a clean context (no conversation history,
	// no writes to the session's ContextWindow or timeline).
	AskIsolated(ctx context.Context, prompt string) (<-chan iface.AgentEvent, error)
	// HasNotifyChannel reports whether the session has a configured notification
	// channel, independent of whether a live sender is currently available.
	HasNotifyChannel() bool
	// SendViaChannel delivers a text notification through the session's bound
	// channel bridge (QQ/WeChat).
	SendViaChannel(ctx context.Context, text string) error
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

// ResolvedModel is the concrete model configuration for one scheduled run.
type ResolvedModel struct {
	Params            iface.ModelOverrideParams
	RequestedTaskType string
	UsedFallback      bool
	FallbackReason    string
}

// ModelResolver resolves the latest configured model for a persisted task type.
type ModelResolver func(taskType string) (ResolvedModel, error)

type modelRoutedSession interface {
	AskIsolatedWithModel(ctx context.Context, prompt string, params *iface.ModelOverrideParams) (<-chan iface.AgentEvent, error)
	AskStreamWithModel(ctx context.Context, prompt string, params *iface.ModelOverrideParams) (<-chan iface.AgentEvent, error)
}

var errTaskModelResolution = errors.New("scheduled task model resolution failed")

// cronTask wraps a Task with its execution metadata.
type cronTask struct {
	task     Task
	enqueued time.Time
}

// CronStartCallback is called when a cron task execution begins.
type CronStartCallback func(taskID, taskTitle string)

// CronDoneCallback is called when a cron task execution completes.
// taskID and taskTitle identify the task; success indicates the result;
// summary is a brief human-readable description (first line of reply or error).
type CronDoneCallback func(taskID, taskTitle string, success bool, summary string)

// Scheduler manages executing scheduled tasks (both cron and timer-based) in the background.
type Scheduler struct {
	dbStore    *DBStore
	sessionMgr SessionManager
	logger     *logger.Logger
	cron       *robfig.Cron
	workDir    string // base directory for cron log storage

	modelResolver ModelResolver

	// L1 task queue: serializes L1-targeted cron tasks
	l1Queue []cronTask
	l1Mu    sync.Mutex
	l1Cond  *sync.Cond

	mu          sync.Mutex
	entries     map[string]robfig.EntryID
	timers      map[string]*time.Timer
	oneTimeRuns map[string]string
	stopped     bool

	// OnTaskStart is called when a cron task begins execution.
	// Set from the server layer to integrate with WebSocket notifications.
	OnTaskStart CronStartCallback

	// OnTaskComplete is called when a cron task finishes execution.
	// Set from the server layer to integrate with WebSocket notifications.
	OnTaskComplete CronDoneCallback
}

// SetModelResolver configures per-run task-type model selection.
func (s *Scheduler) SetModelResolver(resolver ModelResolver) {
	s.modelResolver = resolver
}

// SetWorkDir configures the base directory for cron execution logs.
func (s *Scheduler) SetWorkDir(dir string) {
	s.workDir = dir
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
				robfig.Minute|robfig.Hour|robfig.Dom|robfig.Month|robfig.Dow,
			)),
			robfig.WithChain(robfig.SkipIfStillRunning(robfig.DiscardLogger)),
		),
		entries:     make(map[string]robfig.EntryID),
		timers:      make(map[string]*time.Timer),
		oneTimeRuns: make(map[string]string),
	}
	s.l1Cond = sync.NewCond(&s.l1Mu)
	return s
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
	if !s.claimOneTimeRun(t) {
		s.logger.Info(logger.CatApp, "cron: duplicate one-time task trigger skipped", "task_id", t.ID)
		return
	}
	s.logger.Info(logger.CatApp, "cron: task execution triggered", "task_id", t.ID,
		"instruction", t.Instruction, "target_agent", t.TargetAgent)

	if isL1Target(t) {
		s.executeL1Task(t)
	} else {
		s.executeL2Task(t)
	}
}

// claimOneTimeRun makes a one-time task idempotent for a specific scheduled
// instant. Updating the same task to a different instant permits a new run.
func (s *Scheduler) claimOneTimeRun(t Task) bool {
	if !t.IsOneTime() {
		return true
	}
	key := t.NextRunAt.UTC().Format(time.RFC3339Nano)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.oneTimeRuns[t.ID] == key {
		return false
	}
	s.oneTimeRuns[t.ID] = key
	return true
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

	// Panic recovery: catch panic, log, record history, return to 'active' for retry.
	defer func() {
		if panicValue := recover(); panicValue != nil {
			s.logger.Error(logger.CatApp, "cron: L1 task execution panicked", "task_id", t.ID, "panic", panicValue)
			_ = s.dbStore.UpdateTaskStatus(ctx, t.ID, "active")
			s.recordExecution(ctx, t, ResolvedModel{}, time.Now(), drainEventsResult{},
				fmt.Sprintf("panic: %v", panicValue), "panic")
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

// notifyTaskStarted is a helper that calls OnTaskStart if set.
func (s *Scheduler) notifyTaskStarted(t Task) {
	if s.OnTaskStart == nil {
		return
	}
	s.OnTaskStart(t.ID, t.Title)
}

// notifyTaskComplete is a helper that calls OnTaskComplete if set, extracting
// a short summary from the reply text or error message.
// When summary is empty (e.g. replyText starts with "\n" or agent produced
// only tool calls with no text), a fallback message is used so the desktop
// notification always has a visible body.
func (s *Scheduler) notifyTaskComplete(t Task, success bool, summary string) {
	if s.OnTaskComplete == nil {
		return
	}
	if summary == "" {
		if success {
			summary = "Execution completed"
		} else {
			summary = "Execution failed"
		}
	}
	s.OnTaskComplete(t.ID, t.Title, success, summary)
}

// firstLineSummary returns at most the first line of s, truncated to ~100 runes.
func firstLineSummary(s string) string {
	if s == "" {
		return ""
	}
	line, _, _ := strings.Cut(s, "\n")
	// Truncate to ~100 characters.
	if len(line) > 100 {
		line = line[:100] + "..."
	}
	return line
}

const continuationPrompt = "[SYSTEM NOTICE] The previous streaming response was interrupted due to a network error. System connection has been restored. Please resume and complete the unfinished task based on the above tool calls and intermediate results."

// runL1Task executes a single L1 task on the given session.
func (s *Scheduler) runL1Task(ctx context.Context, t Task, l1Session Session) {
	start := time.Now()

	s.notifyTaskStarted(t)

	cronCtx := s.buildCronContext(t)

	resolved, ch, err := s.askWithTaskModel(cronCtx, t, l1Session)
	if err != nil {
		s.logger.Error(logger.CatApp, "cron: L1 task execution failed to start", "task_id", t.ID, "err", err)
		if errors.Is(err, errTaskModelResolution) {
			_ = s.dbStore.UpdateTaskStatus(ctx, t.ID, "failed")
		} else {
			s.updateTaskAfterExecution(ctx, t)
		}
		s.recordExecution(ctx, t, resolved, start, drainEventsResult{}, err.Error(), "failed")
		s.notifyTaskComplete(t, false, err.Error())
		return
	}

	result, drainErr := s.drainEventsWithTimeline(ch, t, uuid.New().String())

	// ── 1-time 10s Retry Logic ──
	if drainErr != nil {
		s.logger.Warn(logger.CatApp, "cron: L1 task drain error, preparing 10s retry",
			"task_id", t.ID, "tool_calls", result.toolCallCount, "err", drainErr)
		time.Sleep(10 * time.Second)

		var retrySess Session
		var retryPrompt string

		if result.toolCallCount > 0 {
			// iter > 0: Reuse existing session with tool outputs preserved in context window.
			retrySess = l1Session
			retryPrompt = continuationPrompt
		} else {
			// iter == 0: Get a fresh session to avoid duplicate instructions in context.
			freshSess := s.sessionMgr.Session()
			if freshSess != nil {
				retrySess = freshSess
			} else {
				retrySess = l1Session
			}
			retryPrompt = s.buildTaskPrompt(t)
		}

		retryCtx := s.buildCronContext(t)
		_, retryCh, retryStartErr := s.askWithTaskModelPrompt(retryCtx, t, retrySess, retryPrompt)
		if retryStartErr == nil {
			retryResult, retryDrainErr := s.drainEventsWithTimeline(retryCh, t, uuid.New().String()+"-retry")
			if retryDrainErr == nil {
				result = retryResult
				drainErr = nil
				s.logger.Info(logger.CatApp, "cron: L1 task retry succeeded", "task_id", t.ID)
			} else {
				s.logger.Error(logger.CatApp, "cron: L1 task retry failed", "task_id", t.ID, "err", retryDrainErr)
				drainErr = retryDrainErr
			}
		} else {
			s.logger.Error(logger.CatApp, "cron: L1 task retry failed to start", "task_id", t.ID, "err", retryStartErr)
		}
	}

	duration := time.Since(start)
	s.logger.Info(logger.CatApp, "cron: L1 task completed", "task_id", t.ID, "duration_ms", duration.Milliseconds())

	// Record execution history.
	status := "success"
	errMsg := ""
	if drainErr != nil {
		status = "failed"
		errMsg = drainErr.Error()
	}
	s.recordExecution(ctx, t, resolved, start, result, errMsg, status)
	if status != "success" {
		s.notifyTaskComplete(t, false, "Execution failed: "+errMsg)
	} else {
		s.notifyTaskComplete(t, true, firstLineSummary(result.replyText))
	}

	// Deliver result through the session's bound channel (QQ/WeChat).
	if status == "success" && result.replyText != "" {
		l1Session.SendViaChannel(ctx, result.replyText)
	}

	if drainErr != nil {
		s.logger.Error(logger.CatApp, "cron: L1 task drain error", "task_id", t.ID, "err", drainErr)
	}

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
			s.recordExecution(ctx, t, ResolvedModel{}, time.Now(), drainEventsResult{},
				fmt.Sprintf("panic: %v", panicValue), "panic")
			s.notifyTaskComplete(t, false, fmt.Sprintf("panic: %v", panicValue))
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

	s.notifyTaskStarted(t)

	cronCtx := s.buildCronContext(t)
	resolved, ch, err := s.askWithTaskModel(cronCtx, t, l2Session)
	if err != nil {
		s.logger.Error(logger.CatApp, "cron: L2 task execution failed to start", "task_id", t.ID, "err", err)
		if errors.Is(err, errTaskModelResolution) {
			_ = s.dbStore.UpdateTaskStatus(ctx, t.ID, "failed")
		} else {
			s.updateTaskAfterExecution(ctx, t)
		}
		s.recordExecution(ctx, t, resolved, start, drainEventsResult{}, err.Error(), "failed")
		s.notifyTaskComplete(t, false, err.Error())
		return
	}

	result, drainErr := s.drainEventsWithTimeline(ch, t, uuid.New().String())

	// ── 1-time 10s Retry Logic ──
	if drainErr != nil {
		s.logger.Warn(logger.CatApp, "cron: L2 task drain error, preparing 10s retry",
			"task_id", t.ID, "tool_calls", result.toolCallCount, "err", drainErr)
		time.Sleep(10 * time.Second)

		var retrySess Session
		var retryPrompt string

		if result.toolCallCount > 0 {
			// iter > 0: Reuse existing session with tool outputs preserved in context window.
			retrySess = l2Session
			retryPrompt = continuationPrompt
		} else {
			// iter == 0: Get a fresh session to avoid duplicate instructions in context.
			if isNew && cleanup != nil {
				cleanup()
			}
			freshSess, freshIsNew, freshCleanup, freshErr := s.sessionMgr.GetSession(ctx, t.TargetAgent, t.ID)
			if freshErr == nil && freshSess != nil {
				retrySess = freshSess
				if freshIsNew && freshCleanup != nil {
					defer freshCleanup()
				}
			} else {
				retrySess = l2Session
			}
			retryPrompt = s.buildTaskPrompt(t)
		}

		retryCtx := s.buildCronContext(t)
		_, retryCh, retryStartErr := s.askWithTaskModelPrompt(retryCtx, t, retrySess, retryPrompt)
		if retryStartErr == nil {
			retryResult, retryDrainErr := s.drainEventsWithTimeline(retryCh, t, uuid.New().String()+"-retry")
			if retryDrainErr == nil {
				result = retryResult
				drainErr = nil
				s.logger.Info(logger.CatApp, "cron: L2 task retry succeeded", "task_id", t.ID)
			} else {
				s.logger.Error(logger.CatApp, "cron: L2 task retry failed", "task_id", t.ID, "err", retryDrainErr)
				drainErr = retryDrainErr
			}
		} else {
			s.logger.Error(logger.CatApp, "cron: L2 task retry failed to start", "task_id", t.ID, "err", retryStartErr)
		}
	}

	replyText := result.replyText
	duration := time.Since(start)
	s.logger.Info(logger.CatApp, "cron: L2 task completed", "task_id", t.ID,
		"target", t.TargetAgent, "duration_ms", duration.Milliseconds())

	// Record execution history.
	status := "success"
	errMsg := ""
	if drainErr != nil {
		status = "failed"
		errMsg = drainErr.Error()
	}
	s.recordExecution(ctx, t, resolved, start, result, errMsg, status)
	if status != "success" {
		s.notifyTaskComplete(t, false, "Execution failed: "+errMsg)
	} else {
		s.notifyTaskComplete(t, true, firstLineSummary(replyText))
	}

	// Deliver through L2's bound channel. If L2 has no configured notification
	// channel, fall back to L1's channel. A configured-but-unavailable L2 sender
	// is not a fallback case: it is an operational delivery failure.
	if status == "success" && replyText != "" {
		s.deliverL2ResultViaChannel(ctx, t, l2Session, replyText)
	}

	if drainErr != nil {
		s.logger.Error(logger.CatApp, "cron: L2 task drain error", "task_id", t.ID, "err", drainErr)
	}

	s.updateTaskAfterExecution(ctx, t)
}

func (s *Scheduler) deliverL2ResultViaChannel(ctx context.Context, t Task, l2Session Session, replyText string) {
	if !l2Session.HasNotifyChannel() {
		l1Session := s.sessionMgr.Session()
		if l1Session == nil {
			s.logger.Warn(logger.CatApp, "cron: L2 notification fallback skipped, no L1 session", "task_id", t.ID)
			return
		}
		if err := l1Session.SendViaChannel(ctx, replyText); err != nil {
			s.logger.Warn(logger.CatApp, "cron: L2 notification fallback to L1 failed", "task_id", t.ID, "err", err)
		}
		return
	}
	err := l2Session.SendViaChannel(ctx, replyText)
	if err != nil {
		s.logger.Warn(logger.CatApp, "cron: L2 channel notification failed", "task_id", t.ID, "err", err)
	}
}

func (s *Scheduler) askWithTaskModelPrompt(ctx context.Context, t Task, sess Session, prompt string) (ResolvedModel, <-chan iface.AgentEvent, error) {
	if s.modelResolver == nil {
		ch, err := sess.AskIsolated(ctx, prompt)
		return ResolvedModel{}, ch, err
	}
	resolved, err := s.modelResolver(t.TaskType)
	if err != nil {
		return ResolvedModel{}, nil, fmt.Errorf("%w: resolve task type %s: %v", errTaskModelResolution, t.TaskType, err)
	}
	routed, ok := sess.(modelRoutedSession)
	if !ok {
		return resolved, nil, fmt.Errorf("%w: session does not support model routing", errTaskModelResolution)
	}
	s.logger.Info(logger.CatApp, "cron: resolved task model",
		"task_id", t.ID,
		"title", t.Title,
		"task_type", t.TaskType,
		"requested_task_type", resolved.RequestedTaskType,
		"provider_id", resolved.Params.ProviderID,
		"model_id", resolved.Params.ModelID,
		"used_fallback", resolved.UsedFallback,
		"fallback_reason", resolved.FallbackReason,
	)
	ch, err := routed.AskIsolatedWithModel(ctx, prompt, &resolved.Params)
	return resolved, ch, err
}

func (s *Scheduler) askWithTaskModel(ctx context.Context, t Task, sess Session) (ResolvedModel, <-chan iface.AgentEvent, error) {
	return s.askWithTaskModelPrompt(ctx, t, sess, s.buildTaskPrompt(t))
}

// askWithTaskModelStream is like askWithTaskModel but uses AskStream so the
// result writes to the session timeline and triggers channel bridge delivery.
func (s *Scheduler) askWithTaskModelStreamPrompt(ctx context.Context, t Task, sess Session, prompt string) (ResolvedModel, <-chan iface.AgentEvent, error) {
	if s.modelResolver == nil {
		ch, err := sess.AskStream(ctx, prompt)
		return ResolvedModel{}, ch, err
	}
	resolved, err := s.modelResolver(t.TaskType)
	if err != nil {
		return ResolvedModel{}, nil, fmt.Errorf("%w: resolve task type %s: %v", errTaskModelResolution, t.TaskType, err)
	}
	routed, ok := sess.(modelRoutedSession)
	if !ok {
		return resolved, nil, fmt.Errorf("%w: session does not support model routing", errTaskModelResolution)
	}
	s.logger.Info(logger.CatApp, "cron: resolved task model",
		"task_id", t.ID,
		"title", t.Title,
		"task_type", t.TaskType,
		"requested_task_type", resolved.RequestedTaskType,
		"provider_id", resolved.Params.ProviderID,
		"model_id", resolved.Params.ModelID,
		"used_fallback", resolved.UsedFallback,
		"fallback_reason", resolved.FallbackReason,
	)
	ch, err := routed.AskStreamWithModel(ctx, prompt, &resolved.Params)
	return resolved, ch, err
}

func (s *Scheduler) askWithTaskModelStream(ctx context.Context, t Task, sess Session) (ResolvedModel, <-chan iface.AgentEvent, error) {
	return s.askWithTaskModelStreamPrompt(ctx, t, sess, s.buildTaskPrompt(t))
}

// buildTaskPrompt builds the prompt for a task.
func (s *Scheduler) buildTaskPrompt(t Task) string {
	return buildCronPrompt(t)
}

// buildCronContext creates a context with bypass-confirm flag.
func (s *Scheduler) buildCronContext(t Task) context.Context {
	cronCtx := iface.ContextWithBypassConfirm(context.Background())
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

// drainEventsResult holds the output of draining an agent event channel into a timeline.
type drainEventsResult struct {
	replyText     string
	mediaFiles    []SendFileMedia
	timelineDir   string // relative path from workDir: logs/cron/<taskID>/<execID>
	toolCallCount int
}

// drainEventsWithTimeline drains an agent event channel and writes every event
// (content, reasoning, tool calls, tool results) to a timeline file so the full
// execution can be replayed later. Returns accumulated reply text, media files,
// and the timeline directory path.
func (s *Scheduler) drainEventsWithTimeline(ch <-chan iface.AgentEvent, t Task, execID string) (drainEventsResult, error) {
	var result drainEventsResult

	// Determine timeline directory.
	var tlDir string
	if s.workDir == "" {
		tlDir = filepath.Join(os.TempDir(), "soloqueue-cron", t.ID, execID)
	} else {
		tlDir = filepath.Join(s.workDir, "logs", "cron", t.ID, execID)
	}
	if err := os.MkdirAll(tlDir, 0755); err != nil {
		return result, fmt.Errorf("create cron timeline dir: %w", err)
	}

	agentID := "cron-task-" + t.ID
	tl, err := timeline.NewWriter(tlDir, "timeline", 50*1024*1024, 15)
	if err != nil {
		return result, fmt.Errorf("create cron timeline writer: %w", err)
	}
	defer tl.Close()

	// Write the user prompt (task instruction).
	prompt := s.buildTaskPrompt(t)
	_ = tl.AppendMessage(&timeline.MessagePayload{
		Role:    "user",
		Content: prompt,
		AgentID: agentID,
	})

	// ── Event processing state ──
	var (
		contentBuf   strings.Builder
		reasoningBuf strings.Builder
		replyBuf     strings.Builder // accumulates full reply text (not reset by flush)
	)

	flushAssistant := func(content, reasoning string, toolCalls []timeline.ToolCallRec) {
		_ = tl.AppendMessage(&timeline.MessagePayload{
			Role:             "assistant",
			Content:          content,
			ReasoningContent: reasoning,
			ToolCalls:        toolCalls,
			AgentID:          agentID,
		})
		contentBuf.Reset()
		reasoningBuf.Reset()
	}

	appendDoneContent := func(content string) {
		if content == "" {
			return
		}
		cur := contentBuf.String()
		if cur != "" && strings.HasPrefix(content, cur) {
			suffix := content[len(cur):]
			if suffix != "" {
				contentBuf.WriteString(suffix)
				replyBuf.WriteString(suffix)
			}
		} else if cur == "" {
			contentBuf.WriteString(content)
			replyBuf.WriteString(content)
		} else if !strings.Contains(cur, content) {
			contentBuf.WriteString(content)
			replyBuf.WriteString(content)
		}
	}

	for ev := range ch {
		// Use iface.EventConsumer for safe cross-package content extraction.
		if consumer, ok := ev.(iface.EventConsumer); ok {
			if delta, ok := consumer.ContentDelta(); ok {
				contentBuf.WriteString(delta)
				replyBuf.WriteString(delta)
			}
		}

		rv := reflect.ValueOf(ev)
		for rv.Kind() == reflect.Ptr {
			rv = rv.Elem()
		}
		typeName := rv.Type().Name()

		switch typeName {
		case "ReasoningDeltaEvent":
			delta := rv.FieldByName("Delta").String()
			reasoningBuf.WriteString(delta)

		case "ToolExecStartEvent", "testToolExecStart":
			result.toolCallCount++
			callID := rv.FieldByName("CallID").String()
			name := rv.FieldByName("Name").String()
			args := rv.FieldByName("Args").String()
			if callID != "" {
				flushAssistant(contentBuf.String(), reasoningBuf.String(), []timeline.ToolCallRec{
					{ID: callID, Type: "function", Name: name, Arguments: args},
				})
			}

		case "ToolExecDoneEvent":
			callID := rv.FieldByName("CallID").String()
			name := rv.FieldByName("Name").String()
			toolResult := rv.FieldByName("Result").String()
			errField := rv.FieldByName("Err")
			if errField.IsValid() && !errField.IsNil() {
				toolResult = "error: " + errField.Elem().String()
			}
			_ = tl.AppendMessage(&timeline.MessagePayload{
				Role:        "tool",
				Content:     toolResult,
				Name:        name,
				ToolCallID:  callID,
				IsEphemeral: len(toolResult) > 2000,
				AgentID:     agentID,
			})
			// Extract SendFile media.
			if name == "SendFile" && toolResult != "" {
				if m := parseSendFileMedia(toolResult); m != nil {
					result.mediaFiles = append(result.mediaFiles, *m)
				}
			}

		case "DoneEvent":
			content := rv.FieldByName("Content").String()
			reasoning := rv.FieldByName("ReasoningContent").String()
			if reasoning != "" && reasoningBuf.Len() == 0 {
				reasoningBuf.WriteString(reasoning)
			}
			appendDoneContent(content)
			flushAssistant(contentBuf.String(), reasoningBuf.String(), nil)
			continue

		case "ErrorEvent":
			errField := rv.FieldByName("Err")
			if errField.IsValid() && !errField.IsNil() {
				return result, fmt.Errorf("agent error: %v", errField.Elem().Interface())
			}
			return result, errors.New("agent error: unknown")
		}

		// Fallback: use EventConsumer for Done and Error if reflection didn't match.
		if consumer, ok := ev.(iface.EventConsumer); ok {
			if content, ok := consumer.DoneContent(); ok {
				appendDoneContent(content)
				flushAssistant(contentBuf.String(), reasoningBuf.String(), nil)
			}
			if errVal, ok := consumer.Error(); ok {
				return result, fmt.Errorf("agent error: %v", errVal)
			}
		}
	}

	// Write completion marker.
	_ = tl.AppendControl(&timeline.ControlPayload{
		Action: "complete",
		Reason: "cron_task_done",
	})

	result.replyText = replyBuf.String()
	result.timelineDir = filepath.Join("logs", "cron", t.ID, execID)
	return result, nil
}

// recordExecution writes an execution history record to the database.
func (s *Scheduler) recordExecution(ctx context.Context, t Task, resolved ResolvedModel, start time.Time, result drainEventsResult, errMsg string, status string) {
	if s.dbStore == nil {
		return
	}
	_ = s.dbStore.RecordExecution(ctx, ExecutionRecord{
		ID:            uuid.New().String(),
		TaskID:        t.ID,
		ExecutedAt:    start,
		CompletedAt:   time.Now(),
		DurationMs:    time.Since(start).Milliseconds(),
		Status:        status,
		ResultSummary: result.replyText,
		ErrorMessage:  errMsg,
		TaskType:      t.TaskType,
		TargetAgent:   t.TargetAgent,
		ModelID:       resolved.Params.ModelID,
		ProviderID:    resolved.Params.ProviderID,
		TimelineDir:   result.timelineDir,
	})
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
		s.l1Queue = s.l1Queue[1:]
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
			"Title: %s\n"+
			"Task type: %s\n"+
			"Schedule: %s\n"+
			"Triggered at: %s\n"+
			"\nIMPORTANT: This message is automatically triggered by the scheduler — NOT a user request. "+
			"Do NOT call create_cron_job or create any new cron jobs. "+
			"Simply execute the following instruction directly:\n\n%s",
		t.ID, t.Title, t.TaskType, scheduleDesc, triggerTime, t.Instruction,
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
