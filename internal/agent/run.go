package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// jobWatchdogGrace is the time to wait for a job to finish after ctx
// cancellation before declaring it stuck and continuing the run loop.
const jobWatchdogGrace = 1 * time.Second

// runJob runs fn(ctx) in a goroutine with a watchdog. If the context is
// cancelled and fn doesn't return within jobWatchdogGrace, a warning is
// logged and runJob returns. The fn goroutine will eventually terminate
// on its own (e.g., when orphan processes finish / a.emit detects ctx.Done).
func (a *Agent) runJob(ctx context.Context, fn func(context.Context)) {
	done := make(chan struct{}, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				err := fmt.Errorf("agent job panic: %v", r)
				a.setRuntimeExitErr(err)
				a.logError(ctx, logger.CatActor, "agent job panic", err)
				a.cancel()
			}
			close(done)
		}()
		fn(ctx)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		select {
		case <-done:
		case <-time.After(jobWatchdogGrace):
			a.logError(ctx, logger.CatActor, "job did not stop after context cancellation",
				fmt.Errorf("job stuck for %s after ctx.Done", jobWatchdogGrace),
				"grace_period", jobWatchdogGrace.String(),
			)
		}
	}
}

// ─── run goroutine ──────────────────────────────────────────────────────────

// run 是 agent 的主循环
//
// 接受 ctx / mailbox / done 作为参数（而非从 receiver 读）：
// Start 构造它们并作为局部参数传入，run 就不需要和 Start/Stop 抢锁；
// 即使 Stop 重置了 a.mailbox，这里的局部 mailbox 还指向同一个 chan。
func (a *Agent) run(ctx context.Context, mailbox <-chan job, done chan<- struct{}) {
	a.logInfo(ctx, logger.CatActor, "agent run goroutine started")
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("agent panic: %v", r)
			a.setRuntimeExitErr(err)
			a.logError(ctx, logger.CatActor, "agent run goroutine panic", err)
			a.setRuntimeState(StateStopped)
			close(done)
			// panic 已记录到 exitErr，caller 通过 Err() 可获取；
			// 不再 re-panic：re-panic 会跳过 close(done)，导致 caller 永远阻塞在 Done()
		} else {
			a.logInfo(ctx, logger.CatActor, "agent run goroutine stopped")
			a.setRuntimeState(StateStopped)
			close(done)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			a.setRuntimeState(StateStopping)
			drained := a.drainMailbox(ctx, mailbox)
			a.logInfo(ctx, logger.CatActor, "agent run loop exit",
				"drained_jobs", drained,
			)
			return
		case jb := <-mailbox:
			a.setRuntimeState(StateProcessing)
			a.ResetErrors()
			a.runJob(ctx, jb)
			a.setRuntimeState(StateIdle)
		}
	}
}

// runWithPriorityMailbox 是启用 PriorityMailbox 时的主循环
//
// 优先消费 highCh（委托回传、超时事件），再消费 normalCh（用户 Ask/Submit）。
// 保证异步委托结果不被普通消息阻塞。
func (a *Agent) runWithPriorityMailbox(ctx context.Context, pm *PriorityMailbox, done chan<- struct{}) {
	a.logInfo(ctx, logger.CatActor, "agent run goroutine started")
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("agent panic: %v", r)
			a.setRuntimeExitErr(err)
			a.logError(ctx, logger.CatActor, "agent run goroutine panic", err)
			a.setRuntimeState(StateStopped)
			close(done)
		} else {
			a.logInfo(ctx, logger.CatActor, "agent run goroutine stopped")
			a.setRuntimeState(StateStopped)
			close(done)
		}
	}()

	for {
		// 优先检查 highCh（非阻塞）
		select {
		case <-ctx.Done():
			a.setRuntimeState(StateStopping)
			drained := a.drainPriorityMailbox(ctx, pm)
			a.logInfo(ctx, logger.CatActor, "agent run loop exit",
				"drained_jobs", drained,
			)
			return
		case pj := <-pm.HighCh():
			a.setRuntimeState(StateProcessing)
			a.ResetErrors()
			a.runJob(ctx, pj.job)
			a.setRuntimeState(StateIdle)
			continue
		default:
		}

		// highCh 无消息时，同时等 highCh + normalCh
		select {
		case <-ctx.Done():
			a.setRuntimeState(StateStopping)
			drained := a.drainPriorityMailbox(ctx, pm)
			a.logInfo(ctx, logger.CatActor, "agent run loop exit",
				"drained_jobs", drained,
			)
			return
		case pj := <-pm.HighCh():
			a.setRuntimeState(StateProcessing)
			a.ResetErrors()
			a.runJob(ctx, pj.job)
			a.setRuntimeState(StateIdle)
		case pj := <-pm.NormalCh():
			a.setRuntimeState(StateProcessing)
			a.ResetErrors()
			a.runJob(ctx, pj.job)
			a.setRuntimeState(StateIdle)
		}
	}
}

// drainPriorityMailbox 把已入队的 job 全部以已 canceled 的 ctx 调用一遍
func (a *Agent) drainPriorityMailbox(ctx context.Context, pm *PriorityMailbox) int {
	n := 0
	// 先 drain highCh
	for {
		select {
		case pj := <-pm.HighCh():
			pj.job(ctx)
			n++
		default:
			goto drainNormal
		}
	}
drainNormal:
	// 再 drain normalCh
	for {
		select {
		case pj := <-pm.NormalCh():
			pj.job(ctx)
			n++
		default:
			return n
		}
	}
}

// drainMailbox 把已入队的 job 全部以已 canceled 的 ctx 调用一遍
//
// 目的：让每个 caller 的 Ask 能从 replyCh 拿到结果（通常是 ctx.Canceled），
// 不会永远卡住。
// 不会再从 mailbox 之外读（mailbox 永不 close，send 方会看到 agentDone
// 已 close 后直接返回 ErrStopped）。
//
// 返回 drain 的 job 数量，用于日志统计。
func (a *Agent) drainMailbox(ctx context.Context, mailbox <-chan job) int {
	n := 0
	for {
		select {
		case jb := <-mailbox:
			jb(ctx)
			n++
		default:
			return n
		}
	}
}
