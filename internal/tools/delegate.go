package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// --- Delegate constants ---

const (
	// DelegateDefaultTimeout is the default delegation task timeout.
	DelegateDefaultTimeout = 10 * time.Minute

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
	logger   *logger.Logger // optional logger for delegation tracking

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

// NewDelegateTool creates a new DelegateTool with optional logger.
func NewDelegateTool(leaderID, desc string, timeout time.Duration, locator iface.AgentLocator, l *logger.Logger) *DelegateTool {
	if l == nil {
		var err error
		l, err = logger.System("/tmp", logger.WithConsole(false), logger.WithFile(false))
		if err != nil {
			// Fallback to nil logger is ok here, we check before use
			l = nil
		}
	}
	return &DelegateTool{
		LeaderID: leaderID,
		Desc:     desc,
		Timeout:  timeout,
		Locator:  locator,
		logger:   l,
	}
}

// SetLogger sets the logger for this DelegateTool.
func (dt *DelegateTool) SetLogger(l *logger.Logger) {
	dt.logger = l
}

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
	start := time.Now()

	// 1. Parse arguments
	var dArgs delegateArgs
	if err := json.Unmarshal([]byte(args), &dArgs); err != nil {
		if dt.logger != nil {
			dt.logger.DebugContext(ctx, logger.CatTool, "delegate: invalid args",
				"leader_id", dt.LeaderID,
				"err", err.Error(),
			)
		}
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if dArgs.Task == "" {
		if dt.logger != nil {
			dt.logger.DebugContext(ctx, logger.CatTool, "delegate: task is empty",
				"leader_id", dt.LeaderID,
			)
		}
		return "", fmt.Errorf("delegate: task is empty")
	}

	if dt.logger != nil {
		dt.logger.DebugContext(ctx, logger.CatTool, "delegate: starting synchronous delegation",
			"leader_id", dt.LeaderID,
			"task_len", len(dArgs.Task),
			"timeout_sec", dt.Timeout.Seconds(),
		)
	}

	// 2. Locate or spawn target agent
	var targetAgent iface.Locatable
	var isSpawned bool

	if dt.SpawnFn != nil {
		var err error
		targetAgent, err = dt.SpawnFn(ctx, dArgs.Task)
		if err != nil {
			if dt.logger != nil {
				dt.logger.WarnContext(ctx, logger.CatTool, "delegate: failed to spawn agent",
					"leader_id", dt.LeaderID,
					"err", err.Error(),
					"duration_ms", time.Since(start).Milliseconds(),
				)
			}
			return "", fmt.Errorf("failed to spawn agent '%s': %w", dt.LeaderID, err)
		}
		isSpawned = true

		if dt.logger != nil {
			dt.logger.DebugContext(ctx, logger.CatTool, "delegate: agent spawned",
				"leader_id", dt.LeaderID,
			)
		}
	} else {
		var ok bool
		targetAgent, ok = dt.Locator.Locate(dt.LeaderID)
		if !ok {
			if dt.logger != nil {
				dt.logger.WarnContext(ctx, logger.CatTool, "delegate: target agent not found",
					"leader_id", dt.LeaderID,
					"duration_ms", time.Since(start).Milliseconds(),
				)
			}
			return "", fmt.Errorf("team leader '%s' not found", dt.LeaderID)
		}

		if dt.logger != nil {
			dt.logger.DebugContext(ctx, logger.CatTool, "delegate: target agent located",
				"leader_id", dt.LeaderID,
			)
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

	// 4c. Propagate task-level model override to target agent (L1→L2→L3 chain)
	if params := iface.ModelOverrideFromContext(ctx); params != nil {
		if mo, ok := targetAgent.(iface.ModelOverridable); ok {
			mo.SetModelOverride(params)

			if dt.logger != nil {
				dt.logger.DebugContext(ctx, logger.CatTool, "delegate: model override propagated",
					"leader_id", dt.LeaderID,
					"provider_id", params.ProviderID,
					"model_id", params.ModelID,
				)
			}
		}
	}

	// 5. Call target agent's streaming interface
	if dt.logger != nil {
		dt.logger.DebugContext(ctx, logger.CatTool, "delegate: calling AskStream on target",
			"leader_id", dt.LeaderID,
			"timeout_sec", timeout.Seconds(),
		)
	}

	evCh, err := targetAgent.AskStream(delCtx, dArgs.Task)
	if err != nil {
		if delCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
			if dt.logger != nil {
				dt.logger.WarnContext(ctx, logger.CatTool, "delegate: timeout",
					"leader_id", dt.LeaderID,
					"timeout_sec", timeout.Seconds(),
					"duration_ms", time.Since(start).Milliseconds(),
				)
			}
			return "", fmt.Errorf("delegation to %s timed out after %s", dt.LeaderID, timeout)
		}

		if dt.logger != nil {
			dt.logger.WarnContext(ctx, logger.CatTool, "delegate: AskStream failed",
				"leader_id", dt.LeaderID,
				"err", err.Error(),
				"duration_ms", time.Since(start).Milliseconds(),
			)
		}
		return "", err
	}

	// 6. Consume events: relay to parent, track content/error
	var content string
	var finalErr error
	var eventCount int

	for ev := range evCh {
		if ev == nil {
			continue
		}

		eventCount++

		// Relay event to parent event channel (for ToolNeedsConfirmEvent bubbling)
		if parentEventCh != nil {
			select {
			case parentEventCh <- ev:
				if dt.logger != nil {
					dt.logger.DebugContext(ctx, logger.CatTool, "delegate: event relayed to parent",
						"leader_id", dt.LeaderID,
						"event_type", fmt.Sprintf("%T", ev),
					)
				}
			case <-delCtx.Done():
				// parent cancelled or timed out, stop relaying
				if dt.logger != nil {
					dt.logger.DebugContext(ctx, logger.CatTool, "delegate: parent cancelled, stop relaying",
						"leader_id", dt.LeaderID,
					)
				}
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

			if dt.logger != nil {
				dt.logger.DebugContext(ctx, logger.CatTool, "delegate: confirmation forwarded",
					"leader_id", dt.LeaderID,
					"call_id", callID,
				)
			}
		}

		// Accumulate content delta
		if delta, has := ec.ContentDelta(); has {
			content += delta
		}

		// DoneEvent content overrides accumulated deltas
		if doneContent, has := ec.DoneContent(); has && doneContent != "" {
			content = doneContent

			if dt.logger != nil {
				dt.logger.DebugContext(ctx, logger.CatTool, "delegate: done event received",
					"leader_id", dt.LeaderID,
					"content_len", len(content),
				)
			}
		}

		// Capture error
		if errValue, has := ec.Error(); has && errValue != nil {
			finalErr = errValue

			if dt.logger != nil {
				dt.logger.WarnContext(ctx, logger.CatTool, "delegate: error event received",
					"leader_id", dt.LeaderID,
					"err", errValue.Error(),
				)
			}
		}
	}

	// Notify that delegation is done so the target can be reaped immediately.
	if dn, ok := targetAgent.(iface.DoneNotifier); ok {
		dn.OnDelegationDone()
	}

	if finalErr != nil {
		if dt.logger != nil {
			dt.logger.WarnContext(ctx, logger.CatTool, "delegate: finished with error",
				"leader_id", dt.LeaderID,
				"events_processed", eventCount,
				"duration_ms", time.Since(start).Milliseconds(),
				"err", finalErr.Error(),
			)
		}
		return "", finalErr
	}

	// Check whether the child agent had tool errors even though its LLM
	// produced a normal DoneEvent. This catches cases where L3's tools
	// silently failed and L3's LLM compensated without surfacing errors.
	if et, ok := targetAgent.(iface.ErrorTracker); ok {
		if ec := et.ErrorCount(); ec > 0 {
			prefix := fmt.Sprintf("[WARNING: worker encountered %d error(s) during execution", ec)
			if le := et.LastError(); le != "" {
				prefix += fmt.Sprintf("; last error: %s", le)
			}
			prefix += "]\n"
			content = prefix + content
		}
	}

	if dt.logger != nil {
		dt.logger.DebugContext(ctx, logger.CatTool, "delegate: completed successfully",
			"leader_id", dt.LeaderID,
			"content_len", len(content),
			"events_processed", eventCount,
			"is_spawned", isSpawned,
			"duration_ms", time.Since(start).Milliseconds(),
		)
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
	start := time.Now()

	// 1. Parse arguments
	var dArgs delegateArgs
	if err := json.Unmarshal([]byte(args), &dArgs); err != nil {
		if dt.logger != nil {
			dt.logger.DebugContext(ctx, logger.CatTool, "delegate async: invalid args",
				"leader_id", dt.LeaderID,
				"err", err.Error(),
			)
		}
		return nil, fmt.Errorf("invalid args: %w", err)
	}
	if dArgs.Task == "" {
		if dt.logger != nil {
			dt.logger.DebugContext(ctx, logger.CatTool, "delegate async: task is empty",
				"leader_id", dt.LeaderID,
			)
		}
		return nil, fmt.Errorf("task is empty")
	}

	if dt.logger != nil {
		dt.logger.DebugContext(ctx, logger.CatTool, "delegate async: starting asynchronous delegation",
			"leader_id", dt.LeaderID,
			"task_len", len(dArgs.Task),
		)
	}

	// 2. Locate or spawn target agent (intent only, no execution)
	var target iface.Locatable
	var err error

	if dt.SpawnFn != nil {
		target, err = dt.SpawnFn(ctx, dArgs.Task)
		if err != nil {
			if dt.logger != nil {
				dt.logger.WarnContext(ctx, logger.CatTool, "delegate async: failed to spawn agent",
					"leader_id", dt.LeaderID,
					"err", err.Error(),
				)
			}
			return nil, fmt.Errorf("failed to spawn agent '%s': %w", dt.LeaderID, err)
		}

		if dt.logger != nil {
			dt.logger.DebugContext(ctx, logger.CatTool, "delegate async: agent spawned",
				"leader_id", dt.LeaderID,
			)
		}
	} else if dt.Locator != nil {
		var ok bool
		target, ok = dt.Locator.Locate(dt.LeaderID)
		if !ok {
			if dt.logger != nil {
				dt.logger.WarnContext(ctx, logger.CatTool, "delegate async: target agent not found",
					"leader_id", dt.LeaderID,
				)
			}
			return nil, fmt.Errorf("team leader '%s' not found", dt.LeaderID)
		}

		if dt.logger != nil {
			dt.logger.DebugContext(ctx, logger.CatTool, "delegate async: target agent located",
				"leader_id", dt.LeaderID,
			)
		}
	} else {
		if dt.logger != nil {
			dt.logger.ErrorContext(ctx, logger.CatTool, "delegate async: no Locator or SpawnFn configured",
				"leader_id", dt.LeaderID,
			)
		}
		return nil, fmt.Errorf("delegate tool '%s': no Locator or SpawnFn configured", dt.LeaderID)
	}

	// 2b. Propagate task-level model override to target agent
	if params := iface.ModelOverrideFromContext(ctx); params != nil {
		if mo, ok := target.(iface.ModelOverridable); ok {
			mo.SetModelOverride(params)

			if dt.logger != nil {
				dt.logger.DebugContext(ctx, logger.CatTool, "delegate async: model override propagated",
					"leader_id", dt.LeaderID,
					"provider_id", params.ProviderID,
					"model_id", params.ModelID,
				)
			}
		}
	}

	// 3. Return async intent
	timeout := dt.Timeout
	if timeout <= 0 {
		timeout = DelegateDefaultTimeout
	}
	if timeout > DelegateMaxTimeout {
		timeout = DelegateMaxTimeout
	}

	if dt.logger != nil {
		dt.logger.DebugContext(ctx, logger.CatTool, "delegate async: async action created",
			"leader_id", dt.LeaderID,
			"timeout_sec", timeout.Seconds(),
			"preparation_ms", time.Since(start).Milliseconds(),
		)
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
