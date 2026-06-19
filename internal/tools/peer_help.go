package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// maxPeerDepth limits how many L2→L2 hops a single task can make.
// dev → ops → qa is the limit; deeper chains indicate a decomposition
// problem and should escalate to L1 instead.
const maxPeerDepth = 2

// ─── Delegation chain context propagation ────────────────────────────────────

type delegationChainCtxKey struct{}

// ContextWithDelegationChain injects the L2 peer delegation chain into context.
// The chain is the list of team names visited so far, in order.
// Used by RequestTeamHelpTool to detect cycles and enforce depth limits.
func ContextWithDelegationChain(ctx context.Context, chain []string) context.Context {
	if len(chain) == 0 {
		return ctx
	}
	return context.WithValue(ctx, delegationChainCtxKey{}, chain)
}

// DelegationChainFromContext extracts the peer delegation chain from context.
// Returns nil if no chain is set (top-level call).
func DelegationChainFromContext(ctx context.Context) []string {
	v, _ := ctx.Value(delegationChainCtxKey{}).([]string)
	return v
}

// ─── PeerTeamInfo: read-only team catalog entry ──────────────────────────────

// PeerTeamInfo describes a peer team that an L2 leader can request help from.
type PeerTeamInfo struct {
	Name             string `json:"name"`               // team leader name (template ID)
	Group            string `json:"group"`              // team group name
	LeaderDescription string `json:"leader_description"` // one-line capability summary
	WorkerCount      int    `json:"worker_count"`        // number of L3 workers in the team
}

// ─── ListPeerTeamsTool ───────────────────────────────────────────────────────

// ListPeerTeamsTool is a read-only tool that lists peer teams available for
// cross-team help. Excludes the caller's own team.
//
// The catalog is injected at factory time (a snapshot of Stack.Leaders +
// Stack.Groups). It is read-only and safe for concurrent use.
type ListPeerTeamsTool struct {
	selfName string
	catalog  []PeerTeamInfo
}

// NewListPeerTeamsTool creates a ListPeerTeamsTool.
// selfName is the caller's own team leader name, which is excluded from results.
func NewListPeerTeamsTool(catalog []PeerTeamInfo, selfName string) *ListPeerTeamsTool {
	return &ListPeerTeamsTool{catalog: catalog, selfName: selfName}
}

func (t *ListPeerTeamsTool) Name() string { return "list_peer_teams" }

func (t *ListPeerTeamsTool) Description() string {
	return "List peer teams available for cross-team help. " +
		"Call this first to discover which teams exist and what they can do, " +
		"then use request_team_help to delegate a sub-task to a specific team. " +
		"Your own team is excluded from the list."
}

func (t *ListPeerTeamsTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {}
}`)
}

func (t *ListPeerTeamsTool) Execute(ctx context.Context, args string) (string, error) {
	peers := make([]PeerTeamInfo, 0, len(t.catalog))
	for _, p := range t.catalog {
		if p.Name == t.selfName {
			continue
		}
		peers = append(peers, p)
	}
	if len(peers) == 0 {
		return "No peer teams available. You are the only team or no other teams are configured.", nil
	}
	data, err := json.MarshalIndent(peers, "", "  ")
	if err != nil {
		return "", fmt.Errorf("list_peer_teams: marshal failed: %w", err)
	}
	return string(data), nil
}

// ─── RequestTeamHelpTool ─────────────────────────────────────────────────────

// requestTeamHelpArgs is the parameter struct for RequestTeamHelpTool.
type requestTeamHelpArgs struct {
	TeamName string `json:"team_name"` // target team leader name
	Task     string `json:"task"`      // task description for the target team
	Context  string `json:"context"`   // background context to pass along
}

var requestTeamHelpParamsSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "team_name": {
      "type": "string",
      "description": "Name of the target team leader to request help from (from list_peer_teams)"
    },
    "task": {
      "type": "string",
      "description": "Clear, self-contained task description for the target team"
    },
    "context": {
      "type": "string",
      "description": "Background context: what you've done so far, why you need help, and any constraints"
    }
  },
  "required": ["team_name", "task", "context"]
}`)

// LocateOrSpawnFn locates an idle instance of the target team leader, or spawns
// a new one if none are idle. Returns the Locatable and whether it was freshly
// spawned (vs reused). The reap function, if non-nil, is called after the
// delegation completes to clean up spawned instances.
//
// This is a function type (not an interface) to avoid importing the agent
// package — the factory layer injects a closure that wraps
// Registry.LocateIdle + AgentFactory.Create.
type LocateOrSpawnFn func(ctx context.Context, teamName string) (iface.Locatable, bool, error)

