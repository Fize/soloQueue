package session

import (
	"context"
	"fmt"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/permanent"
)

// DailyMemoryFlusher runs a daily task at midnight that:
//  1. Flushes all unpersisted context window messages to short-term memory files.
//  2. Immediately migrates expired short-term memory files (>3 days) to the
//     permanent vector store.
type DailyMemoryFlusher struct {
	sessionMgr *SessionManager
	permMgr    *permanent.Manager
	logger     *logger.Logger
}

// NewDailyMemoryFlusher creates a flusher. permMgr may be nil if permanent
// memory is disabled (migration step will be skipped).
func NewDailyMemoryFlusher(sm *SessionManager, pm *permanent.Manager, l *logger.Logger) *DailyMemoryFlusher {
	return &DailyMemoryFlusher{
		sessionMgr: sm,
		permMgr:    pm,
		logger:     l,
	}
}

// Run sleeps until the next midnight, executes the flush+migrate cycle, and
// repeats forever. Returns when ctx is cancelled.
func (f *DailyMemoryFlusher) Run(ctx context.Context) {
	for {
		sleep := timeUntilNextMidnight()
		if f.logger != nil {
			f.logger.InfoContext(ctx, logger.CatApp, "daily memory flusher: scheduled",
				"next_run_in", sleep.String())
		}

		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		f.doFlush(ctx)
	}
}

func (f *DailyMemoryFlusher) doFlush(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			if f.logger != nil {
				f.logger.Error(logger.CatApp, "daily memory flusher: panic recovered",
					"panic", fmt.Sprintf("%v", r))
			}
		}
	}()

	// Step 1: Flush unpersisted messages to short-term memory.
	s := f.sessionMgr.Session()
	if s != nil {
		s.FlushMemory(ctx)
		if f.logger != nil {
			f.logger.InfoContext(ctx, logger.CatApp, "daily memory flusher: flush completed")
		}
	}

	// Step 2: Migrate expired short-term files to permanent vector store.
	if f.permMgr != nil {
		count, err := f.permMgr.Migrate(ctx)
		if err != nil {
			if f.logger != nil {
				f.logger.LogError(ctx, logger.CatApp, "daily memory flusher: migration failed", err)
			}
		} else if count > 0 && f.logger != nil {
			f.logger.InfoContext(ctx, logger.CatApp, "daily memory flusher: migration completed",
				"migrated", count)
		}
	}
}

// timeUntilNextMidnight returns the duration from now to the next 00:00:00 in
// the local timezone.
func timeUntilNextMidnight() time.Duration {
	now := time.Now()
	next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	return next.Sub(now)
}
