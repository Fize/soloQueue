package agent

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
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
	results   []string      // 与 toolCalls 等长
	pending   atomic.Int32  // 还剩几个异步调用未完成
	callerCtx context.Context
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

			result, err := action.Target.Ask(delCtx, action.Prompt)
			replyCh <- delegateResult{content: result, err: err}
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
		// caller 取消，清理
		a.turnMu.Lock()
		delete(a.asyncTurns, task.turn.iter)
		a.turnMu.Unlock()
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