// ReapFn cleans up a spawned agent instance after delegation completes.
// May be nil if the instance was reused (not spawned).
type ReapFn func(loc iface.Locatable)

// RequestTeamHelpTool lets an L2 team leader delegate a sub-task to a peer
// L2 team leader. This is a synchronous, lateral delegation channel that
// bypasses L1 for genuine cross-domain help requests.
//
// Safety mechanisms:
//   - Cycle detection: the delegation chain is propagated via context. If the
//     target team is already in the chain, the request is rejected immediately.
//   - Depth limit: maxPeerDepth (2) bounds the L2→L2 hop count.
//   - Spawn/reap: target instances are located via LocateIdle first (reusing
//     idle agents), falling back to spawning new instances that are reaped
//     after the delegation completes.
type RequestTeamHelpTool struct {
	selfName string
	locateOrSpawn LocateOrSpawnFn
	reap          ReapFn
	timeout       time.Duration
	logger        *logger.Logger
}

// NewRequestTeamHelpTool creates a RequestTeamHelpTool without a separate reap
// function. Use this when the target instances are managed externally (e.g.
// reused from an existing supervisor) and should not be reaped.
func NewRequestTeamHelpTool(selfName string, locate LocateOrSpawnFn, reap ReapFn, timeout time.Duration) *RequestTeamHelpTool {
	return NewRequestTeamHelpToolWithReap(selfName, locate, reap, timeout)
}

// NewRequestTeamHelpToolWithReap creates a RequestTeamHelpTool with an explicit
// reap function called after each delegation completes.
func NewRequestTeamHelpToolWithReap(selfName string, locate LocateOrSpawnFn, reap ReapFn, timeout time.Duration) *RequestTeamHelpTool {
	t := &RequestTeamHelpTool{
		selfName:      selfName,
		locateOrSpawn: locate,
		reap:          reap,
		timeout:       timeout,
	}
	if l, err := logger.System("/tmp", logger.WithConsole(false), logger.WithFile(false)); err == nil {
		t.logger = l
	}
	return t
}

// SetLogger sets the logger for this tool.
func (t *RequestTeamHelpTool) SetLogger(l *logger.Logger) { t.logger = l }

func (t *RequestTeamHelpTool) Name() string { return "request_team_help" }

func (t *RequestTeamHelpTool) Description() string {
	return "Request help from a peer team leader. Use when your team lacks the " +
		"capability to handle a sub-task and another team can help. " +
		"First call list_peer_teams to see available teams. " +
		"Do NOT use this for tasks your own team can handle, and do NOT " +
		"form delegation loops (the system will reject cyclic requests)."
}

func (t *RequestTeamHelpTool) Parameters() json.RawMessage {
	return requestTeamHelpParamsSchema
}

// PreferredTimeout returns the delegation timeout for agent-level scheduling.
func (t *RequestTeamHelpTool) PreferredTimeout() time.Duration {
	if t.timeout <= 0 {
		return DelegateDefaultTimeout
	}
	return t.timeout
}

