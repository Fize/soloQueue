package agent

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ─── delegatedTask ─────────────────────────────────────────────────────────

// delegatedTask 单个委托追踪（单一职责）
//
// 每个异步 tool_call 对应一个 delegatedTask，由框架在 execTools 中创建。
// 它不直接管理聚合——聚合由 asyncTurnState 负责。
type delegatedTask struct {
	correlationID string
	targetAgentID string
	replyCh       chan delegateResult
	callID        string          // 属于哪个 tool_call
	callIndex     int             // 在 toolCalls 中的位置
	turn          *asyncTurnState // 反向引用所属轮次
}

type delegateResult struct {
	content string
	err     error
}

// ─── asyncTurnState ────────────────────────────────────────────────────────

// asyncTurnState 一轮异步状态（聚合层）
//
// 当一轮 tool_calls 中有至少一个异步工具时，execTools 创建此结构。
// 它追踪本轮所有 tool_call 的结果（同步的立即填，异步的回传时填），
// 并在最后一个异步结果到达时触发 tool loop 恢复。
type asyncTurnState struct {
	agentID   string
	out       chan<- AgentEvent
	cw        *ctxwin.ContextWindow
	iter      int
	toolCalls []llm.ToolCall
	// results holds tool execution results, indexed by tool call position.
	//
	// CONCURRENCY SAFETY: Each watchDelegatedTask goroutine writes to a
	// distinct index (its own callIndex), never overlapping with other
	// writers. The main goroutine only reads results in resumeTurn, which
	// runs after ALL goroutines have completed. The happens-before
	// relationship is established by:
	//   1. watchDelegatedTask writes results[callIndex] THEN does pending.Add(-1)
	//   2. The last Add(-1) returning 0 triggers submitHighPriority(resumeTurn)
	//   3. resumeTurn reads results[] after the atomic publish
	//
	// The slice header (len/cap/ptr) is never modified after creation.
	// This pattern is safe per the Go memory model for non-overlapping
	// writes to different indices of a fixed-size slice.
	results []string
	pending   atomic.Int32  // 还剩几个异步调用未完成
	callerCtx context.Context

	// cancelMerged holds the cancel function for the merged context.
	//
	// When streamLoop yields for async delegation, the job closure in
	// AskStreamWithHistory must NOT cancel the merged context (callerCtx)
	// — resumeTurn still needs it. Instead, the cancel is deferred here
	// and invoked after the final streamLoop completes in resumeTurn.
	cancelMerged context.CancelFunc
}

// ─── execTools 异步路径 ────────────────────────────────────────────────────

