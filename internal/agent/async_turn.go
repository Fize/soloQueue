package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/logger"
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
	results   []string
	pending   atomic.Int32 // 还剩几个异步调用未完成
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

		// 注入 model override 到 context，使 DelegateTool 能传播给子 Agent
		asyncCtx := ctx
		if override := a.modelOverride.Load(); override != nil {
			asyncCtx = iface.ContextWithModelOverride(asyncCtx, &iface.ModelOverrideParams{
				ProviderID:      override.ProviderID,
				ModelID:         override.ModelID,
				ThinkingEnabled: override.ThinkingEnabled,
				ReasoningEffort: override.ReasoningEffort,
				Level:           override.Level,
				ContextWindow:   override.ContextWindow,
			})
		}

		// 调用 ExecuteAsync 获取意图（不启动 goroutine）
		action, err := at.ExecuteAsync(asyncCtx, tc.Function.Arguments)
		if err != nil {
			results[i] = "error: " + err.Error()
			continue
		}

		results[i] = formatDelegationStarted(tc)

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

			a.logInfo(delCtx, logger.CatTool, "async-goroutine: starting, about to call AskStream",
				"target_agent_id", task.targetAgentID,
				"timeout", timeout,
			)

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
				defer func() {
					if r := recover(); r != nil {
						a.RecordError(fmt.Errorf("relay goroutine panic: %v", r))
					}
				}()
				for ev := range relayCh {
					if a.Log != nil {
						a.Log.InfoContext(turnState.callerCtx, logger.CatTool, "relay-goroutine: received event from relayCh",
							"event_type", fmt.Sprintf("%T", ev),
						)
					}
					if _, isConfirm := ev.(ToolNeedsConfirmEvent); isConfirm {
						if a.Log != nil {
							a.Log.InfoContext(turnState.callerCtx, logger.CatTool, "relay-goroutine: forwarding confirm event to L1 output")
						}
						if agentEv, ok := ev.(AgentEvent); ok {
							ok := a.emit(turnState.callerCtx, turnState.out, agentEv)
							if a.Log != nil {
								a.Log.InfoContext(turnState.callerCtx, logger.CatTool, "relay-goroutine: emit confirm result",
									"ok", ok,
								)
							}
						} else if a.Log != nil {
							a.Log.WarnContext(turnState.callerCtx, logger.CatTool, "relay-goroutine: confirm event failed AgentEvent assertion",
								"event_type", fmt.Sprintf("%T", ev),
							)
						}
					}
					if ee, isError := ev.(ErrorEvent); isError {
						a.emit(turnState.callerCtx, turnState.out, ee)
					}
				}
			}()

			// --- 用 AskStream + 手动消费替代 Ask ---
			evCh, err := action.Target.AskStream(delCtx, action.Prompt)
			if err != nil {
				if delCtx.Err() == context.DeadlineExceeded {
					if s, ok := action.Target.(interface{ Stop(time.Duration) error }); ok {
						go func() { _ = s.Stop(5 * time.Second) }()
					}
				}
				a.logInfo(delCtx, logger.CatTool, "async-goroutine: AskStream failed",
					"target_agent_id", task.targetAgentID,
					"err", err.Error(),
				)
				close(relayCh)
				<-relayDone
				replyCh <- delegateResult{err: err}
				return
			}
			a.logInfo(delCtx, logger.CatTool, "async-goroutine: AskStream succeeded, consuming events",
				"target_agent_id", task.targetAgentID,
			)

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
					if a.Log != nil {
						a.Log.InfoContext(delCtx, logger.CatTool, "async-goroutine: event not EventConsumer, skipping",
							"event_type", fmt.Sprintf("%T", ev),
						)
					}
					continue
				}

				if callID, has := ec.ConfirmRequest(); has {
					if a.Log != nil {
						a.Log.InfoContext(delCtx, logger.CatTool, "async-goroutine: confirm request detected, firing forwarder",
							"call_id", callID,
							"target_agent_id", task.targetAgentID,
						)
					}
					go func(fc context.Context, cid string, target iface.Locatable) {
						defer func() {
							if r := recover(); r != nil {
								a.RecordError(fmt.Errorf("forwarder goroutine panic: %v", r))
							}
						}()
						forwarder(fc, cid, target)
					}(delCtx, callID, action.Target)
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

			// Stop target agent if delegation timed out.
			if delCtx.Err() == context.DeadlineExceeded {
				if s, ok := action.Target.(interface{ Stop(time.Duration) error }); ok {
					go func() { _ = s.Stop(5 * time.Second) }()
				}
			}

			// Notify that delegation is done so the target can be reaped immediately.
			if dn, ok := action.Target.(iface.DoneNotifier); ok {
				dn.OnDelegationDone()
			}

			replyCh <- delegateResult{content: content, err: finalErr}
		})

		results[i] = "" // 占位
	}

	// 第二阶段：如果有异步工具，注册状态 + 启动 goroutine
	if turnState != nil && turnState.pending.Load() > 0 {
		a.logInfo(ctx, logger.CatTool, "execToolsWithAsync: registering async turn and starting goroutines",
			"agent_id", a.Def.ID,
			"iter", iter,
			"num_async", turnState.pending.Load(),
		)
		a.turnMu.Lock()
		a.asyncTurns[iter] = turnState
		a.turnMu.Unlock()

		// 启动所有异步 goroutine（状态已绝对安全落盘）
		for _, action := range asyncActions {
			go func(act func()) {
				defer func() {
					if r := recover(); r != nil {
						a.RecordError(fmt.Errorf("async action goroutine panic: %v", r))
					}
				}()
				act()
			}(action)
		}

		// 启动结果回收 goroutine（每个 delegatedTask 一个）
		for _, task := range tasks {
			go func(t *delegatedTask) {
				defer func() {
					if r := recover(); r != nil {
						a.RecordError(fmt.Errorf("watchDelegatedTask goroutine panic: %v", r))
					}
				}()
				a.watchDelegatedTask(t)
			}(task)
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
			a.RecordError(result.err)
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
				a.RecordError(result.err)
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
			a.RecordError(errors.New("delegation cancelled"))
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
//  2. 将实际委托结果格式化为 user 消息 push 到 cw
//  3. 发射 DelegationCompletedEvent
//  4. 继续工具循环
func (a *Agent) resumeTurn(turn *asyncTurnState) {
	// 清理 asyncTurns 注册
	a.turnMu.Lock()
	delete(a.asyncTurns, turn.iter)
	a.turnMu.Unlock()

	// 将实际委托结果格式化为 user 消息 push 到 cw
	// 通过 wrapInToolPair 包裹为 assistant(tool_calls) + tool(result) + user(result) 的结构，
	// 确保 LLM 能正确理解这是一次工具调用的返回结果。
	resultMsg := formatDelegationCompleted(turn.toolCalls, turn.results)
	if resultMsg != "" {
		turn.cw.Push(ctxwin.RoleUser, resultMsg)
	}

	// 发射 DelegationCompletedEvent
	a.emit(turn.callerCtx, turn.out, DelegationCompletedEvent{
		Iter:          turn.iter,
		TargetAgentID: turn.agentID,
	})

	// Overflow check: async results may have pushed CW over capacity
	// while the agent was handling user messages.
	if turn.cw.Overflow() {
		current, max, _ := turn.cw.TokenUsage()
		err := fmt.Errorf("context overflow after async delegation: %d tokens exceed effective limit %d",
			current, max)
		a.emit(turn.callerCtx, turn.out, ErrorEvent{Err: err})
		close(turn.out)
		if turn.cancelMerged != nil {
			turn.cancelMerged()
		}
		return
	}

	// 继续工具循环
	yielded := a.continueToolLoop(turn.callerCtx, turn.out, turn.cw, turn.iter+1)

	// Manage the merged context lifecycle.
	//
	// Normal path: continueToolLoop completes fully → cancel merged ctx to
	// prevent leak.
	//
	// Nested async path: continueToolLoop yielded again (another async
	// delegation started in this stream loop). The new asyncTurnState has
	// the same callerCtx (merged) but does NOT have cancelMerged set
	// (saveAsyncCancel is only called by the outer AskStreamWithHistory,
	// not by the inner continueToolLoop). Transfer our cancelMerged to
	// the new turn so it can cancel on its own completion.
	if yielded {
		if turn.cancelMerged != nil {
			a.saveAsyncCancel(turn.callerCtx, turn.cancelMerged)
		}
	} else if turn.cancelMerged != nil {
		turn.cancelMerged()
	}
}

// continueToolLoop 从指定 iter 开始继续工具循环
//
// 逻辑与 runOnceStreamWithHistory 的 for 循环一致，但从 startIter 开始。
// Returns true if the stream loop yielded (another async delegation started).
func (a *Agent) continueToolLoop(
	ctx context.Context,
	out chan<- AgentEvent,
	cw *ctxwin.ContextWindow,
	startIter int,
) bool {
	// 复用 runOnceStreamWithHistory 的循环体
	return a.runOnceStreamWithHistoryFromIter(ctx, cw, out, startIter)
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

// delegationArgs 委托工具的参数结构
type delegationArgs struct {
	Task string `json:"task"`
}

// formatDelegationStarted generates an immediate tool result for async delegation.
// This ensures the assistant(tool_calls) → tool(result) pair is complete in CW,
// preventing interleaved user messages from violating LLM API message ordering.
func formatDelegationStarted(tc llm.ToolCall) string {
	var d delegationArgs
	_ = json.Unmarshal([]byte(tc.Function.Arguments), &d)
	task := d.Task
	if task == "" {
		task = tc.Function.Arguments
	}
	name := tc.Function.Name
	if name == "" {
		name = "delegate"
	}
	return fmt.Sprintf("Delegation started: task assigned via '%s'.\nTask: %s\nWaiting for results...", name, task)
}

// formatDelegationCompleted builds a user message containing completed delegation results.
// It filters only delegate_* tool calls and formats their results as:
//
//	[Delegation Completed]
//
//	Task: {task description}
//	Assigned to: {agent ID}
//	Result:
//	{result content}
func formatDelegationCompleted(toolCalls []llm.ToolCall, results []string) string {
	var sb strings.Builder
	sb.WriteString("[Delegation Completed]\n\n")
	hasResults := false
	for i, tc := range toolCalls {
		if !strings.HasPrefix(tc.Function.Name, "delegate_") {
			continue
		}
		result := ""
		if i < len(results) {
			result = results[i]
		}
		var d delegationArgs
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &d)
		task := d.Task
		if task == "" {
			task = tc.Function.Arguments
		}
		sb.WriteString(fmt.Sprintf("Task: %s\n", task))
		sb.WriteString("Result:\n")
		sb.WriteString(result)
		sb.WriteString("\n\n")
		hasResults = true
	}
	if !hasResults {
		return ""
	}
	return sb.String()
}
