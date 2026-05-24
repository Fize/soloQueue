package cron

import (
	"context"
	"os"
	"sync"
	"time"

	robfig "github.com/robfig/cron/v3"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// Session defines the interface required by the Scheduler to trigger tasks,
// decoupling the cron package from the concrete session package to prevent circular imports.
type Session interface {
	Idle() bool
	QueueMessage(prompt string)
	AskStream(ctx context.Context, prompt string) (<-chan iface.AgentEvent, error)
}

// SessionManager defines the interface required to retrieve the active session.
type SessionManager interface {
	Session() Session
}

// Scheduler manages executing scheduled tasks (both cron and timer-based) in the background.
type Scheduler struct {
	dbStore    *DBStore
	sessionMgr SessionManager
	logger     *logger.Logger
	cron       *robfig.Cron

	mu      sync.Mutex
	entries map[string]robfig.EntryID
	timers  map[string]*time.Timer
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
	return &Scheduler{
		dbStore:    db,
		sessionMgr: sm,
		logger:     l,
		cron: robfig.New(robfig.WithParser(robfig.NewParser(
			robfig.Minute | robfig.Hour | robfig.Dom | robfig.Month | robfig.Dow,
		))),
		entries: make(map[string]robfig.EntryID),
		timers:  make(map[string]*time.Timer),
	}
}

// Start loads all active tasks from DB, schedules them, and starts the cron runner.
func (s *Scheduler) Start(ctx context.Context) error {
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

// Stop stops the background cron runner and cancels all active one-time timers.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cron.Stop()
	for _, timer := range s.timers {
		timer.Stop()
	}
	s.entries = make(map[string]robfig.EntryID)
	s.timers = make(map[string]*time.Timer)
	s.logger.Info(logger.CatApp, "cron: scheduler daemon stopped")
}

// Schedule dynamically schedules (or updates) a task.
func (s *Scheduler) Schedule(t Task) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Cancel existing first to handle updates
	s.unscheduleLocked(t.ID)

	if t.IsOneTime() {
		delay := time.Until(t.NextRunAt)
		if delay <= 0 {
			// Trigger immediately if time has passed
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

func (s *Scheduler) executeTask(t Task) {
	s.logger.Info(logger.CatApp, "cron: task execution triggered", "task_id", t.ID, "instruction", t.Instruction)

	session := s.sessionMgr.Session()
	if session == nil {
		s.logger.Warn(logger.CatApp, "cron: task execution skipped, no active session", "task_id", t.ID)
		return
	}

	if !session.Idle() {
		s.logger.Warn(logger.CatApp, "cron: session busy, queueing task into pending queue", "task_id", t.ID)
		session.QueueMessage(t.Instruction)
		return
	}

	start := time.Now()
	// Trigger task execution via AskStream
	ch, err := session.AskStream(context.Background(), t.Instruction)
	if err != nil {
		s.logger.Error(logger.CatApp, "cron: task execution failed to start", "task_id", t.ID, "err", err)
		return
	}

	// Drain the channel to run the agent turn to completion
	for range ch {
	}

	duration := time.Since(start)
	s.logger.Info(logger.CatApp, "cron: task execution completed successfully", "task_id", t.ID, "duration_ms", duration.Milliseconds())

	// Update DB timestamps
	ctx := context.Background()
	if t.IsOneTime() {
		_ = s.dbStore.MarkCompleted(ctx, t.ID)
	} else {
		next, _ := NextTrigger(t.Expression, time.Now())
		_ = s.dbStore.UpdateNextRun(ctx, t.ID, time.Now(), next)
	}
}