// execToolsWithAsync 是 execTools 的异步感知版本
//
// 流程：
//  1. 遍历所有 toolCalls，识别 AsyncTool
//  2. 对 AsyncTool 调用 ExecuteAsync 获取意图（不启动 goroutine）
//  3. 组装 asyncTurnState（所有上下文 100% 就位）
//  4. 注册 asyncTurnState 到 Agent
//  5. 启动所有异步 goroutine
//  6. 同步工具正常执行
//
// 返回的 results 中，异步工具的位置为占位空字符串（""）。
func (a *Agent) execToolsWithAsync(
	ctx context.Context,
	iter int,
	calls []llm.ToolCall,
	out chan<- AgentEvent,
	cw *ctxwin.ContextWindow,
) []string {
	results := make([]string, len(calls))

	// 第一阶段：识别异步工具 + 预创建 asyncTurnState
	var (
		turnState    *asyncTurnState
		asyncActions []func() // 收集需要异步启动的闭包
		tasks        []*delegatedTask
	)

	for i, tc := range calls {
		if err := ctx.Err(); err != nil {
			results[i] = "error: " + err.Error()
			continue
		}

		tool, ok := a.tools.SafeGet(tc.Function.Name)
		if !ok {
			results[i] = a.execToolStream(ctx, iter, tc, out)
			continue
		}

		// 检查是否为 AsyncTool
		at, isAsync := tool.(tools.AsyncTool)
		if !isAsync {
			// 同步工具：正常执行
			results[i] = a.execToolStream(ctx, iter, tc, out)
			continue
		}

		// 懒初始化 turnState（第一个异步工具时创建）
		if turnState == nil {
			turnState = &asyncTurnState{
				agentID:   a.Def.ID,
				out:       out,
				cw:        cw,
				iter:      iter,
				toolCalls: calls,
				results:   results,
				callerCtx: ctx,
			}
		}

		// 调用 ExecuteAsync 获取意图（不启动 goroutine）
		action, err := at.ExecuteAsync(ctx, tc.Function.Arguments)
		if err != nil {
			results[i] = "error: " + err.Error()
			continue
		}

		turnState.pending.Add(1)
		replyCh := make(chan delegateResult, 1)

		// 组装 delegatedTask（此时 turnState 100% 就位）
		task := &delegatedTask{
			correlationID: generateCorrID(),
			targetAgentID: action.TargetID(),
			replyCh:       replyCh,
			callID:        tc.ID,
			callIndex:     i,
			turn:          turnState,
		}
		tasks = append(tasks, task)

		// 收集异步启动闭包（还没 go！）
		asyncActions = append(asyncActions, func() {
			timeout := action.Timeout
			if timeout <= 0 {
				timeout = tools.DelegateDefaultTimeout
			}
			delCtx, cancel := context.WithTimeout(turnState.callerCtx, timeout)
			defer cancel()

			// --- 注入 confirm relay（与 execToolStream 同步路径对齐） ---
			relayCh := make(chan iface.AgentEvent, 16)

			forwarder := iface.ConfirmForwarder(func(fwdCtx context.Context, callID string, child iface.Locatable) (string, error) {
				slot := &confirmSlot{ch: make(chan string, 1)}
				a.confirmMu.Lock()
				a.pendingConfirm[callID] = slot
				a.confirmMu.Unlock()

				defer func() {
					a.confirmMu.Lock()
					delete(a.pendingConfirm, callID)
					a.confirmMu.Unlock()
				}()

				select {
				case choice := <-slot.ch:
					if err := child.Confirm(callID, choice); err != nil {
						return "", err
					}
					return choice, nil
				case <-fwdCtx.Done():
					return "", fwdCtx.Err()
				}
			})

			relayDone := make(chan struct{})
			go func() {
				defer close(relayDone)
				for ev := range relayCh {
					if _, isConfirm := ev.(ToolNeedsConfirmEvent); isConfirm {
						a.emit(turnState.callerCtx, turnState.out, ev.(AgentEvent))
					}
				}
			}()

			// --- 用 AskStream + 手动消费替代 Ask ---
			evCh, err := action.Target.AskStream(delCtx, action.Prompt)
			if err != nil {
				close(relayCh)
				<-relayDone
				replyCh <- delegateResult{err: err}
				return
			}

			var content string
			var finalErr error
			for ev := range evCh {
				if ev == nil {
					continue
				}

				select {
				case relayCh <- ev:
				case <-delCtx.Done():
				}

				ec, ok := ev.(iface.EventConsumer)
				if !ok {
					continue
				}

				if callID, has := ec.ConfirmRequest(); has {
					go forwarder(delCtx, callID, action.Target)
				}

				if delta, has := ec.ContentDelta(); has {
					content += delta
				}
				if doneContent, has := ec.DoneContent(); has && doneContent != "" {
					content = doneContent
				}
				if errValue, has := ec.Error(); has && errValue != nil {
					finalErr = errValue
				}
			}

			close(relayCh)
			<-relayDone

			replyCh <- delegateResult{content: content, err: finalErr}
		})

		results[i] = "" // 占位
	}

	// 第二阶段：如果有异步工具，注册状态 + 启动 goroutine
	if turnState != nil && turnState.pending.Load() > 0 {
		a.turnMu.Lock()
		a.asyncTurns[iter] = turnState
		a.turnMu.Unlock()

		// 启动所有异步 goroutine（状态已绝对安全落盘）
		for _, action := range asyncActions {
			go action()
		}

		// 启动结果回收 goroutine（每个 delegatedTask 一个）
		for _, task := range tasks {
			go a.watchDelegatedTask(task)
		}
	}

	return results
}

