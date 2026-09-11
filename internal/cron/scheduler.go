package cron

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	robfig "github.com/robfig/cron/v3"

	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetryctx"
	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
	"github.com/xiaobaitu/soloqueue/internal/runwatch"
)

// Session defines the interface required by the Scheduler to trigger tasks.
type Session interface {
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
	SendMediaViaChannel(ctx context.Context, media []channel.OutboundMedia) error
}

// SessionManager provides sessions for scheduled task execution.
type SessionManager interface {
	// Session returns the L1 session (may be nil if not initialized).
	Session() Session

	// GetSession creates an isolated temporary session for the requested L1 or
	// L2 target. isNew=true indicates cleanup must be called after execution.
	// The caller MUST call cleanup() when done with a new session.
	GetSession(ctx context.Context, teamID, taskID string) (sess Session, isNew bool, cleanup func(), err error)
}

// SendFileMedia holds metadata about a file the agent sent via the SendFile tool.
type SendFileMedia = channel.OutboundMedia

// ResolvedModel is the concrete model configuration for one scheduled run.
type ResolvedModel struct {
	Params            iface.ModelOverrideParams
	RequestedTaskType string
	UsedFallback      bool
	FallbackReason    string
}

// CronResultArtifact is a model-authored reference to an output produced by the task.
type CronResultArtifact struct {
	Name string `json:"name"`
	Ref  string `json:"ref"`
}

// CronResultV1 is the scheduler-owned result boundary for every Cron run. The
// model owns only Content and Artifacts; all remaining fields are project-owned.
type CronResultV1 struct {
	Version     string               `json:"version"`
	TaskID      string               `json:"task_id"`
	RunID       string               `json:"run_id"`
	Title       string               `json:"title"`
	Status      string               `json:"status"`
	Content     string               `json:"content"`
	Artifacts   []CronResultArtifact `json:"artifacts"`
	Error       *string              `json:"error"`
	GeneratedAt string               `json:"generated_at"`
}

// ModelResolver resolves the latest configured model for a persisted task type.
type ModelResolver func(taskType string) (ResolvedModel, error)

type modelRoutedSession interface {
	AskIsolatedWithModel(ctx context.Context, prompt string, params *iface.ModelOverrideParams) (<-chan iface.AgentEvent, error)
	AskStreamWithModel(ctx context.Context, prompt string, params *iface.ModelOverrideParams) (<-chan iface.AgentEvent, error)
}

var errTaskModelResolution = errors.New("scheduled task model resolution failed")

const oneTimeClaimRetryDelay = 250 * time.Millisecond

// maxCronRetries is the number of retries after the initial task attempt.
// Retries reuse the same temporary session so the agent can see the successful
// work from earlier attempts and verify any side effects whose outcome was
// interrupted or otherwise unknown.
const maxCronRetries = 3

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
	retryDelay time.Duration

	modelResolver ModelResolver

	mu                    sync.Mutex
	entries               map[string]robfig.EntryID
	timers                map[string]*time.Timer
	oneTimeRuns           map[string]string
	oneTimeGenerations    map[string]uint64
	nextOneTimeGeneration uint64
	stopped               bool

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
		entries:            make(map[string]robfig.EntryID),
		timers:             make(map[string]*time.Timer),
		oneTimeRuns:        make(map[string]string),
		oneTimeGenerations: make(map[string]uint64),
		retryDelay:         10 * time.Second,
	}
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

	s.logger.Info(logger.CatApp, "cron: scheduler daemon stopped")
}

