package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/dispatch"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	workdirutil "github.com/xiaobaitu/soloqueue/internal/infra/workdir"
)

// --- Delegate constants ---

const (
	// DelegateDefaultTimeout is the default delegation task timeout.
	DelegateDefaultTimeout = 25 * time.Minute

	// DelegateMaxTimeout is the maximum allowed delegation task timeout.
	DelegateMaxTimeout = 30 * time.Minute

	// maxPeerDepth limits how many L2→L2 hops a single task can make.
	maxPeerDepth = 2
)

// ─── Delegation chain context propagation ────────────────────────────────────

type delegationChainCtxKey struct{}

// ContextWithDelegationChain injects the L2 peer delegation chain into context.
func ContextWithDelegationChain(ctx context.Context, chain []string) context.Context {
	if len(chain) == 0 {
		return ctx
	}
	return context.WithValue(ctx, delegationChainCtxKey{}, chain)
}

// DelegationChainFromContext extracts the peer delegation chain from context.
func DelegationChainFromContext(ctx context.Context) []string {
	v, _ := ctx.Value(delegationChainCtxKey{}).([]string)
	return v
}

// --- DelegateWorkDirPolicy ---

// DelegateWorkDirPolicy controls whether a delegated agent must inherit the
// caller's project scope or may explicitly select another existing directory.
type DelegateWorkDirPolicy int

const (
	WorkDirInheritOnly DelegateWorkDirPolicy = iota
	WorkDirExplicitOrInherited
)

// --- delegateArgs ---

