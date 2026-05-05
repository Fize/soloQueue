package permanent

import (
	"context"
	"fmt"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

const (
	maxRetries      = 10
	migrationPeriod = 24 * time.Hour
	initialBackoff  = 1 * time.Minute
)

// NotifyFunc is called when migration fails after max retries.
type NotifyFunc func(message string)

// Scheduler runs periodic permanent memory migrations with retry.
type Scheduler struct {
	mgr    *Manager
	logger *logger.Logger
	notify NotifyFunc

	retryCount int
	nextDelay  time.Duration
}

// NewScheduler creates a Scheduler for periodic migration.
func NewScheduler(mgr *Manager, l *logger.Logger, notify NotifyFunc) *Scheduler {
	return &Scheduler{
		mgr:       mgr,
		logger:    l,
		notify:    notify,
		nextDelay: initialBackoff,
	}
}

// Run starts the migration loop. It returns when ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	s.doMigrate(ctx)

	ticker := time.NewTicker(migrationPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.doMigrate(ctx)
		}
	}
}

func (s *Scheduler) doMigrate(ctx context.Context) {
	count, err := s.mgr.Migrate(ctx)
	if err == nil {
		s.retryCount = 0
		s.nextDelay = initialBackoff
		if count > 0 && s.logger != nil {
			s.logger.InfoContext(ctx, logger.CatApp, "permanent: migration succeeded", "count", count)
		}
		return
	}

	if s.logger != nil {
		s.logger.LogError(ctx, logger.CatApp, "permanent: migration failed", err)
	}
	s.retryCount++

	if s.retryCount <= maxRetries {
		if s.logger != nil {
			s.logger.WarnContext(ctx, logger.CatApp, "permanent: will retry after backoff",
				"retry", s.retryCount,
				"delay_sec", int(s.nextDelay.Seconds()),
			)
		}

		timer := time.NewTimer(s.nextDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		s.nextDelay *= 2
		if s.nextDelay > 1*time.Hour {
			s.nextDelay = 1 * time.Hour
		}

		s.doMigrate(ctx)
		return
	}

	// Max retries exceeded — notify user
	if s.notify != nil {
		s.notify(fmt.Sprintf("长期记忆迁移失败，已重试 %d 次: %v", maxRetries, err))
	}
}
