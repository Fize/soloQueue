package agent

import (
	"context"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── Lifecycle ──────────────────────────────────────────────────────────────

// Start 启动 agent 的 run goroutine
//
// 重复 Start 返回 ErrAlreadyStarted。Stop 后可以再次 Start（重置 mailbox 和 exitErr）。
// parent 通常是 context.Background() 或进程级 ctx；parent 取消会让 agent 自动退出。
func (a *Agent) Start(parent context.Context) error {
	if parent == nil {
		parent = context.Background()
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// 如果上次的 done 还没 close 并且 ctx 非 nil，说明 agent 还在运行
	if a.ctx != nil {
		select {
		case <-a.done:
			// 上次已退出，可以重启
		default:
			return ErrAlreadyStarted
		}
	}

	a.ctx, a.cancel = context.WithCancel(parent)
	// agent 自己的 ctx 也注入 actor_id，这样 run/drain 的日志也自动带
	a.ctx = a.ctxWithAgentAttrs(a.ctx)
	a.done = make(chan struct{})
	a.setRuntimeExitErr(nil)
	a.setRuntimeState(StateIdle)

	// 每次 Start 清空会话级确认白名单（对应新 session）
	a.confirmStore.Clear()

	// 根据是否启用 PriorityMailbox 选择 run 函数
	if a.priorityMailbox != nil {
		go a.runWithPriorityMailbox(a.ctx, a.priorityMailbox, a.done)
	} else {
		a.mailbox = make(chan job, a.mailboxCap)
		go a.run(a.ctx, a.mailbox, a.done)
	}

	a.logInfo(a.ctx, logger.CatActor, "agent started",
		"kind", string(a.Def.Kind),
		"role", string(a.Def.Role),
		"model_id", a.Def.ModelID,
		"mailbox_cap", a.mailboxCap,
		"priority_mailbox", a.priorityMailbox != nil,
	)
	return nil
}

// Stop 请求 agent 停止
//
//  1. cancel agent ctx → run goroutine 下轮 select 退出
//  2. 正在执行的 job 其 ctx 也被取消（job 应监听 ctx.Done）
//  3. 已入队的 pending job 会被 drain（每个 job 以已 canceled 的 ctx 调用）
//     使得卡在 reply chan 的 Ask 能返回 ctx.Canceled
//  4. 等待 run goroutine 退出；timeout <= 0 表示无限等待
//
// 超时返回 ErrStopTimeout，但 goroutine 仍会最终退出。
// 未 Start 直接调 Stop 返回 ErrNotStarted。
func (a *Agent) Stop(timeout time.Duration) error {
	a.mu.Lock()
	cancel := a.cancel
	done := a.done
	// 快照 a.ctx：cancel 之后它的 value（actor_id）仍可读，用于 Stop 日志
	stopCtx := a.ctx
	a.mu.Unlock()

	if cancel == nil || done == nil {
		return ErrNotStarted
	}

	a.logInfo(stopCtx, logger.CatActor, "agent stop requested",
		"timeout_ms", timeout.Milliseconds(),
	)

	cancel()

	start := time.Now()
	if timeout <= 0 {
		<-done
		a.logInfo(stopCtx, logger.CatActor, "agent stopped",
			"wait_ms", time.Since(start).Milliseconds(),
		)
		return nil
	}
	select {
	case <-done:
		a.logInfo(stopCtx, logger.CatActor, "agent stopped",
			"wait_ms", time.Since(start).Milliseconds(),
		)
		return nil
	case <-time.After(timeout):
		a.logError(stopCtx, logger.CatActor, "agent stop timeout", ErrStopTimeout)
		return ErrStopTimeout
	}
}

// Done 返回一个 channel，run goroutine 退出后 close
//
// 语义类似 context.Context.Done：可用于 select 等待 agent 退出。
// 未 Start 时返回一个已 close 的 channel（立即可读）。
func (a *Agent) Done() <-chan struct{} {
	a.mu.Lock()
	d := a.done
	a.mu.Unlock()
	if d == nil {
		// 未 Start：返回一个已 close 的 channel
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return d
}

// Err 返回 agent 退出原因
//
//   - nil：未 Start / 正在运行 / 已正常 Stop
//   - non-nil：run goroutine 内部 panic，值为封装的 error
//
// 仅在 <-Done() 之后读取才有定论。
func (a *Agent) Err() error {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	return a.runtime.exitErr
}

func (a *Agent) State() State {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	return a.runtime.state
}
