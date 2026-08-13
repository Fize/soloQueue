// Package agentexec provides the real NodeExecutor implementation that creates
// temporary L1 agents for workflow node execution. It wires up the workflow_handoff
// tool, consumes child agent events, relays confirmations, and handles cleanup.
package agentexec

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetry"
	"github.com/xiaobaitu/soloqueue/internal/workflow"
)

// handoffTool implements tools.Tool + tools.TurnTerminator.
// The executor creates one per NodeRun; after the agent calls workflow_handoff,
// the tool stores the outcome/content and signals the turn to end.
type handoffTool struct {
	mu              sync.Mutex
	result          *workflow.HandoffData // written by Execute, read by executor
	allowedOutcomes []string
	maxOutputBytes  int
	completed       bool
}

// handoffArgs is the JSON schema for workflow_handoff parameters.
type handoffArgs struct {
	Outcome string `json:"outcome"`
	Content string `json:"content"`
}

func newHandoffTool(allowedOutcomes []string, maxOutputBytes int) *handoffTool {
	allowed := append([]string(nil), allowedOutcomes...)
	sort.Strings(allowed)
	return &handoffTool{
		result:          &workflow.HandoffData{},
		allowedOutcomes: allowed,
		maxOutputBytes:  maxOutputBytes,
	}
}

func nodeAllowedOutcomes(node *workflow.NodeDef) []string {
	if node == nil {
		return nil
	}
	outcomes := make([]string, 0, len(node.Outputs))
	for outcome := range node.Outputs {
		outcomes = append(outcomes, outcome)
	}
	sort.Strings(outcomes)
	return outcomes
}

func (t *handoffTool) Name() string { return "workflow_handoff" }

func (t *handoffTool) Description() string {
	return "Complete the current workflow node and hand off to the next node. " +
		"Must be called exactly once per node execution. " +
		"outcome: the result label matching one of the node's defined outputs. " +
		"content: a summary of what was done."
}

func (t *handoffTool) Parameters() json.RawMessage {
	type property struct {
		Type        string   `json:"type"`
		Description string   `json:"description"`
		Enum        []string `json:"enum,omitempty"`
	}
	schema := struct {
		Type       string              `json:"type"`
		Properties map[string]property `json:"properties"`
		Required   []string            `json:"required"`
	}{
		Type: "object",
		Properties: map[string]property{
			"outcome": {Type: "string", Description: "The outcome label matching the node's outputs", Enum: t.allowedOutcomes},
			"content": {Type: "string", Description: "A summary of what was accomplished"},
		},
		Required: []string{"outcome", "content"},
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("workflow_handoff: marshal schema: %v", err))
	}
	return raw
}

func (t *handoffTool) Execute(ctx context.Context, args string) (string, error) {
	var ha handoffArgs
	if err := json.Unmarshal([]byte(args), &ha); err != nil {
		return "", fmt.Errorf("workflow_handoff: invalid arguments: %w", err)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.completed {
		return "", fmt.Errorf("HANDOFF_DUPLICATE")
	}
	if !slices.Contains(t.allowedOutcomes, ha.Outcome) {
		return "", fmt.Errorf("HANDOFF_OUTCOME_UNKNOWN: %s", ha.Outcome)
	}
	if len(ha.Content) > t.maxOutputBytes {
		return "", fmt.Errorf("OUTPUT_TOO_LARGE: %d bytes exceeds %d", len(ha.Content), t.maxOutputBytes)
	}

	t.result.Outcome = ha.Outcome
	t.result.Content = ha.Content
	t.completed = true

	return fmt.Sprintf("Handoff accepted: outcome=%s", ha.Outcome), nil
}

// Failed terminal calls stay in the tool loop so the model can correct them.
func (t *handoffTool) TerminatesTurn(result string, err error) bool {
	return err == nil
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
	ctx = telemetry.WithTelemetryMetadata(ctx, telemetry.Metadata{
		RunID:   req.RunID,
		AgentID: req.NodeRun.ID,
		Origin:  telemetry.OriginWorkflow,
	})
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
	handoff := newHandoffTool(nodeAllowedOutcomes(req.Node), req.MaxOutputBytes)

	createOpts := agent.CreateOptions{
		ExtraSystemPrompt: buildWorkflowSystemPrompt(req) + req.Node.Prompt + "\n</node_instruction>",
		ExtraTools:        []tools.Tool{handoff},
		MemoryPolicy:      agent.MemoryDisabled,
	}
	if tmpl.IsLeader && tmpl.Group != "" {
		createOpts.MemoryPolicy = agent.MemoryL2Group
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

	// Drain the full stream so producer backpressure cannot hide a terminal error.
	var childErr error
	for event := range evCh {
		if streamError, ok := event.(agent.ErrorEvent); ok && childErr == nil {
			childErr = streamError.Err
		}
		if confirmation, ok := event.(agent.ToolNeedsConfirmEvent); ok && req.RecordConfirmation != nil {
			callID := confirmation.CallID
			req.RecordConfirmation(workflow.ConfirmationRequest{
				CallID:         callID,
				NodeRunID:      req.NodeRun.ID,
				ToolName:       confirmation.Name,
				PromptRedacted: confirmation.Prompt,
				Options:        append([]string(nil), confirmation.Options...),
				AllowInSession: confirmation.AllowInSession,
				Resolve: func(choice string) error {
					return child.Confirm(callID, choice)
				},
			})
		}
		// Just drain — the handoff result is stored in handoff.result
		if ctx.Err() != nil {
			return workflow.NodeRunResult{}, ctx.Err()
		}
	}
	if childErr != nil {
		return workflow.NodeRunResult{}, fmt.Errorf("agentexec: child stream: %w", childErr)
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

	if outcomes := nodeAllowedOutcomes(req.Node); len(outcomes) > 0 {
		prompt += "\n\n<allowed_outcomes>"
		for _, outcome := range outcomes {
			prompt += "\n- " + outcome
		}
		prompt += "\n</allowed_outcomes>"
	}

	prompt += "\n\n<node_instruction>\n"
	return prompt
}
