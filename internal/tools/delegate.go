package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/iface"
)

// --- Delegate constants ---

const (
	// DelegateDefaultTimeout is the default delegation task timeout.
	DelegateDefaultTimeout = 5 * time.Minute

	// DelegateMaxTimeout is the maximum allowed delegation task timeout.
	DelegateMaxTimeout = 15 * time.Minute
)

// --- DelegateTool ---

// delegateArgs is the parameter struct for DelegateTool.
type delegateArgs struct {
	Task string `json:"task"`
}

// Pre-computed parameter schema.
var delegateParamsSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "task": {
      "type": "string",
      "description": "Task description to delegate"
    }
  },
  "required": ["task"]
}`)

// DelegateTool delegates tasks to other agents.
//
// Implements the Tool interface and is registered in ToolRegistry so the
// LLM can invoke it via function calling (e.g., delegate_dev(task="...")).
//
// Two modes share one struct because they have identical LLM-facing schemas
// and 90% shared Execute logic (arg parsing, timeout, event consumption).
// Mode is determined at wiring time (factory), not at runtime:
//
//   - Synchronous (L2 -> L3): SpawnFn is nil, uses Locator to find an
//     already-registered agent. Execute blocks until the child completes.
//   - Asynchronous (L1 -> L2): SpawnFn is non-nil, used to dynamically
//     spawn or locate the target agent. Implements AsyncTool so the
//     framework manages goroutine lifecycle.
//
// If the two modes diverge significantly, split into separate types.
type DelegateTool struct {
	LeaderID string        // target agent identifier (e.g., "dev")
	Desc     string        // leader description (for Tool.Description)
	Timeout  time.Duration

	// Synchronous mode (L2 -> L3): look up an already-registered agent.
	Locator iface.AgentLocator

	// Asynchronous mode (L1 -> L2): closure-injected, dynamically spawns
	// or locates the target agent.
	// nil = synchronous mode (use Locator)
	// non-nil = asynchronous mode (AsyncTool.ExecuteAsync path)
	SpawnFn func(ctx context.Context, task string) (iface.Locatable, error)
}

// Compile-time interface checks.
var (
	_ Tool      = (*DelegateTool)(nil)
	_ AsyncTool = (*DelegateTool)(nil)
)

func (dt *DelegateTool) Name() string {
	return "delegate_" + dt.LeaderID
}

func (dt *DelegateTool) Description() string {
	return fmt.Sprintf("Delegate a task to team leader '%s': %s", dt.LeaderID, dt.Desc)
}

func (dt *DelegateTool) Parameters() json.RawMessage {
	return delegateParamsSchema
}

// Execute runs delegation synchronously (L2 -> L3 path).
//
// Calls AskStream on the target agent, consumes the event stream, relays
// ToolNeedsConfirmEvent to the parent event channel (if injected via
// context), and accumulates the final content or error.
func (dt *DelegateTool) Execute(ctx context.Context, args string) (string, error) {
	// 1. Parse arguments
	var dArgs delegateArgs
	if err := json.Unmarshal([]byte(args), &dArgs); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if dArgs.Task == "" {
		return "error: task is empty", nil
	}

	// 2. Locate or spawn target agent
	var targetAgent iface.Locatable
	if dt.SpawnFn != nil {
		var err error
		targetAgent, err = dt.SpawnFn(ctx, dArgs.Task)
		if err != nil {
			return fmt.Sprintf("error: failed to spawn agent '%s': %s", dt.LeaderID, err), nil
		}
	} else {
		var ok bool
		targetAgent, ok = dt.Locator.Locate(dt.LeaderID)
		if !ok {
			return fmt.Sprintf("error: team leader '%s' not found", dt.LeaderID), nil
		}
	}

	// 3. Apply delegation timeout
	timeout := dt.Timeout
	if timeout <= 0 {
		timeout = DelegateDefaultTimeout
	}
	if timeout > DelegateMaxTimeout {
		timeout = DelegateMaxTimeout
	}

	delCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 4. Extract parent event channel (injected by agent.execToolStream)
	parentEventCh, _ := ToolEventChannelFromCtx(ctx)

	// 4b. Extract confirm forwarder (injected by agent.execToolStream)
	confirmFwd, hasConfirmFwd := ConfirmForwarderFromCtx(ctx)

	// 5. Call target agent's streaming interface
	evCh, err := targetAgent.AskStream(delCtx, dArgs.Task)
	if err != nil {
		if delCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
			return fmt.Sprintf("error: delegation to %s timed out after %s, task has been cancelled", dt.LeaderID, timeout), nil
		}
		return "error: " + err.Error(), nil
	}

	// 6. Consume events: relay to parent, track content/error
	var content string
	var finalErr error

	for ev := range evCh {
		if ev == nil {
			continue
		}

		// Relay event to parent event channel (for ToolNeedsConfirmEvent bubbling)
		if parentEventCh != nil {
			select {
			case parentEventCh <- ev:
			case <-delCtx.Done():
				// parent cancelled or timed out, stop relaying
			}
		}

		// Extract typed data via EventConsumer (no reflection needed)
		ec, ok := ev.(iface.EventConsumer)
		if !ok {
			continue
		}

		// Route confirmation requests to parent agent
		if callID, has := ec.ConfirmRequest(); has && hasConfirmFwd {
			go confirmFwd(delCtx, callID, targetAgent)
		}

		// Accumulate content delta
		if delta, has := ec.ContentDelta(); has {
			content += delta
		}

		// DoneEvent content overrides accumulated deltas
		if doneContent, has := ec.DoneContent(); has && doneContent != "" {
			content = doneContent
		}

		// Capture error
		if errValue, has := ec.Error(); has && errValue != nil {
			finalErr = errValue
		}
	}

	if finalErr != nil {
		return "", finalErr
	}

	return content, nil
}

// --- Context helpers for event relay & confirm routing ---

// toolEventChannelCtxKey is the context key for the parent event relay channel.
// Defined here as the single source of truth; the agent package uses the
// exported helper functions to inject/extract it.
type toolEventChannelCtxKey struct{}

// WithToolEventChannel injects a parent event relay channel into context.
// Called by agent.execToolStream before invoking tool.Execute.
func WithToolEventChannel(ctx context.Context, ch chan<- iface.AgentEvent) context.Context {
	return context.WithValue(ctx, toolEventChannelCtxKey{}, ch)
}

// ToolEventChannelFromCtx extracts the parent event relay channel from context.
// Used by DelegateTool.Execute to relay child agent events to the parent.
func ToolEventChannelFromCtx(ctx context.Context) (chan<- iface.AgentEvent, bool) {
	ch, ok := ctx.Value(toolEventChannelCtxKey{}).(chan<- iface.AgentEvent)
	return ch, ok
}

// confirmForwarderCtxKey is the context key for the confirm forwarder closure.
type confirmForwarderCtxKey struct{}

// WithConfirmForwarder injects a ConfirmForwarder into context.
func WithConfirmForwarder(ctx context.Context, f iface.ConfirmForwarder) context.Context {
	return context.WithValue(ctx, confirmForwarderCtxKey{}, f)
}

// ConfirmForwarderFromCtx extracts the ConfirmForwarder from context.
func ConfirmForwarderFromCtx(ctx context.Context) (iface.ConfirmForwarder, bool) {
	f, ok := ctx.Value(confirmForwarderCtxKey{}).(iface.ConfirmForwarder)
	return f, ok
}

// --- ExecuteAsync (L1 -> L2 asynchronous path) ---

// ExecuteAsync implements the AsyncTool interface for asynchronous delegation.
//
// It only declares the async execution intent — no goroutine is started.
// The framework layer is responsible for:
//  1. Assembling asyncTurnState (cw, out, results all in place)
//  2. Registering the state in Agent.asyncTurns
//  3. Starting goroutines to execute Ask
//  4. Monitoring results and resuming the tool loop
func (dt *DelegateTool) ExecuteAsync(ctx context.Context, args string) (*AsyncAction, error) {
	// 1. Parse arguments
	var dArgs delegateArgs
	if err := json.Unmarshal([]byte(args), &dArgs); err != nil {
		return nil, fmt.Errorf("invalid args: %w", err)
	}
	if dArgs.Task == "" {
		return nil, fmt.Errorf("task is empty")
	}

	// 2. Locate or spawn target agent (intent only, no execution)
	var target iface.Locatable
	var err error

	if dt.SpawnFn != nil {
		target, err = dt.SpawnFn(ctx, dArgs.Task)
		if err != nil {
			return nil, fmt.Errorf("failed to spawn agent '%s': %w", dt.LeaderID, err)
		}
	} else if dt.Locator != nil {
		var ok bool
		target, ok = dt.Locator.Locate(dt.LeaderID)
		if !ok {
			return nil, fmt.Errorf("team leader '%s' not found", dt.LeaderID)
		}
	} else {
		return nil, fmt.Errorf("delegate tool '%s': no Locator or SpawnFn configured", dt.LeaderID)
	}

	// 3. Return async intent
	timeout := dt.Timeout
	if timeout <= 0 {
		timeout = DelegateDefaultTimeout
	}
	if timeout > DelegateMaxTimeout {
		timeout = DelegateMaxTimeout
	}

	return &AsyncAction{
		Target:  target,
		Prompt:  dArgs.Task,
		Timeout: timeout,
	}, nil
}

// IsAsync returns whether this DelegateTool is configured for async mode.
func (dt *DelegateTool) IsAsync() bool {
	return dt.SpawnFn != nil
}
