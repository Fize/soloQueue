package agent

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── run goroutine ──────────────────────────────────────────────────────────

// run 是 agent 的主循环
//
// 接受 ctx / mailbox / done 作为参数（而非从 receiver 读）：
// Start 构造它们并作为局部参数传入，run 就不需要和 Start/Stop 抢锁；
// 即使 Stop 重置了 a.mailbox，这里的局部 mailbox 还指向同一个 chan。
func (a *Agent) run(ctx context.Context, mailbox <-chan job, done chan<- struct{}) {
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("agent panic: %v", r)
			a.exitErr.Store(errHolder{err: err})
			a.logError(ctx, logger.CatActor, "agent run goroutine panic", err)
			a.state.Store(int32(StateStopped))
			close(done)
			// panic 已记录到 exitErr，caller 通过 Err() 可获取；
			// 不再 re-panic：re-panic 会跳过 close(done)，导致 caller 永远阻塞在 Done()
		} else {
			a.state.Store(int32(StateStopped))
			close(done)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			a.state.Store(int32(StateStopping))
			drained := a.drainMailbox(ctx, mailbox)
			a.logInfo(ctx, logger.CatActor, "agent run loop exit",
				slog.Int("drained_jobs", drained),
			)
			return
		case jb := <-mailbox:
			a.state.Store(int32(StateProcessing))
			jb(ctx)
			a.state.Store(int32(StateIdle))
		}
	}
}

// runWithPriorityMailbox 是启用 PriorityMailbox 时的主循环
//
// 优先消费 highCh（委托回传、超时事件），再消费 normalCh（用户 Ask/Submit）。
// 保证异步委托结果不被普通消息阻塞。
func (a *Agent) runWithPriorityMailbox(ctx context.Context, pm *PriorityMailbox, done chan<- struct{}) {
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("agent panic: %v", r)
			a.exitErr.Store(errHolder{err: err})
			a.logError(ctx, logger.CatActor, "agent run goroutine panic", err)
			a.state.Store(int32(StateStopped))
			close(done)
		} else {
			a.state.Store(int32(StateStopped))
			close(done)
		}
	}()

	for {
		// 优先检查 highCh（非阻塞）
		select {
		case <-ctx.Done():
			a.state.Store(int32(StateStopping))
			drained := a.drainPriorityMailbox(ctx, pm)
			a.logInfo(ctx, logger.CatActor, "agent run loop exit",
				slog.Int("drained_jobs", drained),
			)
			return
		case pj := <-pm.HighCh():
			a.state.Store(int32(StateProcessing))
			pj.job(ctx)
			a.state.Store(int32(StateIdle))
			continue
		default:
		}

		// highCh 无消息时，同时等 highCh + normalCh
		select {
		case <-ctx.Done():
			a.state.Store(int32(StateStopping))
			drained := a.drainPriorityMailbox(ctx, pm)
			a.logInfo(ctx, logger.CatActor, "agent run loop exit",
				slog.Int("drained_jobs", drained),
			)
			return
		case pj := <-pm.HighCh():
			a.state.Store(int32(StateProcessing))
			pj.job(ctx)
			a.state.Store(int32(StateIdle))
		case pj := <-pm.NormalCh():
			a.state.Store(int32(StateProcessing))
			pj.job(ctx)
			a.state.Store(int32(StateIdle))
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
