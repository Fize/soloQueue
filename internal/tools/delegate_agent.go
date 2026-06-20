package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// delegateAgentArgs holds the parameters for the delegate_agent tool.
type delegateAgentArgs struct {
	Name         string `json:"name"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	SkillID      string `json:"skill_id,omitempty"`
	Task         string `json:"task"`
	WorkDir      string `json:"work_dir"`
	Async        bool   `json:"async"`
	ModelID      string `json:"model_id,omitempty"`
}

// DelegateAgentTool delegates a task to a dynamically spawned child agent with a specific name, system prompt, and task description.
// It implements both Tool and AsyncTool.
type DelegateAgentTool struct {
	logger                 *logger.Logger
	SpawnFn                func(ctx context.Context, name, systemPrompt, modelID, task, workDir string, baseAgentName string, skillDir string) (iface.Locatable, error)
	SkillInstructionsLook  func(skillID string) (instructions string, agentName string, skillDir string, ok bool)
}

// Compile-time interface checks.
var (
	_ Tool      = (*DelegateAgentTool)(nil)
	_ AsyncTool = (*DelegateAgentTool)(nil)
)

// NewDelegateAgentTool creates a new DelegateAgentTool.
func NewDelegateAgentTool(l *logger.Logger, spawnFn func(ctx context.Context, name, systemPrompt, modelID, task, workDir string, baseAgentName string, skillDir string) (iface.Locatable, error)) *DelegateAgentTool {
	return &DelegateAgentTool{
		logger:  l,
		SpawnFn: spawnFn,
	}
}

func (DelegateAgentTool) Name() string { return "delegate_agent" }

func (DelegateAgentTool) Description() string {
	return "Delegate a task to a dynamically created child agent with a specific name, system prompt, and task description. Supports both synchronous (blocking) and asynchronous (background) execution."
}

func (DelegateAgentTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {
				"type": "string",
				"description": "The name of the target agent (e.g. 'code-reviewer')."
			},
			"system_prompt": {
				"type": "string",
				"description": "The system prompt / instructions for the target agent. Can be omitted if skill_id is provided."
			},
			"skill_id": {
				"type": "string",
				"description": "Optional skill ID. If provided, the dynamic agent will be injected with the specific execution logic and steps described by this skill."
			},
			"task": {
				"type": "string",
				"description": "The task query or prompt to delegate to the target agent."
			},
			"work_dir": {
				"type": "string",
				"description": "The working directory for the agent. REQUIRED."
			},
			"async": {
				"type": "boolean",
				"description": "If true, runs asynchronously in the background. If false (default), runs synchronously and blocks until complete."
			},
			"model_id": {
				"type": "string",
				"description": "Optional model ID to override the default model."
			}
		},
		"required": ["name", "task", "work_dir"]
	}`)
}

// Execute is called for synchronous execution (fallback when async=false).
func (t *DelegateAgentTool) Execute(ctx context.Context, rawArgs string) (string, error) {
	var args delegateAgentArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if args.SystemPrompt == "" && args.SkillID == "" {
		return "", fmt.Errorf("either system_prompt or skill_id must be provided")
	}

	if t.SpawnFn == nil {
		return "", fmt.Errorf("spawn function not configured")
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "delegate_agent sync execution starting",
			"name", args.Name, "task_len", len(args.Task))
	}

	systemPrompt := args.SystemPrompt
	var baseAgentName string
	var skillDir string
	if args.SkillID != "" && t.SkillInstructionsLook != nil {
		if inst, agentName, sDir, ok := t.SkillInstructionsLook(args.SkillID); ok {
			baseAgentName = agentName
			skillDir = sDir
			if inst != "" {
				if systemPrompt != "" {
					systemPrompt = systemPrompt + "\n\n# Skill Execution Instructions\n" + inst
				} else {
					systemPrompt = "# Skill Execution Instructions\n" + inst
				}
			}
		}
	}

	// Spawn the agent
	target, err := t.SpawnFn(ctx, args.Name, systemPrompt, args.ModelID, args.Task, args.WorkDir, baseAgentName, skillDir)
	if err != nil {
		return "", fmt.Errorf("failed to spawn agent %q: %w", args.Name, err)
	}

	// Propagate task-level model override if present
	if params := iface.ModelOverrideFromContext(ctx); params != nil {
		if mo, ok := target.(iface.ModelOverridable); ok {
			mo.SetModelOverride(params)
		}
	}

	// Call synchronous execution
	resultCh, err := target.AskStream(ctx, args.Task)
	if err != nil {
		return "", err
	}

	// Accumulate events
	var content string
	var finalErr error
	for event := range resultCh {
		if event == nil {
			continue
		}
		if ec, ok := event.(iface.EventConsumer); ok {
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
	}

	// Cleanup if target implements DoneNotifier
	if dn, ok := target.(iface.DoneNotifier); ok {
		dn.OnDelegationDone()
	}

	if finalErr != nil {
		return "", finalErr
	}

	return content, nil
}

// ExecuteAsync implements the AsyncTool interface for asynchronous execution.
func (t *DelegateAgentTool) ExecuteAsync(ctx context.Context, rawArgs string) (*AsyncAction, error) {
	var args delegateAgentArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	// If the LLM requested synchronous execution, return nil to fallback
	if !args.Async {
		return nil, nil
	}

	if args.SystemPrompt == "" && args.SkillID == "" {
		return nil, fmt.Errorf("either system_prompt or skill_id must be provided")
	}

	if t.SpawnFn == nil {
		return nil, fmt.Errorf("spawn function not configured")
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "delegate_agent async execution starting",
			"name", args.Name, "task_len", len(args.Task))
	}

	systemPrompt := args.SystemPrompt
	var baseAgentName string
	var skillDir string
	if args.SkillID != "" && t.SkillInstructionsLook != nil {
		if inst, agentName, sDir, ok := t.SkillInstructionsLook(args.SkillID); ok {
			baseAgentName = agentName
			skillDir = sDir
			if inst != "" {
				if systemPrompt != "" {
					systemPrompt = systemPrompt + "\n\n# Skill Execution Instructions\n" + inst
				} else {
					systemPrompt = "# Skill Execution Instructions\n" + inst
				}
			}
		}
	}

	// Spawn the agent
	target, err := t.SpawnFn(ctx, args.Name, systemPrompt, args.ModelID, args.Task, args.WorkDir, baseAgentName, skillDir)
	if err != nil {
		return nil, fmt.Errorf("failed to spawn agent %q: %w", args.Name, err)
	}

	// Propagate task-level model override if present
	if params := iface.ModelOverrideFromContext(ctx); params != nil {
		if mo, ok := target.(iface.ModelOverridable); ok {
			mo.SetModelOverride(params)
		}
	}

	// Return AsyncAction
	return &AsyncAction{
		Target:  target,
		Prompt:  args.Task,
		Timeout: 25 * time.Minute,
	}, nil
}