func (t *RequestTeamHelpTool) Execute(ctx context.Context, args string) (string, error) {
	start := time.Now()

	// 1. Parse arguments.
	var a requestTeamHelpArgs
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return "", fmt.Errorf("request_team_help: invalid args: %w", err)
	}
	if a.TeamName == "" {
		return "", fmt.Errorf("request_team_help: team_name is required")
	}
	if a.Task == "" {
		return "", fmt.Errorf("request_team_help: task is required")
	}

	// 2. Cycle detection: reject if target is already in the chain.
	chain := DelegationChainFromContext(ctx)
	for _, name := range chain {
		if strings.EqualFold(name, a.TeamName) {
			if t.logger != nil {
				t.logger.WarnContext(ctx, logger.CatTool, "request_team_help: cycle detected",
					"self", t.selfName,
					"target", a.TeamName,
					"chain", strings.Join(chain, " → "),
				)
			}
			return "", fmt.Errorf("request_team_help: delegation cycle detected (%s is already in the chain: %s) — refusing to delegate to avoid infinite loop",
				a.TeamName, strings.Join(append(chain, t.selfName), " → "))
		}
	}

	// 3. Depth limit: reject if chain is already at max depth.
	if len(chain) >= maxPeerDepth {
		if t.logger != nil {
			t.logger.WarnContext(ctx, logger.CatTool, "request_team_help: depth limit exceeded",
				"self", t.selfName,
				"target", a.TeamName,
				"chain_len", len(chain),
				"max", maxPeerDepth,
			)
		}
		return "", fmt.Errorf("request_team_help: delegation depth limit reached (%d >= %d) — escalate to L1 instead of chaining further",
			len(chain), maxPeerDepth)
	}

	// 4. Build delegation context: propagate chain (append self), bypass confirm.
	//    Done before locateOrSpawn so the spawned agent inherits the chain.
	newChain := make([]string, 0, len(chain)+1)
	newChain = append(newChain, chain...)
	newChain = append(newChain, t.selfName)
	delCtx := ContextWithDelegationChain(ctx, newChain)
	delCtx = iface.ContextWithBypassConfirm(delCtx)

	timeout := t.timeout
	if timeout <= 0 {
		timeout = DelegateDefaultTimeout
	}
	if timeout > DelegateMaxTimeout {
		timeout = DelegateMaxTimeout
	}
	delCtx, cancel := context.WithTimeout(delCtx, timeout)
	defer cancel()

	// 5. Locate or spawn target agent.
	if t.locateOrSpawn == nil {
		return "", fmt.Errorf("request_team_help: no locate/spawn function configured")
	}
	target, spawned, err := t.locateOrSpawn(delCtx, a.TeamName)
	if err != nil {
		if t.logger != nil {
			t.logger.WarnContext(ctx, logger.CatTool, "request_team_help: locate/spawn failed",
				"target", a.TeamName,
				"err", err.Error(),
				"duration_ms", time.Since(start).Milliseconds(),
			)
		}
		return "", fmt.Errorf("request_team_help: cannot reach team %q: %w", a.TeamName, err)
	}

	// 6. Propagate model override to target (same as DelegateTool).
	if params := iface.ModelOverrideFromContext(ctx); params != nil {
		if mo, ok := target.(iface.ModelOverridable); ok {
			mo.SetModelOverride(params)
		}
	}

	// 7. Construct the task prompt: context + task.
	prompt := a.Task
	if a.Context != "" {
		prompt = fmt.Sprintf("Context: %s\n\nTask: %s", a.Context, a.Task)
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "request_team_help: delegating",
			"self", t.selfName,
			"target", a.TeamName,
			"spawned", spawned,
			"chain", strings.Join(newChain, " → "),
			"task_len", len(a.Task),
			"timeout_sec", timeout.Seconds(),
		)
	}

	// 8. Call target agent's streaming interface.
	evCh, err := target.AskStream(delCtx, prompt)
	if err != nil {
		if delCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
			t.maybeReap(target, spawned)
			return "", fmt.Errorf("request_team_help: delegation to %s timed out after %s", a.TeamName, timeout)
		}
		t.maybeReap(target, spawned)
		return "", fmt.Errorf("request_team_help: AskStream failed: %w", err)
	}

	// 9. Consume events (with parent event relay, same pattern as DelegateTool).
	parentEventCh, _ := ToolEventChannelFromCtx(ctx)
	confirmFwd, hasConfirmFwd := ConfirmForwarderFromCtx(ctx)

	var content string
	var finalErr error
	var eventCount int
	for ev := range evCh {
		if ev == nil {
			continue
		}
		eventCount++

		// Relay event to parent event channel (for confirm/error bubbling)
		if parentEventCh != nil {
			select {
			case parentEventCh <- ev:
			case <-delCtx.Done():
			}
		}

		ec, ok := ev.(iface.EventConsumer)
		if !ok {
			continue
		}

		// Route confirmation requests to parent agent
		if callID, has := ec.ConfirmRequest(); has && hasConfirmFwd {
			go func() {
				defer func() {
					if r := recover(); r != nil {
						if t.logger != nil {
							t.logger.ErrorContext(ctx, logger.CatTool, "request_team_help confirmFwd goroutine panic recovered",
								"target", a.TeamName,
								"call_id", callID,
								"panic", fmt.Sprintf("%v", r),
							)
						}
					}
				}()
				confirmFwd(delCtx, callID, target)
			}()
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

	// 10. Notify DoneNotifier (for externally-managed reaping).
	if dn, ok := target.(iface.DoneNotifier); ok {
		dn.OnDelegationDone()
	}

	// 11. Reap spawned instances (if a reap fn is set and we spawned it).
	t.maybeReap(target, spawned)

	if finalErr != nil {
		if t.logger != nil {
			t.logger.WarnContext(ctx, logger.CatTool, "request_team_help: finished with error",
				"target", a.TeamName,
				"events_processed", eventCount,
				"duration_ms", time.Since(start).Milliseconds(),
				"err", finalErr.Error(),
			)
		}
		return "", finalErr
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "request_team_help: completed",
			"target", a.TeamName,
			"content_len", len(content),
			"events_processed", eventCount,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}

	return content, nil
}

// maybeReap calls the reap function for spawned instances only.
func (t *RequestTeamHelpTool) maybeReap(target iface.Locatable, spawned bool) {
	if !spawned || t.reap == nil {
		return
	}
	t.reap(target)
}