// watchDelegatedTask 监听单个委托的结果
//
// 在 goroutine 中运行，结果到达后：
//  1. 填入 results[callIndex]
//  2. pending.Add(-1)
//  3. 如果 pending == 0（全部完成），投递高优先级 job 恢复 tool loop
func (a *Agent) watchDelegatedTask(task *delegatedTask) {
	select {
	case result := <-task.replyCh:
		// 填入结果
		toolResult := result.content
		if result.err != nil {
			toolResult = "error: " + result.err.Error()
		}
		task.turn.results[task.callIndex] = toolResult

		// 检查是否全部完成
		if task.turn.pending.Add(-1) == 0 {
			// 全部异步结果已到齐，投递高优先级 job 恢复 tool loop
			a.submitHighPriority(func(ctx context.Context) {
				a.resumeTurn(task.turn)
			})
		}
	case <-task.turn.callerCtx.Done():
		// Caller context cancelled. Give replyCh a short grace period —
		// the result might already be in-flight and arrive momentarily.
		select {
		case result := <-task.replyCh:
			toolResult := result.content
			if result.err != nil {
				toolResult = "error: " + result.err.Error()
			}
			task.turn.results[task.callIndex] = toolResult

			if task.turn.pending.Add(-1) == 0 {
				a.submitHighPriority(func(ctx context.Context) {
					a.resumeTurn(task.turn)
				})
			}
		case <-time.After(100 * time.Millisecond):
			// Genuinely cancelled — fill a synthetic error result and
			// ensure resumeTurn is still triggered so out gets closed.
			task.turn.results[task.callIndex] = "error: delegation cancelled"
			if task.turn.pending.Add(-1) == 0 {
				a.submitHighPriority(func(ctx context.Context) {
					a.resumeTurn(task.turn)
				})
			}
		}
	}
}

// resumeTurn 恢复 tool loop
//
// 由高优先级 job 调用。此时所有异步结果已到齐：
//  1. 清理 asyncTurns 注册
//  2. push 所有 tool result 到 cw
//  3. 发射 DelegationCompletedEvent
//  4. 继续工具循环
func (a *Agent) resumeTurn(turn *asyncTurnState) {
	// 清理 asyncTurns 注册
	a.turnMu.Lock()
	delete(a.asyncTurns, turn.iter)
	a.turnMu.Unlock()

	// push 所有 tool result 到 cw
	for i, tc := range turn.toolCalls {
		turn.cw.Push(ctxwin.RoleTool, turn.results[i],
			ctxwin.WithToolCallID(tc.ID),
			ctxwin.WithToolName(tc.Function.Name),
			ctxwin.WithEphemeral(true),
		)
	}

	// 发射 DelegationCompletedEvent
	a.emit(turn.callerCtx, turn.out, DelegationCompletedEvent{
		Iter:          turn.iter,
		TargetAgentID: turn.agentID,
	})

	// 继续工具循环
	a.continueToolLoop(turn.callerCtx, turn.out, turn.cw, turn.iter+1)

	// Final streamLoop completed — cancel the merged context that was
	// kept alive for resumeTurn. This prevents context leak.
	if turn.cancelMerged != nil {
		turn.cancelMerged()
	}
}

// continueToolLoop 从指定 iter 开始继续工具循环
//
// 逻辑与 runOnceStreamWithHistory 的 for 循环一致，但从 startIter 开始。
func (a *Agent) continueToolLoop(
	ctx context.Context,
	out chan<- AgentEvent,
	cw *ctxwin.ContextWindow,
	startIter int,
) {
	// 复用 runOnceStreamWithHistory 的循环体
	a.runOnceStreamWithHistoryFromIter(ctx, cw, out, startIter)
}

// generateCorrID 生成一个唯一的 correlation ID
func generateCorrID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// saveAsyncCancel stores the cancel function into the asyncTurnState whose
// callerCtx matches ctx. Called by AskStreamWithHistory when streamLoop
// yields so that resumeTurn can cancel the merged context after the final
// streamLoop completes.
//
// If no matching asyncTurnState is found (should not happen), cancel is
// invoked immediately to prevent context leak.
func (a *Agent) saveAsyncCancel(ctx context.Context, cancel context.CancelFunc) {
	a.turnMu.Lock()
	defer a.turnMu.Unlock()
	for _, ts := range a.asyncTurns {
		if ts.callerCtx == ctx {
			ts.cancelMerged = cancel
			return
		}
	}
	// Defensive: no matching turn found — cancel now to prevent leak.
	cancel()
}
