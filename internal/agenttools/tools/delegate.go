package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
	Task         string `json:"task"`
	Context      string `json:"context,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	SkillID      string `json:"skill_id,omitempty"`
	WorkDir      string `json:"work_dir,omitempty"`
	Async        bool   `json:"async,omitempty"`
	ModelID      string `json:"model_id,omitempty"`
}

// LocateOrSpawnResolver resolves or spawns a target agent.
type LocateOrSpawnResolver func(ctx context.Context, targetName, systemPrompt, modelID, task, workDir, skillID string) (loc iface.Locatable, spawned bool, err error)

// DelegateToolOption configures a DelegateTool at construction time.
type DelegateToolOption func(*DelegateTool)

// WithAlwaysAsyncDelegation makes every delegation asynchronous, regardless
// of the model-provided async argument. L1 uses this to preserve message
// parallelism while L2/L3 retain the default synchronous behavior.
func WithAlwaysAsyncDelegation() DelegateToolOption {
	return func(dt *DelegateTool) {
		dt.alwaysAsync = true
	}
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
	Reap                  func(loc iface.Locatable)
	SkillInstructionsLook func(skillID string) (instructions string, agentName string, skillDir string, ok bool)
	alwaysAsync           bool
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
		"Supports both synchronous (blocking) and asynchronous (background) execution."
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
    "async": {
      "type": "boolean",
      "description": "If true, runs asynchronously in the background. Default is false (synchronous blocking)."
    },
    "model_id": {
      "type": "string",
      "description": "Optional model ID override for the target agent."
    }
  },
  "required": ["target", "task"]
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

func (dt *DelegateTool) Execute(ctx context.Context, args string) (string, error) {
	start := time.Now()

	var dArgs delegateArgs
	if err := json.Unmarshal([]byte(args), &dArgs); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if dArgs.Target == "" {
		return "", fmt.Errorf("delegate: target is required")
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

	// 2. Resolve work directory
	workDir, err := dt.resolveWorkDir(ctx, dArgs.WorkDir)
	if err != nil {
		return "", err
	}

	// 3. Locate or spawn target agent
	var targetAgent iface.Locatable
	var isSpawned bool

	if dt.LocateOrSpawn != nil {
		targetAgent, isSpawned, err = dt.LocateOrSpawn(ctx, dArgs.Target, dArgs.SystemPrompt, dArgs.ModelID, dArgs.Task, workDir, dArgs.SkillID)
		if err != nil {
			return "", fmt.Errorf("failed to reach agent %q: %w", dArgs.Target, err)
		}
	} else if dt.Locator != nil {
		var ok bool
		targetAgent, ok = dt.Locator.Locate(dArgs.Target)
		if !ok {
			return "", fmt.Errorf("agent %q not found", dArgs.Target)
		}
	} else {
		return "", fmt.Errorf("delegate tool: no locator or resolver configured")
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
			return "", fmt.Errorf("delegation to %s timed out after %s", dArgs.Target, timeout)
		}
		return "", fmt.Errorf("delegation to %s failed: %w", dArgs.Target, err)
	}

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

	if dn, ok := targetAgent.(iface.DoneNotifier); ok {
		dn.OnDelegationDone()
	}
	dt.maybeReap(targetAgent, isSpawned)

	if finalErr != nil {
		return "", finalErr
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

	return content, nil
}

func (dt *DelegateTool) maybeReap(target iface.Locatable, spawned bool) {
	if !spawned || dt.Reap == nil {
		return
	}
	dt.Reap(target)
}

func (dt *DelegateTool) ExecuteAsync(ctx context.Context, args string) (*AsyncAction, error) {
	var dArgs delegateArgs
	if err := json.Unmarshal([]byte(args), &dArgs); err != nil {
		return nil, fmt.Errorf("invalid args: %w", err)
	}

	if !dArgs.Async && !dt.alwaysAsync {
		return nil, nil
	}

	if dArgs.Target == "" {
		return nil, fmt.Errorf("delegate async: target is required")
	}
	if dArgs.Task == "" {
		return nil, fmt.Errorf("delegate async: task is required")
	}
	delCtx, err := dt.prepareDelegationContext(ctx, dArgs.Target)
	if err != nil {
		return nil, err
	}

	workDir, err := dt.resolveWorkDir(ctx, dArgs.WorkDir)
	if err != nil {
		return nil, err
	}

	var target iface.Locatable
	if dt.LocateOrSpawn != nil {
		var err error
		target, _, err = dt.LocateOrSpawn(ctx, dArgs.Target, dArgs.SystemPrompt, dArgs.ModelID, dArgs.Task, workDir, dArgs.SkillID)
		if err != nil {
			return nil, fmt.Errorf("failed to reach agent %q: %w", dArgs.Target, err)
		}
	} else if dt.Locator != nil {
		var ok bool
		target, ok = dt.Locator.Locate(dArgs.Target)
		if !ok {
			return nil, fmt.Errorf("agent %q not found", dArgs.Target)
		}
	} else {
		return nil, fmt.Errorf("delegate tool: no locator or resolver configured")
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
		Target:  target,
		Prompt:  prompt,
		Timeout: timeout,
		Context: delCtx,
	}, nil
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
