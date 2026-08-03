// Package agentexec provides the real NodeExecutor implementation that creates
// temporary L1 agents for workflow node execution. It wires up the workflow_handoff
// tool, consumes child agent events, relays confirmations, and handles cleanup.
package agentexec

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
	"github.com/xiaobaitu/soloqueue/internal/workflow"
)

// handoffTool implements tools.Tool + tools.TurnTerminator.
// The executor creates one per NodeRun; after the agent calls workflow_handoff,
// the tool stores the outcome/content and signals the turn to end.
type handoffTool struct {
	result *workflow.HandoffData // written by Execute, read by executor
}

// handoffArgs is the JSON schema for workflow_handoff parameters.
type handoffArgs struct {
	Outcome string `json:"outcome"`
	Content string `json:"content"`
}

func newHandoffTool() *handoffTool {
	return &handoffTool{result: &workflow.HandoffData{}}
}

func (t *handoffTool) Name() string { return "workflow_handoff" }

func (t *handoffTool) Description() string {
	return "Complete the current workflow node and hand off to the next node. " +
		"Must be called exactly once per node execution. " +
		"outcome: the result label matching one of the node's defined outputs. " +
		"content: a summary of what was done."
}

func (t *handoffTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"outcome": {"type": "string", "description": "The outcome label matching the node's outputs"},
			"content": {"type": "string", "description": "A summary of what was accomplished"}
		},
		"required": ["outcome", "content"]
	}`)
}

func (t *handoffTool) Execute(ctx context.Context, args string) (string, error) {
	var ha handoffArgs
	if err := json.Unmarshal([]byte(args), &ha); err != nil {
		return "", fmt.Errorf("workflow_handoff: invalid arguments: %w", err)
	}

	t.result.Outcome = ha.Outcome
	t.result.Content = ha.Content

	return fmt.Sprintf("Handoff accepted: outcome=%s", ha.Outcome), nil
}

// TerminatesTurn always returns true — a successful handoff ends the agent turn.
func (t *handoffTool) TerminatesTurn(result string, err error) bool {
	return true
}

// ─── Executor ──────────────────────────────────────────────────────────────

// Executor implements workflow.NodeExecutor using temporary agents.
type Executor struct {
	factory  agent.AgentFactory
	registry *agent.Registry
	log      *logger.Logger
}

type templateResolver interface {
	ResolveTemplate(context.Context, string) (agent.AgentTemplate, bool)
}

// NewExecutor creates a workflow agent executor.
func NewExecutor(factory agent.AgentFactory, registry *agent.Registry, log *logger.Logger) *Executor {
	return &Executor{factory: factory, registry: registry, log: log}
}

// Execute implements workflow.NodeExecutor.
//
// It creates a temporary agent with the workflow node's prompt, injects
// workflow_handoff as the only extra tool, waits for the agent to complete
// (which terminates when workflow_handoff is called), and returns the
// handoff result.
//
// The temporary agent is always stopped and unregistered, regardless
// of success, failure, or cancellation.
func (e *Executor) Execute(ctx context.Context, req workflow.NodeRunRequest) (workflow.NodeRunResult, error) {
	resolver, ok := e.factory.(templateResolver)
	if !ok {
		return workflow.NodeRunResult{}, fmt.Errorf("agentexec: factory cannot resolve agent templates")
	}
	tmpl, ok := resolver.ResolveTemplate(ctx, req.AgentRef.Template)
	if !ok {
		return workflow.NodeRunResult{}, fmt.Errorf(
			"agentexec: workflow agent template %q does not exist",
			req.AgentRef.Template,
		)
	}
	tmpl.ID = req.Node.ID + "_" + req.NodeRun.ID
	tmpl.Name = req.Node.ID
	if req.AgentRef.Model != "" {
		tmpl.ModelID = req.AgentRef.Model
	}

	// Create the handoff tool (one per NodeRun)
	handoff := newHandoffTool()

	createOpts := agent.CreateOptions{
		ExtraSystemPrompt: buildWorkflowSystemPrompt(req) + req.Node.Prompt + "\n</node_instruction>",
		ExtraTools:        []tools.Tool{handoff},
	}

	// Create temp agent
	child, cw, err := e.factory.CreateWithOptions(ctx, tmpl, req.WorkDir, createOpts)
	if err != nil {
		return workflow.NodeRunResult{}, fmt.Errorf("agentexec: create agent: %w", err)
	}
	defer func() {
		_ = child.Stop(5 * time.Second)
		e.registry.Unregister(child.InstanceID)
	}()

	// Push node instruction as user message
	cw.Push("user", req.Node.Prompt)

	// AskStream: wait for agent to complete
	evCh, err := child.AskStream(ctx, req.Node.Prompt)
	if err != nil {
		return workflow.NodeRunResult{}, fmt.Errorf("agentexec: ask: %w", err)
	}

	// Consume events (we don't need to do anything with them;
	// confirmation relay is handled by the session-level event channel)
	for range evCh {
		// Just drain — the handoff result is stored in handoff.result
		if ctx.Err() != nil {
			return workflow.NodeRunResult{}, ctx.Err()
		}
	}

	// Check if handoff occurred
	if handoff.result.Outcome == "" {
		return workflow.NodeRunResult{}, fmt.Errorf("agentexec: agent completed without calling workflow_handoff")
	}

	return workflow.NodeRunResult{
		Handoff: handoff.result,
	}, nil
}

// buildWorkflowSystemPrompt builds the system prompt with workflow context
// and upstream inputs, per Section 4.2 of the design doc.
func buildWorkflowSystemPrompt(req workflow.NodeRunRequest) string {
	prompt := fmt.Sprintf("<workflow_context>\nworkflow: %s\nrun_id: %s\nnode: %s\nattempt: %d\n</workflow_context>",
		req.Workflow.Name, req.RunID, req.Node.ID, req.NodeRun.Attempt)

	if req.WorkflowInput != "" {
		prompt += fmt.Sprintf("\n\n<workflow_input>\n%s\n</workflow_input>", req.WorkflowInput)
	}

	if len(req.NodeRun.Inputs) > 0 {
		prompt += "\n\n<upstream_inputs>"
		for _, inp := range req.NodeRun.Inputs {
			prompt += fmt.Sprintf("\n- from: %s\n  outcome: %s\n  content: %s", inp.FromNode, inp.Outcome, inp.Content)
		}
		prompt += "\n</upstream_inputs>"
	}

	prompt += "\n\n<node_instruction>\n"
	return prompt
}
