package simulation

import (
	"context"
	"sync"
	"sync/atomic"
)

// AgentScheduler manages agent goroutine lifecycle and concurrency.
type AgentScheduler struct {
	agents   []*SimAgent
	poolSize int
	active   atomic.Int32
	wg       sync.WaitGroup
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewAgentScheduler creates a scheduler. poolSize=0 means one goroutine per agent (no limit).
func NewAgentScheduler(poolSize int) *AgentScheduler {
	return &AgentScheduler{
		poolSize: poolSize,
		stopCh:   make(chan struct{}),
	}
}

// Launch starts all agent goroutines. Each goroutine calls fn(sa) in a loop
// until stopped. If poolSize > 0, a semaphore limits concurrent goroutines.
func (as *AgentScheduler) Launch(ctx context.Context, fn func(ctx context.Context, sa *SimAgent)) {
	// Build agent list
	agents := make([]*SimAgent, len(as.agents))
	copy(agents, as.agents)

	sem := make(chan struct{}, as.concurrency())

	for _, sa := range agents {
		as.wg.Add(1)
		go func(sa *SimAgent) {
			defer as.wg.Done()

			if as.poolSize > 0 {
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-as.stopCh:
					return
				case <-ctx.Done():
					return
				}
			}

			as.active.Add(1)
			defer as.active.Add(-1)

			fn(ctx, sa)
		}(sa)
	}
}

func (as *AgentScheduler) concurrency() int {
	if as.poolSize <= 0 {
		return len(as.agents)
	}
	return as.poolSize
}

// Wait blocks until all agent goroutines finish.
func (as *AgentScheduler) Wait() {
	as.wg.Wait()
}

// Stop signals all goroutines to stop.
func (as *AgentScheduler) Stop() {
	as.stopOnce.Do(func() {
		close(as.stopCh)
	})
}

// Active returns the number of currently running agent goroutines.
func (as *AgentScheduler) Active() int32 {
	return as.active.Load()
}

// SetAgents sets the agent list (called before Launch).
func (as *AgentScheduler) SetAgents(agents []*SimAgent) {
	as.agents = agents
}