// Schedule dynamically schedules (or updates) a task.
func (s *Scheduler) Schedule(t Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}

	s.unscheduleLocked(t.ID)
	generation := s.advanceOneTimeGenerationLocked(t.ID)
	delete(s.oneTimeRuns, t.ID)

	if t.IsOneTime() {
		delay := time.Until(t.NextRunAt)
		if delay <= 0 {
			go s.executeTaskGeneration(t, generation)
			return
		}

		s.armOneTimeTimerLocked(t, delay, generation)
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

// armOneTimeTimerLocked replaces the timer for a one-time task. The callback
// only removes its own generation, so a claim-error retry armed from inside a
// firing callback cannot be erased by that older callback's cleanup.
func (s *Scheduler) armOneTimeTimerLocked(t Task, delay time.Duration, generation uint64) {
	var timer *time.Timer
	timer = time.AfterFunc(delay, func() {
		s.executeTaskGeneration(t, generation)
		s.mu.Lock()
		if s.oneTimeGenerations[t.ID] == generation && s.timers[t.ID] == timer {
			delete(s.timers, t.ID)
		}
		s.mu.Unlock()
	})
	s.timers[t.ID] = timer
}

// Unschedule dynamically removes a task by ID.
func (s *Scheduler) Unschedule(taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.advanceOneTimeGenerationLocked(taskID)
	delete(s.oneTimeRuns, taskID)
	s.unscheduleLocked(taskID)
}

func (s *Scheduler) advanceOneTimeGenerationLocked(taskID string) uint64 {
	s.nextOneTimeGeneration++
	if s.nextOneTimeGeneration == 0 {
		s.nextOneTimeGeneration++
	}
	generation := s.nextOneTimeGeneration
	s.oneTimeGenerations[taskID] = generation
	return generation
}

func (s *Scheduler) oneTimeGeneration(t Task) uint64 {
	if !t.IsOneTime() {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	generation := s.oneTimeGenerations[t.ID]
	if generation == 0 {
		generation = s.advanceOneTimeGenerationLocked(t.ID)
	}
	return generation
}

func (s *Scheduler) isCurrentOneTimeGeneration(t Task, generation uint64) bool {
	if !t.IsOneTime() {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.oneTimeGenerations[t.ID] == generation
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
	s.executeTaskGeneration(t, s.oneTimeGeneration(t))
}

func (s *Scheduler) executeTaskGeneration(t Task, generation uint64) {
	if !s.claimOneTimeRunGeneration(t, generation) {
		s.logger.Info(logger.CatApp, "cron: duplicate one-time task trigger skipped", "task_id", t.ID)
		return
	}
	s.logger.Info(logger.CatApp, "cron: task execution triggered", "task_id", t.ID,
		"instruction", t.Instruction, "target_agent", t.TargetAgent)

	if isL1Target(t) {
		s.executeL1TaskGeneration(t, generation)
	} else {
		s.executeL2TaskGeneration(t, generation)
	}
}

// claimOneTimeRun makes a one-time task idempotent for a specific scheduled
// instant. Updating the same task to a different instant permits a new run.
func (s *Scheduler) claimOneTimeRun(t Task) bool {
	return s.claimOneTimeRunGeneration(t, s.oneTimeGeneration(t))
}

func (s *Scheduler) claimOneTimeRunGeneration(t Task, generation uint64) bool {
	if !t.IsOneTime() {
		return true
	}
	key := t.NextRunAt.UTC().Format(time.RFC3339Nano)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.oneTimeGenerations[t.ID] != generation {
		return false
	}
	if s.oneTimeRuns[t.ID] == key {
		return false
	}
	s.oneTimeRuns[t.ID] = key
	return true
}

// retryOneTimeClaim rolls back only the provisional in-memory claim for this
// scheduled instant. A database error means ownership was not established;
// unlike claimed=false, it must remain retryable without permitting concurrent
// triggers for the same instant to execute twice.
func (s *Scheduler) retryOneTimeClaim(t Task, generation uint64) {
	if !t.IsOneTime() {
		return
	}
	key := t.NextRunAt.UTC().Format(time.RFC3339Nano)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.oneTimeGenerations[t.ID] != generation || s.oneTimeRuns[t.ID] != key {
		return
	}
	delete(s.oneTimeRuns, t.ID)
	if s.stopped {
		return
	}
	if timer := s.timers[t.ID]; timer != nil {
		timer.Stop()
	}
	s.armOneTimeTimerLocked(t, oneTimeClaimRetryDelay, generation)
}

// executeL1Task handles tasks targeting L1 using a temporary cron session.
func (s *Scheduler) executeL1Task(t Task) {
	s.executeL1TaskGeneration(t, s.oneTimeGeneration(t))
}

func (s *Scheduler) executeL1TaskGeneration(t Task, generation uint64) {
	if !s.isCurrentOneTimeGeneration(t, generation) {
		return
	}
	ctx := context.Background()
	start := time.Now()
	panicRunID := uuid.New().String()
	var l1Session Session

	// Two-phase commit: claim the task.
	claimed, err := s.dbStore.ClaimTask(ctx, t.ID)
	if err != nil {
		s.logger.Error(logger.CatApp, "cron: failed to claim L1 task", "task_id", t.ID, "err", err)
		s.retryOneTimeClaim(t, generation)
		return
	}
	if !claimed {
		s.logger.Debug(logger.CatApp, "cron: L1 task already claimed by another instance, skipping", "task_id", t.ID)
		return
	}
	if !s.isCurrentOneTimeGeneration(t, generation) {
		_ = s.dbStore.UpdateTaskStatus(ctx, t.ID, "active")
		return
	}

	// Panic recovery: catch panic, log, record the failed execution, and finalize its task state.
	defer func() {
		if panicValue := recover(); panicValue != nil {
			s.handleCronPanic(ctx, t, start, panicRunID, ResolvedModel{}, l1Session, false, panicValue)
		}
	}()

	s.runL1TaskWithGenerationAndCleanup(ctx, t, nil, panicRunID, generation, nil)
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

func (s *Scheduler) handleCronPanic(ctx context.Context, t Task, start time.Time, runID string, resolved ResolvedModel, sess Session, l2 bool, panicValue any) {
	rawErr := fmt.Errorf("panic: %v", panicValue)
	s.logger.Error(logger.CatApp, "cron: task execution panicked", "task_id", t.ID, "run_id", runID, "panic", panicValue)
	result := drainEventsResult{diagnosticError: diagnosticCronError("execution_panicked", rawErr)}
	result.timelineDir = s.writeCronDiagnosticTimeline(t, runID, "execution_panicked", rawErr.Error())
	applyCronResult(&result, failedCronResult(t, runID, "execution_panicked", time.Now()))
	if err := s.finishExecution(ctx, t, runID, resolved, start, result, result.diagnosticError, result.canonical.Status, "active"); err != nil {
		s.logger.Warn(logger.CatApp, "cron: panic terminal persistence rejected", "task_id", t.ID, "run_id", runID, "err", err)
		return
	}
	s.notifyTaskComplete(t, false, firstLineSummary(result.replyText))
	if l2 {
		if sess == nil {
			return
		}
		s.deliverL2ResultViaChannel(ctx, t, sess, result.replyText)
		return
	}
	s.deliverL1ResultViaChannel(ctx, t, result.replyText, nil)
}

func (s *Scheduler) writeCronDiagnosticTimeline(t Task, runID, reason, diagnostic string) string {
	var tlDir string
	if s.workDir == "" {
		tlDir = filepath.Join(os.TempDir(), "soloqueue-cron", t.ID, runID)
	} else {
		tlDir = filepath.Join(s.workDir, "logs", "cron", t.ID, runID)
	}
	if err := os.MkdirAll(tlDir, 0o755); err != nil {
		s.logger.Error(logger.CatApp, "cron: failed to create diagnostic timeline", "task_id", t.ID, "run_id", runID, "err", err)
		return ""
	}
	tl, err := timeline.NewWriter(tlDir, "timeline", 50*1024*1024, 15)
	if err != nil {
		s.logger.Error(logger.CatApp, "cron: failed to open diagnostic timeline", "task_id", t.ID, "run_id", runID, "err", err)
		return ""
	}
	defer tl.Close()
	if err := tl.AppendControl(&timeline.ControlPayload{Action: "error", Reason: reason, Content: diagnostic}); err != nil {
		s.logger.Error(logger.CatApp, "cron: failed to write diagnostic timeline", "task_id", t.ID, "run_id", runID, "err", err)
		return ""
	}
	return filepath.Join("logs", "cron", t.ID, runID)
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

const cronFinalOutputContract = `<FINAL_OUTPUT_CONTRACT>
After all analysis and tool calls are complete:
Call SubmitCronResult exactly once.

Rules:
- SubmitCronResult must be the only tool call in the final model response.
- Do not put final business content in ordinary assistant text before or after the tool call.
- Pass only "content" and optional "artifacts" to the tool. Do not add metadata or status fields.
- "content" is optional opaque text. "artifacts" is an optional array of objects containing only non-empty "name" and "ref" strings.
- Provide at least non-empty content or one artifact. Artifact-only submission is allowed when the task output was written elsewhere.
- The task instruction and applicable Skill define the business format; this protocol does not impose an output type or layout.
- Tool arguments are JSON. Escape double quotes, newlines, and backslashes inside string values as \", \n, and \\.
- SubmitCronResult parses and serializes the JSON; never hand-concatenate JSON text.
- Do not include intermediate reasoning, tool calls, or tool results in submitted content.
</FINAL_OUTPUT_CONTRACT>`

const continuationPrompt = "[SYSTEM NOTICE] The previous task attempt failed before completion. Continue the same task using the conversation history and successful tool results above. Any tool call that was in flight when the failure occurred has an unknown outcome: verify its actual state before repeating it. Complete the unfinished work and submit the final result.\n\n" + cronFinalOutputContract

func retryPromptWithOriginalTask(taskPrompt string) string {
	return "[ORIGINAL TASK PROMPT]\n" + taskPrompt + "\n\n" + continuationPrompt
}

// runL1Task executes a single L1 task on the given session.
func (s *Scheduler) runL1Task(ctx context.Context, t Task, l1Session Session) {
	generation := s.oneTimeGeneration(t)
	runID := uuid.New().String()
	claimed, err := s.dbStore.ClaimTask(ctx, t.ID)
	if err != nil || !claimed {
		if err != nil {
			s.logger.Warn(logger.CatApp, "cron: direct L1 task claim failed", "task_id", t.ID, "err", err)
		}
		return
	}
	s.runL1TaskWithGeneration(ctx, t, l1Session, runID, generation)
}

func (s *Scheduler) runL1TaskWithID(ctx context.Context, t Task, l1Session Session, execID string) {
	s.runL1TaskWithGeneration(ctx, t, l1Session, execID, s.oneTimeGeneration(t))
}

func (s *Scheduler) runL1TaskWithGeneration(ctx context.Context, t Task, l1Session Session, execID string, generation uint64) {
	s.runL1TaskWithGenerationAndCleanup(ctx, t, l1Session, execID, generation, nil)
}

func (s *Scheduler) runL1TaskWithGenerationAndCleanup(ctx context.Context, t Task, l1Session Session, execID string, generation uint64, cleanup func()) {
	start := time.Now()
	var resolved ResolvedModel
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()
	defer func() {
		if panicValue := recover(); panicValue != nil {
			s.handleCronPanic(ctx, t, start, execID, resolved, l1Session, false, panicValue)
		}
	}()

	resolved, result, drainErr := s.runTaskAttempts(ctx, t, execID, &l1Session, &cleanup)
	ctx = context.WithoutCancel(ctx)

	duration := time.Since(start)
	s.logger.Info(logger.CatApp, "cron: L1 task completed", "task_id", t.ID, "duration_ms", duration.Milliseconds())

	// Record execution history.
	if drainErr != nil {
		failureCode := cronExecutionFailureCode(drainErr)
		applyCronResult(&result, failedCronResult(t, execID, failureCode, time.Now()))
		result.diagnosticError = diagnosticCronError(failureCode, drainErr)
	}
	status := result.canonical.Status
	errMsg := result.diagnosticError
	if persistErr := s.finishExecution(ctx, t, execID, resolved, start, result, errMsg, status, taskStatusAfterExecution(t)); persistErr != nil {
		s.logger.Warn(logger.CatApp, "cron: L1 terminal persistence rejected", "task_id", t.ID, "run_id", execID, "err", persistErr)
		return
	}
	s.notifyTaskComplete(t, status == "success", firstLineSummary(result.replyText))

	var media []channel.OutboundMedia
	if drainErr == nil {
		media = result.mediaFiles
	}
	s.deliverL1ResultViaChannel(ctx, t, result.replyText, media)

	if drainErr != nil {
		s.logger.Error(logger.CatApp, "cron: L1 task drain error", "task_id", t.ID, "err", drainErr)
	}

}

// executeL2Task handles tasks targeting an L2 team. Creates a temporary
// L2 session, executes the task, and queues the result for L1 delivery.
func (s *Scheduler) executeL2Task(t Task) {
	s.executeL2TaskGeneration(t, s.oneTimeGeneration(t))
}

func (s *Scheduler) executeL2TaskGeneration(t Task, generation uint64) {
	if !s.isCurrentOneTimeGeneration(t, generation) {
		return
	}
	ctx := context.Background()
	start := time.Now()
	execID := uuid.New().String()
	var resolved ResolvedModel
	var l2Session Session

	// Two-phase commit: claim the task.
	claimed, err := s.dbStore.ClaimTask(ctx, t.ID)
	if err != nil {
		s.logger.Error(logger.CatApp, "cron: failed to claim L2 task", "task_id", t.ID, "err", err)
		s.retryOneTimeClaim(t, generation)
		return
	}
	if !claimed {
		s.logger.Debug(logger.CatApp, "cron: L2 task already claimed, skipping", "task_id", t.ID)
		return
	}
	if !s.isCurrentOneTimeGeneration(t, generation) {
		_ = s.dbStore.UpdateTaskStatus(ctx, t.ID, "active")
		return
	}
	// Panic recovery.
	defer func() {
		if panicValue := recover(); panicValue != nil {
			s.handleCronPanic(ctx, t, start, execID, resolved, l2Session, true, panicValue)
		}
	}()

	var cleanup func()
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()
	resolved, result, drainErr := s.runTaskAttempts(ctx, t, execID, &l2Session, &cleanup)
	ctx = context.WithoutCancel(ctx)

	duration := time.Since(start)
	s.logger.Info(logger.CatApp, "cron: L2 task completed", "task_id", t.ID,
		"target", t.TargetAgent, "duration_ms", duration.Milliseconds())

	// Record execution history.
	if drainErr != nil {
		failureCode := cronExecutionFailureCode(drainErr)
		applyCronResult(&result, failedCronResult(t, execID, failureCode, time.Now()))
		result.diagnosticError = diagnosticCronError(failureCode, drainErr)
	}
	status := result.canonical.Status
	errMsg := result.diagnosticError
	if persistErr := s.finishExecution(ctx, t, execID, resolved, start, result, errMsg, status, taskStatusAfterExecution(t)); persistErr != nil {
		s.logger.Warn(logger.CatApp, "cron: L2 terminal persistence rejected", "task_id", t.ID, "run_id", execID, "err", persistErr)
		return
	}
	s.notifyTaskComplete(t, status == "success", firstLineSummary(result.replyText))

	// Deliver through L2's bound channel. If L2 has no configured notification
	// channel, fall back to L1's channel. A configured-but-unavailable L2 sender
	// is not a fallback case: it is an operational delivery failure.
	if result.replyText != "" {
		s.deliverL2ResultViaChannel(ctx, t, l2Session, result.replyText)
	}
	if drainErr == nil && len(result.mediaFiles) > 0 && l2Session != nil && l2Session.HasNotifyChannel() {
		if err := l2Session.SendMediaViaChannel(ctx, result.mediaFiles); err != nil {
			s.logger.Warn(logger.CatApp, "cron: L2 media notification failed", "task_id", t.ID, "err", err)
		}
	}

	if drainErr != nil {
		s.logger.Error(logger.CatApp, "cron: L2 task drain error", "task_id", t.ID, "err", drainErr)
	}

}

func (s *Scheduler) deliverL2ResultViaChannel(ctx context.Context, t Task, l2Session Session, replyText string) {
	if l2Session == nil || !l2Session.HasNotifyChannel() {
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

func (s *Scheduler) deliverL1ResultViaChannel(ctx context.Context, t Task, replyText string, media []channel.OutboundMedia) {
	l1Session := s.sessionMgr.Session()
	if l1Session == nil {
		s.logger.Warn(logger.CatApp, "cron: L1 notification skipped, no permanent L1 session", "task_id", t.ID)
		return
	}
	if replyText != "" {
		if err := l1Session.SendViaChannel(ctx, replyText); err != nil {
			s.logger.Warn(logger.CatApp, "cron: L1 notification failed", "task_id", t.ID, "err", err)
		}
	}
	if len(media) > 0 {
		if err := l1Session.SendMediaViaChannel(ctx, media); err != nil {
			s.logger.Warn(logger.CatApp, "cron: L1 media notification failed", "task_id", t.ID, "err", err)
		}
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

func (s *Scheduler) askStatefulWithTaskModelPrompt(ctx context.Context, t Task, sess Session, prompt string) (ResolvedModel, <-chan iface.AgentEvent, error) {
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
	ch, err := routed.AskStreamWithModel(ctx, prompt, &resolved.Params)
	return resolved, ch, err
}

func (s *Scheduler) askWithTaskModel(ctx context.Context, t Task, sess Session) (ResolvedModel, <-chan iface.AgentEvent, error) {
	return s.askWithTaskModelPrompt(ctx, t, sess, s.buildTaskPrompt(t))
}

// buildTaskPrompt builds the prompt for a task.
func (s *Scheduler) buildTaskPrompt(t Task) string {
	return buildCronPrompt(t)
}

// buildCronContext marks a cron execution and adds its telemetry metadata.
func (s *Scheduler) buildCronContext(parent context.Context, t Task, runID string) context.Context {
	parent = iface.ContextWithCronExecution(parent)
	return telemetryctx.WithMetadata(parent, telemetryctx.Metadata{
		RunID:    runID,
		Origin:   telemetryctx.OriginCron,
		TaskType: t.TaskType,
	})
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
	replyText              string
	canonical              CronResultV1
	diagnosticError        string
	mediaFiles             []SendFileMedia
	timelineDir            string // relative path from workDir: logs/cron/<taskID>/<execID>
	toolCallCount          int
	completedToolCallCount int
}

func cronAttemptFailed(result drainEventsResult, err error) bool {
	return err != nil || result.canonical.Status != "success"
}

// runTaskAttempts owns the one bounded budget for acquisition, startup and execution.
// The temporary session belongs to the owner context; each Ask gets a fresh child.
func (s *Scheduler) runTaskAttempts(ctx context.Context, t Task, execID string, sess *Session, cleanup *func()) (resolved ResolvedModel, result drainEventsResult, attemptErr error) {
	started := false
	haveHistory := false
	for attempt := 0; attempt <= maxCronRetries; attempt++ {
		if ctx.Err() != nil {
			attemptErr = context.Cause(ctx)
			break
		}
		if attempt > 0 {
			if !waitContext(ctx, s.retryDelay) {
				attemptErr = context.Cause(ctx)
				break
			}
		}
		prompt := s.buildTaskPrompt(t)
		if attempt > 0 {
			prompt = continuationPrompt
			if !haveHistory {
				prompt = retryPromptWithOriginalTask(s.buildTaskPrompt(t))
			}
		}
		attemptCtx, cancel := context.WithCancel(s.buildCronContext(ctx, t, execID))
		result, attemptErr = func() (result drainEventsResult, err error) {
			defer func() {
				if value := recover(); value != nil {
					err = fmt.Errorf("%w: %v", errCronAttemptPanic, value)
				}
			}()
			if *sess == nil {
				acquired, isNew, release, acquireErr := s.sessionMgr.GetSession(s.buildCronContext(ctx, t, execID), t.TargetAgent, t.ID)
				if acquireErr != nil || acquired == nil {
					if release != nil {
						release()
					}
					if acquireErr == nil {
						acquireErr = errors.New("temporary session is nil")
					}
					return result, fmt.Errorf("%w: %w", errCronAttemptStart, acquireErr)
				}
				*sess = acquired
				if isNew || t.TargetAgent == "L1" {
					*cleanup = release
				}
			}
			route, ch, startErr := s.askStatefulWithTaskModelPrompt(attemptCtx, t, *sess, prompt)
			if startErr != nil {
				return result, fmt.Errorf("%w: %w", errCronAttemptStart, startErr)
			}
			resolved = route
			haveHistory = true
			if !started {
				started = true
				s.notifyTaskStarted(t)
			}
			return s.drainEventsWithTimelinePromptMode(ch, t, execID, prompt, attempt > 0)
		}()
		cancel()
		if attemptErr != nil {
			code := cronExecutionFailureCode(attemptErr)
			applyCronResult(&result, failedCronResult(t, execID, code, time.Now()))
			result.diagnosticError = diagnosticCronError(code, attemptErr)
			if result.timelineDir == "" {
				if attempt > 0 {
					s.writeCronRetryBoundary(t, execID, prompt)
				}
				result.timelineDir = s.writeCronDiagnosticTimeline(t, execID, code, attemptErr.Error())
			}
		}
		if !cronAttemptFailed(result, attemptErr) || runwatch.CodeOf(attemptErr) == runwatch.CodeCancelledByUser {
			break
		}
		s.logger.Warn(logger.CatApp, "cron: task attempt failed", "task_id", t.ID, "attempt", attempt+1, "err", attemptErr)
	}
	return
}

var (
	errCronAttemptPanic = errors.New("execution_panicked")
	errCronAttemptStart = errors.New("execution_start_failed")
)

func normalizeCronResult(t Task, runID, raw string, generatedAt time.Time) (CronResultV1, error) {
	base := CronResultV1{
		Version:     "v1",
		TaskID:      t.ID,
		RunID:       runID,
		Title:       t.Title,
		Artifacts:   []CronResultArtifact{},
		GeneratedAt: generatedAt.Format(time.RFC3339),
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		err := errors.New("model returned empty final output")
		base.Status = "failed"
		base.Error = cronResultError("empty_structured_output", "The model returned no final output")
		return base, err
	}

	var wire struct {
		Content   json.RawMessage `json:"content"`
		Artifacts json.RawMessage `json:"artifacts"`
	}
	decoder := json.NewDecoder(bytes.NewBufferString(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return partialCronResult(base, fmt.Errorf("decode final output: %w", err))
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return partialCronResult(base, fmt.Errorf("decode trailing final output: %w", err))
	}

	content := ""
	if wire.Content != nil {
		if bytes.Equal(bytes.TrimSpace(wire.Content), []byte("null")) {
			return partialCronResult(base, errors.New("content must be a string"))
		}
		if err := json.Unmarshal(wire.Content, &content); err != nil {
			return partialCronResult(base, fmt.Errorf("content must be a string: %w", err))
		}
	}
	if strings.TrimSpace(content) == "" {
		content = ""
	}

	artifacts := []CronResultArtifact{}
	if wire.Artifacts != nil {
		if bytes.Equal(bytes.TrimSpace(wire.Artifacts), []byte("null")) {
			return partialCronResult(base, errors.New("artifacts must be an array"))
		}
		artifactDecoder := json.NewDecoder(bytes.NewReader(wire.Artifacts))
		artifactDecoder.DisallowUnknownFields()
		if err := artifactDecoder.Decode(&artifacts); err != nil {
			return partialCronResult(base, fmt.Errorf("artifacts must be an array of name/ref objects: %w", err))
		}
	}
	for i, artifact := range artifacts {
		if strings.TrimSpace(artifact.Name) == "" {
			return partialCronResult(base, fmt.Errorf("artifacts[%d].name must be a non-empty string", i))
		}
		if strings.TrimSpace(artifact.Ref) == "" {
			return partialCronResult(base, fmt.Errorf("artifacts[%d].ref must be a non-empty string", i))
		}
	}
	if content == "" && len(artifacts) == 0 {
		return partialCronResult(base, errors.New("content or at least one artifact is required"))
	}

	base.Status = "success"
	base.Content = content
	base.Artifacts = artifacts
	return base, nil
}

func partialCronResult(base CronResultV1, parseErr error) (CronResultV1, error) {
	base.Status = "partial"
	base.Content = "任务已完成，但最终结果格式异常，未经验证的内容未发送。详细信息已记录到执行日志。"
	base.Artifacts = []CronResultArtifact{}
	base.Error = cronResultError("invalid_structured_output", "model returned non-v1 output")
	return base, parseErr
}

func failedCronResult(t Task, runID, code string, generatedAt time.Time) CronResultV1 {
	return CronResultV1{
		Version:     "v1",
		TaskID:      t.ID,
		RunID:       runID,
		Title:       t.Title,
		Status:      "failed",
		Artifacts:   []CronResultArtifact{},
		Error:       cronResultError(code, publicCronFailureMessage(code)),
		GeneratedAt: generatedAt.Format(time.RFC3339),
	}
}

func publicCronFailureMessage(code string) string {
	switch code {
	case "execution_start_failed":
		return "Scheduled task could not be started"
	case "execution_panicked":
		return "Scheduled task execution failed unexpectedly"
	case "missing_structured_submission":
		return "Scheduled task produced no validated result"
	case "invalid_structured_submission":
		return "Scheduled task result validation failed"
	default:
		return "Scheduled task execution failed"
	}
}

func diagnosticCronError(code string, err error) string {
	if err == nil {
		return code
	}
	return code + ": " + err.Error()
}

func cronExecutionFailureCode(err error) string {
	if code := runwatch.CodeOf(err); code != "" {
		return string(code)
	}
	if errors.Is(err, errCronAttemptPanic) {
		return "execution_panicked"
	}
	if errors.Is(err, errCronAttemptStart) {
		return "execution_start_failed"
	}
	return "execution_failed"
}

func cronResultError(code, message string) *string {
	value := code + ": " + message
	return &value
}

func formatCronResult(result CronResultV1) string {
	statusLabel := map[string]string{
		"success": "成功",
		"partial": "部分完成",
		"failed":  "失败",
	}[result.Status]
	if statusLabel == "" {
		statusLabel = "未知状态"
	}

	var formatted strings.Builder
	fmt.Fprintf(&formatted, "[%s] %s", statusLabel, result.Title)
	if result.Content != "" {
		formatted.WriteString("\n\n")
		formatted.WriteString(result.Content)
	}
	for i, artifact := range result.Artifacts {
		if i == 0 {
			formatted.WriteString("\n\n")
		} else {
			formatted.WriteByte('\n')
		}
		fmt.Fprintf(&formatted, "- %s: %s", artifact.Name, artifact.Ref)
	}
	if result.Status != "partial" && result.Error != nil {
		formatted.WriteString("\n\n异常：")
		formatted.WriteString(*result.Error)
	}
	return formatted.String()
}

func applyCronResult(result *drainEventsResult, canonical CronResultV1) {
	result.canonical = canonical
	result.replyText = formatCronResult(canonical)
}

// drainEventsWithTimeline drains an agent event channel and writes every event
// (content, reasoning, tool calls, tool results) to a timeline file so the full
// execution can be replayed later. Returns accumulated reply text, media files,
// and the timeline directory path.
func (s *Scheduler) drainEventsWithTimeline(ch <-chan iface.AgentEvent, t Task, execID string) (drainEventsResult, error) {
	return s.drainEventsWithTimelinePromptMode(ch, t, execID, s.buildTaskPrompt(t), false)
}

func (s *Scheduler) drainEventsWithTimelinePrompt(ch <-chan iface.AgentEvent, t Task, execID, prompt string) (drainEventsResult, error) {
	return s.drainEventsWithTimelinePromptMode(ch, t, execID, prompt, false)
}

func (s *Scheduler) drainEventsWithTimelinePromptMode(ch <-chan iface.AgentEvent, t Task, execID, prompt string, retry bool) (drainEventsResult, error) {
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
	result.timelineDir = filepath.Join("logs", "cron", t.ID, execID)

	agentID := "cron-task-" + t.ID
	tl, err := timeline.NewWriter(tlDir, "timeline", 50*1024*1024, 15)
	if err != nil {
		return result, fmt.Errorf("create cron timeline writer: %w", err)
	}
	defer tl.Close()

	// Write the user prompt (task instruction).
	_ = tl.AppendMessage(&timeline.MessagePayload{
		Role:    "user",
		Content: prompt,
		AgentID: agentID,
	})
	if retry {
		_ = tl.AppendControl(&timeline.ControlPayload{
			Action:  "retry",
			Reason:  "task_attempt_failed",
			Content: prompt,
		})
	}

	// ── Event processing state ──
	type submissionAttemptState struct {
		done bool
	}
	var (
		contentBuf                  strings.Builder
		reasoningBuf                strings.Builder
		submissionEvents            int
		submissionAttempts          = make(map[string]*submissionAttemptState)
		submissionAttemptOrder      []string
		submissionProtocolErrors    []string
		submissionAttemptErrors     []string
		successfulSubmissionResults []CronResultV1
		pendingToolCalls            = make(map[string]int)
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
	flushPendingAssistant := func() {
		if contentBuf.Len() == 0 && reasoningBuf.Len() == 0 {
			return
		}
		flushAssistant(contentBuf.String(), reasoningBuf.String(), nil)
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
			}
		} else if cur == "" {
			contentBuf.WriteString(content)
		} else if !strings.Contains(cur, content) {
			contentBuf.WriteString(content)
		}
	}

	for ev := range ch {
		// Use iface.EventConsumer for safe cross-package content extraction.
		if consumer, ok := ev.(iface.EventConsumer); ok {
			if delta, ok := consumer.ContentDelta(); ok {
				contentBuf.WriteString(delta)
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
			pendingToolCalls[callID]++
			if name == "SubmitCronResult" {
				submissionEvents++
				switch {
				case strings.TrimSpace(callID) == "":
					submissionProtocolErrors = append(submissionProtocolErrors, "SubmitCronResult start missing call ID")
				case submissionAttempts[callID] != nil:
					submissionProtocolErrors = append(submissionProtocolErrors, fmt.Sprintf("duplicate SubmitCronResult start for call ID %q", callID))
				default:
					submissionAttempts[callID] = &submissionAttemptState{}
					submissionAttemptOrder = append(submissionAttemptOrder, callID)
				}
			}
			if callID != "" {
				flushAssistant(contentBuf.String(), reasoningBuf.String(), []timeline.ToolCallRec{
					{ID: callID, Type: "function", Name: name, Arguments: args},
				})
			}

		case "ToolExecDoneEvent":
			callID := rv.FieldByName("CallID").String()
			name := rv.FieldByName("Name").String()
			toolResult := rv.FieldByName("Result").String()
			if pendingToolCalls[callID] > 0 {
				pendingToolCalls[callID]--
				result.completedToolCallCount++
			}
			errField := rv.FieldByName("Err")
			var toolErr error
			if errField.IsValid() && !errField.IsNil() {
				toolErr, _ = errField.Interface().(error)
				if toolErr == nil {
					toolErr = errors.New("tool execution failed")
				}
			}
			timelineToolResult := toolResult
			if toolErr != nil {
				timelineToolResult = "error: " + toolErr.Error()
			}
			_ = tl.AppendMessage(&timeline.MessagePayload{
				Role:        "tool",
				Content:     timelineToolResult,
				Name:        name,
				ToolCallID:  callID,
				IsEphemeral: len(toolResult) > 2000,
				AgentID:     agentID,
			})
			if name == "SubmitCronResult" {
				submissionEvents++
				attempt := submissionAttempts[callID]
				paired := false
				switch {
				case strings.TrimSpace(callID) == "":
					submissionProtocolErrors = append(submissionProtocolErrors, "SubmitCronResult completion missing call ID")
				case attempt == nil:
					submissionProtocolErrors = append(submissionProtocolErrors, fmt.Sprintf("orphan SubmitCronResult completion for call ID %q", callID))
				case attempt.done:
					submissionProtocolErrors = append(submissionProtocolErrors, fmt.Sprintf("duplicate SubmitCronResult completion for call ID %q", callID))
				default:
					attempt.done = true
					paired = true
				}
				if paired && toolErr != nil {
					submissionAttemptErrors = append(submissionAttemptErrors, fmt.Sprintf("call ID %q tool execution failed: %s", callID, toolErr.Error()))
				} else if paired {
					canonical, validationErr := normalizeCronResult(t, execID, toolResult, time.Now())
					if validationErr != nil || canonical.Status != "success" {
						if validationErr == nil {
							validationErr = errors.New("submission did not normalize to success")
						}
						submissionAttemptErrors = append(submissionAttemptErrors, fmt.Sprintf("call ID %q validation failed: %s", callID, validationErr.Error()))
					} else {
						successfulSubmissionResults = append(successfulSubmissionResults, canonical)
					}
				}
			}
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
				agentErr := fmt.Errorf("agent error: %w", errField.Elem().Interface().(error))
				flushPendingAssistant()
				_ = tl.AppendControl(&timeline.ControlPayload{Action: "error", Reason: "cron_execution_error", Content: agentErr.Error()})
				return result, agentErr
			}
			agentErr := errors.New("agent error: unknown")
			flushPendingAssistant()
			_ = tl.AppendControl(&timeline.ControlPayload{Action: "error", Reason: "cron_execution_error", Content: agentErr.Error()})
			return result, agentErr
		}

		// Fallback: use EventConsumer for Done and Error if reflection didn't match.
		if consumer, ok := ev.(iface.EventConsumer); ok {
			if content, ok := consumer.DoneContent(); ok {
				appendDoneContent(content)
				flushAssistant(contentBuf.String(), reasoningBuf.String(), nil)
			}
			if errVal, ok := consumer.Error(); ok {
				agentErr := fmt.Errorf("agent error: %w", errVal)
				flushPendingAssistant()
				_ = tl.AppendControl(&timeline.ControlPayload{Action: "error", Reason: "cron_execution_error", Content: agentErr.Error()})
				return result, agentErr
			}
		}
	}

	for _, callID := range submissionAttemptOrder {
		if !submissionAttempts[callID].done {
			submissionProtocolErrors = append(submissionProtocolErrors, fmt.Sprintf("unfinished SubmitCronResult call ID %q", callID))
		}
	}

	var submissionFailureCode, submissionDiagnostic string
	switch {
	case submissionEvents == 0:
		submissionFailureCode = "missing_structured_submission"
		submissionDiagnostic = "no SubmitCronResult execution completed"
	case len(submissionProtocolErrors) > 0:
		submissionFailureCode = "invalid_structured_submission"
		submissionDiagnostic = strings.Join(submissionProtocolErrors, "; ")
	case len(successfulSubmissionResults) > 1:
		submissionFailureCode = "invalid_structured_submission"
		submissionDiagnostic = fmt.Sprintf("multiple successful SubmitCronResult submissions: %d", len(successfulSubmissionResults))
	case len(successfulSubmissionResults) == 0:
		submissionFailureCode = "invalid_structured_submission"
		submissionDiagnostic = strings.Join(submissionAttemptErrors, "; ")
		if submissionDiagnostic == "" {
			submissionDiagnostic = "SubmitCronResult did not produce a valid result"
		}
	default:
		applyCronResult(&result, successfulSubmissionResults[0])
	}
	if submissionFailureCode != "" {
		applyCronResult(&result, failedCronResult(t, execID, submissionFailureCode, time.Now()))
		result.diagnosticError = diagnosticCronError(submissionFailureCode, errors.New(submissionDiagnostic))
		s.logger.Warn(logger.CatApp, "cron: structured submission rejected",
			"task_id", t.ID, "run_id", execID, "reason", submissionFailureCode, "detail", submissionDiagnostic)
		_ = tl.AppendControl(&timeline.ControlPayload{
			Action:  "error",
			Reason:  submissionFailureCode,
			Content: submissionDiagnostic,
		})
	}

	// Write completion marker.
	_ = tl.AppendControl(&timeline.ControlPayload{
		Action: "complete",
		Reason: "cron_task_done",
	})

	return result, nil
}

func (s *Scheduler) writeCronRetryBoundary(t Task, execID, prompt string) {
	var tlDir string
	if s.workDir == "" {
		tlDir = filepath.Join(os.TempDir(), "soloqueue-cron", t.ID, execID)
	} else {
		tlDir = filepath.Join(s.workDir, "logs", "cron", t.ID, execID)
	}
	if err := os.MkdirAll(tlDir, 0o755); err != nil {
		return
	}
	tl, err := timeline.NewWriter(tlDir, "timeline", 50*1024*1024, 15)
	if err != nil {
		return
	}
	defer tl.Close()
	agentID := "cron-task-" + t.ID
	_ = tl.AppendMessage(&timeline.MessagePayload{Role: "user", Content: prompt, AgentID: agentID})
	_ = tl.AppendControl(&timeline.ControlPayload{Action: "retry", Reason: "task_attempt_failed", Content: prompt})
}

// recordExecution writes an execution history record to the database.
func (s *Scheduler) recordExecution(ctx context.Context, t Task, resolved ResolvedModel, start time.Time, result drainEventsResult, errMsg string, status string) {
	if s.dbStore == nil {
		return
	}
	historyStatus := status
	if historyStatus == "partial" {
		historyStatus = "success"
	}
	terminalCode := "completed"
	if result.canonical.Error != nil {
		terminalCode, _, _ = strings.Cut(*result.canonical.Error, ":")
	} else if historyStatus != "success" {
		terminalCode = "execution_failed"
	}
	_ = s.dbStore.RecordExecution(ctx, ExecutionRecord{
		ID:            uuid.New().String(),
		TaskID:        t.ID,
		ExecutedAt:    start,
		CompletedAt:   time.Now(),
		DurationMs:    time.Since(start).Milliseconds(),
		Status:        historyStatus,
		ResultSummary: result.replyText,
		ErrorMessage:  errMsg,
		TaskType:      t.TaskType,
		TargetAgent:   t.TargetAgent,
		ModelID:       resolved.Params.ModelID,
		ProviderID:    resolved.Params.ProviderID,
		TimelineDir:   result.timelineDir,
		TerminalCode:  terminalCode,
	})
}

// updateTaskAfterExecution advances the task state after a run completes.
func (s *Scheduler) updateTaskAfterExecution(ctx context.Context, t Task) {
	if s.dbStore == nil {
		return
	}
	if t.IsOneTime() {
		_ = s.dbStore.MarkCompleted(ctx, t.ID)
		return
	}
	next, _ := NextTrigger(t.Expression, time.Now())
	_ = s.dbStore.UpdateNextRun(ctx, t.ID, time.Now(), next)
}

// finishExecution records history and advances the task state after execution.
func (s *Scheduler) finishExecution(ctx context.Context, t Task, runID string, resolved ResolvedModel, start time.Time, result drainEventsResult, errMsg string, status string, taskStatus string) error {
	if s.dbStore == nil {
		return nil
	}
	s.recordExecution(ctx, t, resolved, start, result, errMsg, status)
	if taskStatus == "failed" {
		return s.dbStore.UpdateTaskStatus(ctx, t.ID, "failed")
	}
	s.updateTaskAfterExecution(ctx, t)
	return nil
}

func taskStatusAfterExecution(t Task) string {
	if t.IsOneTime() {
		return "completed"
	}
	return "active"
}

func waitContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// buildCronPrompt wraps a task's instruction with scheduler context and the
// strict final-content contract. It does not inject previous run output.
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
			"Execute the delimited instruction directly. The SubmitCronResult transport contract below is mandatory; the task instruction and applicable Skill still define the business content, format, layout, and artifact production.\n\n"+
			"<TASK_INSTRUCTION>\n%s\n</TASK_INSTRUCTION>\n\n%s",
		t.ID, t.Title, t.TaskType, scheduleDesc, triggerTime, t.Instruction, cronFinalOutputContract,
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

	kind := channel.MediaFile
	switch r.FileType {
	case "image":
		kind = channel.MediaImage
	case "video":
		kind = channel.MediaVideo
	case "voice":
		kind = channel.MediaVoice
	}

	return &SendFileMedia{
		Kind: kind, Path: r.Path, URL: r.URL, FileName: r.FileName,
	}
}