type delegateArgs struct {
	Target       string `json:"target"`
	TaskName     string `json:"task_name"`
	Task         string `json:"task"`
	Context      string `json:"context,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	SkillID      string `json:"skill_id,omitempty"`
	WorkDir      string `json:"work_dir,omitempty"`
	ModelID      string `json:"model_id,omitempty"`
}

// LocateOrSpawnResolver resolves or spawns a target agent.
type LocateOrSpawnResolver func(ctx context.Context, targetName, systemPrompt, modelID, task, workDir, skillID string) (loc iface.Locatable, spawned bool, err error)

// DelegateToolOption configures a DelegateTool at construction time.
type DelegateToolOption func(*DelegateTool)

// WithAlwaysAsyncDelegation makes every delegation asynchronous. L1 uses this
// framework policy to preserve message parallelism while L2/L3 retain the
// default synchronous behavior.
func WithAlwaysAsyncDelegation() DelegateToolOption {
	return func(dt *DelegateTool) {
		dt.alwaysAsync = true
	}
}

// WithPeerTarget identifies targets that represent lateral L2 help.
func WithPeerTarget(match func(string) bool) DelegateToolOption {
	return func(dt *DelegateTool) { dt.isPeerTarget = match }
}

// DelegateTool delegates tasks to other agents or team leaders.
// Statically named "delegate" to guarantee LLM Prompt Caching is never broken.
type DelegateTool struct {
	SelfName              string
	Timeout               time.Duration
	WorkDirPolicy         DelegateWorkDirPolicy
	logger                *logger.Logger
	Locator               iface.AgentLocator
	LocateOrSpawn         LocateOrSpawnResolver
	PeerLocateOrSpawn     LocateOrSpawnResolver
	Reap                  func(loc iface.Locatable)
	SkillInstructionsLook func(skillID string) (instructions string, agentName string, skillDir string, ok bool)
	alwaysAsync           bool
	isPeerTarget          func(string) bool
}

var (
	_ Tool      = (*DelegateTool)(nil)
	_ AsyncTool = (*DelegateTool)(nil)
)

func NewDelegateTool(selfName string, timeout time.Duration, resolver LocateOrSpawnResolver, locator iface.AgentLocator, l *logger.Logger, workDirPolicy DelegateWorkDirPolicy, opts ...DelegateToolOption) *DelegateTool {
	if l == nil {
		var err error
		l, err = logger.System("/tmp", logger.WithConsole(false), logger.WithFile(false))
		if err != nil {
			l = nil
		}
	}
	dt := &DelegateTool{
		SelfName:      selfName,
		Timeout:       timeout,
		LocateOrSpawn: resolver,
		Locator:       locator,
		WorkDirPolicy: workDirPolicy,
		logger:        l,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(dt)
		}
	}
	return dt
}

func (dt *DelegateTool) SetLogger(l *logger.Logger) {
	dt.logger = l
}

func (dt *DelegateTool) Name() string { return "delegate" }

func (dt *DelegateTool) Description() string {
	return "Delegate a task to another agent, team leader, or dynamic worker (e.g. 'dev', 'ops', 'qa', or custom name). " +
		"Provide a stable task name and the exact work content; scheduling is controlled by the framework."
}

func (dt *DelegateTool) Parameters() json.RawMessage {
	workDirProperty := ""
	if dt.WorkDirPolicy == WorkDirExplicitOrInherited {
		workDirProperty = `,
    "work_dir": {
      "type": "string",
      "description": "Optional working directory for the agent. Defaults to caller's working directory."
    }`
	}
	return json.RawMessage(fmt.Sprintf(`{
  "type": "object",
  "properties": {
    "target": {
      "type": "string",
      "description": "Name of the target agent or team leader (e.g. 'dev', 'ops', 'qa', 'code-reviewer')."
    },
	"task_name": {
	  "type": "string",
	  "description": "Stable, concise identity for this logical task. Reuse it when checking or repeating the same active work."
	},
    "task": {
      "type": "string",
      "description": "Task description or prompt to delegate."
    },
    "context": {
      "type": "string",
      "description": "Optional background context, previous attempt details, or constraints."
    },
    "system_prompt": {
      "type": "string",
      "description": "Optional system prompt / instructions if spawning a dynamic worker agent."
    },
    "skill_id": {
      "type": "string",
      "description": "Optional skill ID if spawning a dynamic worker agent based on a skill."
	}%s,
    "model_id": {
      "type": "string",
      "description": "Optional model ID override for the target agent."
    }
  },
  "required": ["target", "task_name", "task"]
}`, workDirProperty))
}

func (dt *DelegateTool) resolveWorkDir(ctx context.Context, requested string) (string, error) {
	inherited := iface.WorkDirFromContext(ctx)
	if dt.WorkDirPolicy == WorkDirInheritOnly {
		if inherited == "" {
			return "", fmt.Errorf("delegate: parent work directory is not configured")
		}
		return inherited, nil
	}
	if requested != "" {
		return workdirutil.NormalizeExistingDir(requested)
	}
	if inherited == "" {
		return "", fmt.Errorf("delegate: work directory is not configured")
	}
	return inherited, nil
}

func (dt *DelegateTool) prepareDelegationContext(ctx context.Context, target string) (context.Context, error) {
	chain := DelegationChainFromContext(ctx)
	for _, name := range chain {
		if strings.EqualFold(name, target) {
			if dt.logger != nil {
				dt.logger.WarnContext(ctx, logger.CatTool, "delegate: cycle detected",
					"self", dt.SelfName, "target", target, "chain", strings.Join(chain, " → "))
			}
			return nil, fmt.Errorf("delegate: delegation cycle detected (%s is already in chain: %s)",
				target, strings.Join(append(chain, dt.SelfName), " → "))
		}
	}
	if len(chain) >= maxPeerDepth {
		if dt.logger != nil {
			dt.logger.WarnContext(ctx, logger.CatTool, "delegate: depth limit exceeded",
				"self", dt.SelfName, "target", target, "chain_len", len(chain))
		}
		return nil, fmt.Errorf("delegate: delegation depth limit reached (%d >= %d) — escalate to L1 instead",
			len(chain), maxPeerDepth)
	}

	newChain := make([]string, 0, len(chain)+1)
	if dt.SelfName != "" {
		newChain = append(newChain, chain...)
		newChain = append(newChain, dt.SelfName)
	}
	return ContextWithDelegationChain(ctx, newChain), nil
}

func (dt *DelegateTool) Execute(ctx context.Context, args string) (result string, retErr error) {
	start := time.Now()

	var dArgs delegateArgs
	if err := json.Unmarshal([]byte(args), &dArgs); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if dArgs.Target == "" {
		return "", fmt.Errorf("delegate: target is required")
	}
	if strings.TrimSpace(dArgs.TaskName) == "" {
		if _, managed := dispatch.ScopeFromContext(ctx); managed {
			return "", fmt.Errorf("delegate: task_name is required")
		}
		dArgs.TaskName = dArgs.Task
	}
	if dArgs.Task == "" {
		return "", fmt.Errorf("delegate: task is required")
	}

	if dArgs.SkillID != "" && dt.SkillInstructionsLook != nil {
		if inst, _, _, ok := dt.SkillInstructionsLook(dArgs.SkillID); ok && inst != "" {
			if dArgs.SystemPrompt != "" {
				dArgs.SystemPrompt = dArgs.SystemPrompt + "\n\n# Skill Execution Instructions\n" + inst
			} else {
				dArgs.SystemPrompt = "# Skill Execution Instructions\n" + inst
			}
		}
	}

	// 1. Peer cycle detection, depth limits, and child chain construction.
	delCtx, err := dt.prepareDelegationContext(ctx, dArgs.Target)
	if err != nil {
		return "", err
	}
	dispatchID, reused, delCtx, err := dt.beginDispatch(delCtx, dArgs)
	if err != nil {
		return "", err
	}
	if reused {
		return fmt.Sprintf(`{"dispatch_id":%q,"status":"running","reused":true}`, dispatchID), nil
	}
	var finishOnce sync.Once
	var terminalErr error
	finish := func(status dispatch.Status, finishErr error) error {
		finishOnce.Do(func() {
			if scope, ok := dispatch.ScopeFromContext(delCtx); ok && dispatchID != "" {
				terminalErr = scope.Manager.Finish(dispatchID, status, finishErr)
			}
		})
		return terminalErr
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			panicErr := fmt.Errorf("delegate %s panicked: %v", dispatchID, recovered)
			result = ""
			retErr = errors.Join(panicErr, finish(dispatch.StatusFailed, panicErr))
		}
	}()

	// 2. Resolve work directory
	workDir, err := dt.resolveWorkDir(ctx, dArgs.WorkDir)
	if err != nil {
		return "", errors.Join(err, finish(dispatch.StatusFailed, err))
	}

	// 3. Locate or spawn target agent
	var targetAgent iface.Locatable
	var isSpawned bool

	if dt.LocateOrSpawn != nil {
		targetAgent, isSpawned, err = dt.LocateOrSpawn(delCtx, dArgs.Target, dArgs.SystemPrompt, dArgs.ModelID, dArgs.Task, workDir, dArgs.SkillID)
		if err != nil {
			wrapped := fmt.Errorf("failed to reach agent %q: %w", dArgs.Target, err)
			return "", errors.Join(wrapped, finish(dispatch.StatusFailed, wrapped))
		}
	} else if dt.Locator != nil {
		var ok bool
		targetAgent, ok = dt.Locator.Locate(dArgs.Target)
		if !ok {
			wrapped := fmt.Errorf("agent %q not found", dArgs.Target)
			return "", errors.Join(wrapped, finish(dispatch.StatusFailed, wrapped))
		}
	} else {
		wrapped := errors.New("delegate tool: no locator or resolver configured")
		return "", errors.Join(wrapped, finish(dispatch.StatusFailed, wrapped))
	}
	if err := dt.assignExecutorInstance(delCtx, dispatchID, targetAgent); err != nil {
		return "", errors.Join(err, finish(dispatch.StatusFailed, err))
	}

	// 4. Construct prompt (append context if present)
	prompt := dArgs.Task
	if dArgs.Context != "" {
		prompt = fmt.Sprintf("Context: %s\n\nTask: %s", dArgs.Context, dArgs.Task)
	}

	// 5. Apply delegation timeout to the validated child context.
	timeout := dt.Timeout
	if timeout <= 0 {
		timeout = DelegateDefaultTimeout
	}
	if timeout > DelegateMaxTimeout {
		timeout = DelegateMaxTimeout
	}
	delCtx, cancel := context.WithTimeout(delCtx, timeout)
	defer cancel()

	if params := iface.ModelOverrideFromContext(ctx); params != nil {
		if mo, ok := targetAgent.(iface.ModelOverridable); ok {
			mo.SetModelOverride(params)
		}
	}

	if dt.logger != nil {
		dt.logger.InfoContext(ctx, logger.CatTool, "delegate: calling AskStream on target",
			"self", dt.SelfName, "target", dArgs.Target, "timeout_sec", timeout.Seconds())
	}

	evCh, err := targetAgent.AskStream(delCtx, prompt)
	if err != nil {
		dt.maybeReap(targetAgent, isSpawned)
		if delCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
			wrapped := fmt.Errorf("delegation to %s timed out after %s", dArgs.Target, timeout)
			return "", errors.Join(wrapped, finish(dispatch.StatusFailed, wrapped))
		}
		wrapped := fmt.Errorf("delegation to %s failed: %w", dArgs.Target, err)
		return "", errors.Join(wrapped, finish(dispatch.StatusFailed, wrapped))
	}

	parentEventCh, _ := ToolEventChannelFromCtx(ctx)
	confirmFwd, hasConfirmFwd := ConfirmForwarderFromCtx(ctx)

	var content string
	var finalErr error
	var persistenceErr error
	var eventCount int

	for ev := range evCh {
		if ev == nil {
			continue
		}
		eventCount++
		if scope, ok := dispatch.ScopeFromContext(delCtx); ok && dispatchID != "" {
			persistenceErr = errors.Join(persistenceErr, scope.Manager.Append(dispatchID, fmt.Sprintf("agent_event:%T", ev), persistedAgentEvent(ev)))
		}

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

		if callID, has := ec.ConfirmRequest(); has && hasConfirmFwd {
			go func() {
				defer func() {
					if r := recover(); r != nil {
						if dt.logger != nil {
							dt.logger.ErrorContext(ctx, logger.CatTool, "delegate confirmFwd goroutine panic recovered",
								"target", dArgs.Target, "call_id", callID, "panic", fmt.Sprintf("%v", r))
						}
					}
				}()
				confirmFwd(delCtx, callID, targetAgent)
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
	if finalErr == nil && delCtx.Err() != nil {
		finalErr = delCtx.Err()
	}

	if dn, ok := targetAgent.(iface.DoneNotifier); ok {
		dn.OnDelegationDone()
	}
	dt.maybeReap(targetAgent, isSpawned)

	if finalErr != nil {
		return "", errors.Join(finalErr, persistenceErr, finish(dispatch.StatusFailed, errors.Join(finalErr, persistenceErr)))
	}
	if persistenceErr != nil {
		return "", errors.Join(persistenceErr, finish(dispatch.StatusFailed, persistenceErr))
	}

	if ec := targetAgent.ErrorCount(); ec > 0 {
		prefix := fmt.Sprintf("[WARNING: worker encountered %d error(s) during execution", ec)
		if le := targetAgent.LastError(); le != "" {
			prefix += fmt.Sprintf("; last error: %s", le)
		}
		prefix += "]\n"
		content = prefix + content
	}

	if dt.logger != nil {
		dt.logger.InfoContext(ctx, logger.CatTool, "delegate: completed successfully",
			"target", dArgs.Target, "content_len", len(content), "events_processed", eventCount, "duration_ms", time.Since(start).Milliseconds())
	}
	if err := finish(dispatch.StatusCompleted, nil); err != nil {
		return "", fmt.Errorf("dispatch %s terminal persistence: %w", dispatchID, err)
	}

	if dispatchID != "" {
		return fmt.Sprintf("dispatch_id: %s\n%s", dispatchID, content), nil
	}
	return content, nil
}

func (dt *DelegateTool) maybeReap(target iface.Locatable, spawned bool) {
	if !spawned || dt.Reap == nil {
		return
	}
	dt.Reap(target)
}

func (dt *DelegateTool) ExecuteAsync(ctx context.Context, args string) (action *AsyncAction, retErr error) {
	var dArgs delegateArgs
	if err := json.Unmarshal([]byte(args), &dArgs); err != nil {
		return nil, fmt.Errorf("invalid args: %w", err)
	}

	if !dt.alwaysAsync {
		return nil, nil
	}

	if dArgs.Target == "" {
		return nil, fmt.Errorf("delegate async: target is required")
	}
	if strings.TrimSpace(dArgs.TaskName) == "" {
		if _, managed := dispatch.ScopeFromContext(ctx); managed {
			return nil, fmt.Errorf("delegate async: task_name is required")
		}
		dArgs.TaskName = dArgs.Task
	}
	if dArgs.Task == "" {
		return nil, fmt.Errorf("delegate async: task is required")
	}
	delCtx, err := dt.prepareDelegationContext(ctx, dArgs.Target)
	if err != nil {
		return nil, err
	}
	dispatchID, reused, delCtx, err := dt.beginDispatch(delCtx, dArgs)
	if err != nil {
		return nil, err
	}
	if reused {
		return nil, nil
	}
	var finishOnce sync.Once
	var terminalErr error
	finish := func(finishErr error) error {
		finishOnce.Do(func() {
			terminalErr = dt.finishDispatch(delCtx, dispatchID, dispatch.StatusFailed, finishErr)
		})
		return terminalErr
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			panicErr := fmt.Errorf("delegate async setup %s panicked: %v", dispatchID, recovered)
			action = nil
			retErr = errors.Join(panicErr, finish(panicErr))
		}
	}()

	workDir, err := dt.resolveWorkDir(ctx, dArgs.WorkDir)
	if err != nil {
		return nil, errors.Join(err, finish(err))
	}

	var target iface.Locatable
	if dt.LocateOrSpawn != nil {
		var err error
		target, _, err = dt.LocateOrSpawn(delCtx, dArgs.Target, dArgs.SystemPrompt, dArgs.ModelID, dArgs.Task, workDir, dArgs.SkillID)
		if err != nil {
			wrapped := fmt.Errorf("failed to reach agent %q: %w", dArgs.Target, err)
			return nil, errors.Join(wrapped, finish(wrapped))
		}
	} else if dt.Locator != nil {
		var ok bool
		target, ok = dt.Locator.Locate(dArgs.Target)
		if !ok {
			wrapped := fmt.Errorf("agent %q not found", dArgs.Target)
			return nil, errors.Join(wrapped, finish(wrapped))
		}
	} else {
		wrapped := errors.New("delegate tool: no locator or resolver configured")
		return nil, errors.Join(wrapped, finish(wrapped))
	}
	if err := dt.assignExecutorInstance(delCtx, dispatchID, target); err != nil {
		return nil, errors.Join(err, finish(err))
	}

	if params := iface.ModelOverrideFromContext(ctx); params != nil {
		if mo, ok := target.(iface.ModelOverridable); ok {
			mo.SetModelOverride(params)
		}
	}

	timeout := dt.Timeout
	if timeout <= 0 {
		timeout = DelegateDefaultTimeout
	}
	if timeout > DelegateMaxTimeout {
		timeout = DelegateMaxTimeout
	}

	prompt := dArgs.Task
	if dArgs.Context != "" {
		prompt = fmt.Sprintf("Context: %s\n\nTask: %s", dArgs.Context, dArgs.Task)
	}

	return &AsyncAction{
		Target:     target,
		Prompt:     prompt,
		Timeout:    timeout,
		Context:    delCtx,
		DispatchID: dispatchID,
		OnEvent: func(ev iface.AgentEvent) error {
			if scope, ok := dispatch.ScopeFromContext(delCtx); ok {
				return scope.Manager.Append(dispatchID, fmt.Sprintf("agent_event:%T", ev), persistedAgentEvent(ev))
			}
			return nil
		},
		OnFinish: func(err error) error {
			status := dispatch.StatusCompleted
			if err != nil {
				status = dispatch.StatusFailed
			}
			return dt.finishDispatch(delCtx, dispatchID, status, err)
		},
	}, nil
}

func (dt *DelegateTool) beginDispatch(ctx context.Context, args delegateArgs) (string, bool, context.Context, error) {
	scope, ok := dispatch.ScopeFromContext(ctx)
	if !ok {
		return "", false, ctx, nil
	}
	kind := dispatch.KindDelegate
	if dt.isPeerTarget != nil && dt.isPeerTarget(args.Target) {
		kind = dispatch.KindPeerHelp
	}
	result, err := scope.Manager.Begin(dispatch.BeginInput{
		Kind: kind, TaskName: args.TaskName, Task: args.Task, Context: args.Context, Requester: dt.SelfName,
		Executor: args.Target, RootID: scope.RootID, ParentID: scope.ParentID,
	})
	if err != nil {
		return "", false, ctx, err
	}
	childScope := dispatch.Scope{Manager: scope.Manager, RootID: result.Record.RootID, ParentID: result.Record.ID}
	return result.Record.ID, result.Reused, dispatch.WithScope(ctx, childScope), nil
}

func (dt *DelegateTool) assignExecutorInstance(ctx context.Context, id string, target iface.Locatable) error {
	provider, ok := target.(interface{ InstanceID() string })
	if !ok || id == "" {
		return nil
	}
	if scope, present := dispatch.ScopeFromContext(ctx); present {
		return scope.Manager.AssignExecutorInstance(id, provider.InstanceID())
	}
	return nil
}

func (dt *DelegateTool) finishDispatch(ctx context.Context, id string, status dispatch.Status, err error) error {
	if scope, ok := dispatch.ScopeFromContext(ctx); ok && id != "" {
		return scope.Manager.Finish(id, status, err)
	}
	return nil
}

func persistedAgentEvent(ev iface.AgentEvent) any {
	payload := map[string]any{"event_type": fmt.Sprintf("%T", ev), "event": ev}
	if consumer, ok := ev.(iface.EventConsumer); ok {
		if delta, present := consumer.ContentDelta(); present {
			payload["content_delta"] = delta
		}
		if content, present := consumer.DoneContent(); present {
			payload["done_content"] = content
		}
		if err, present := consumer.Error(); present && err != nil {
			payload["error"] = err.Error()
		}
		if callID, present := consumer.ConfirmRequest(); present {
			payload["confirm_call_id"] = callID
		}
	}
	return payload
}

func (dt *DelegateTool) IsAsync() bool {
	return true
}

func (dt *DelegateTool) PreferredTimeout() time.Duration {
	if dt.Timeout > 0 {
		return dt.Timeout
	}
	return DelegateDefaultTimeout
}

// --- Context helpers for event relay & confirm routing ---

type toolEventChannelCtxKey struct{}

func WithToolEventChannel(ctx context.Context, ch chan<- iface.AgentEvent) context.Context {
	return context.WithValue(ctx, toolEventChannelCtxKey{}, ch)
}

func ToolEventChannelFromCtx(ctx context.Context) (chan<- iface.AgentEvent, bool) {
	ch, ok := ctx.Value(toolEventChannelCtxKey{}).(chan<- iface.AgentEvent)
	return ch, ok
}

type confirmForwarderCtxKey struct{}

func WithConfirmForwarder(ctx context.Context, f iface.ConfirmForwarder) context.Context {
	return context.WithValue(ctx, confirmForwarderCtxKey{}, f)
}

func ConfirmForwarderFromCtx(ctx context.Context) (iface.ConfirmForwarder, bool) {
	f, ok := ctx.Value(confirmForwarderCtxKey{}).(iface.ConfirmForwarder)
	return f, ok
}
