package agentexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	agenttools "github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/workflow"
)

func TestHandoffTool(t *testing.T) {
	tool := newHandoffTool([]string{"success"}, 1024)
	if tool.Name() != "workflow_handoff" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "workflow_handoff")
	}

	if !tool.TerminatesTurn("", nil) {
		t.Error("TerminatesTurn should return true")
	}

	args := `{"outcome": "success", "content": "Done working"}`
	res, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if tool.result.Outcome != "success" || tool.result.Content != "Done working" {
		t.Errorf("Unexpected result: %+v", tool.result)
	}

	if res != "Handoff accepted: outcome=success" {
		t.Errorf("Unexpected Execute output: %q", res)
	}

	// Invalid JSON
	_, err = tool.Execute(context.Background(), `invalid json`)
	if err == nil {
		t.Error("Execute with invalid JSON should fail")
	}
}

func TestHandoffToolParametersExposeSortedAllowedOutcomes(t *testing.T) {
	tool := newHandoffTool([]string{"planned", "blocked", "accepted"}, 1024)
	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(tool.Parameters(), &schema); err != nil {
		t.Fatal(err)
	}
	want := []string{"accepted", "blocked", "planned"}
	if got := schema.Properties["outcome"].Enum; !reflect.DeepEqual(got, want) {
		t.Fatalf("outcome enum = %v, want %v", got, want)
	}
}

func TestHandoffToolRejectsUnknownOutcomeWithoutTerminating(t *testing.T) {
	tool := newHandoffTool([]string{"planned"}, 1024)
	_, err := tool.Execute(context.Background(), `{"outcome":"plan_ready","content":"ready"}`)
	if err == nil || !strings.Contains(err.Error(), "HANDOFF_OUTCOME_UNKNOWN: plan_ready") {
		t.Fatalf("Execute error = %v, want HANDOFF_OUTCOME_UNKNOWN", err)
	}
	if tool.TerminatesTurn("", err) {
		t.Fatal("invalid handoff must not terminate the turn")
	}
	if tool.result.Outcome != "" {
		t.Fatalf("stored outcome = %q, want empty", tool.result.Outcome)
	}
}

func TestHandoffToolRejectsOversizedContentWithoutTerminating(t *testing.T) {
	tool := newHandoffTool([]string{"planned"}, 4)
	_, err := tool.Execute(context.Background(), `{"outcome":"planned","content":"12345"}`)
	if err == nil || !strings.Contains(err.Error(), "OUTPUT_TOO_LARGE") {
		t.Fatalf("Execute error = %v, want OUTPUT_TOO_LARGE", err)
	}
	if tool.TerminatesTurn("", err) {
		t.Fatal("oversized handoff must not terminate the turn")
	}
}

func TestHandoffToolRejectsDuplicateWithoutOverwritingResult(t *testing.T) {
	tool := newHandoffTool([]string{"planned"}, 1024)
	if _, err := tool.Execute(context.Background(), `{"outcome":"planned","content":"first"}`); err != nil {
		t.Fatal(err)
	}
	_, err := tool.Execute(context.Background(), `{"outcome":"planned","content":"second"}`)
	if err == nil || !strings.Contains(err.Error(), "HANDOFF_DUPLICATE") {
		t.Fatalf("second Execute error = %v, want HANDOFF_DUPLICATE", err)
	}
	if tool.result.Content != "first" {
		t.Fatalf("stored content = %q, want first", tool.result.Content)
	}
}

func TestBuildWorkflowSystemPrompt(t *testing.T) {
	req := workflow.NodeRunRequest{
		Workflow: &workflow.ParsedWorkflow{
			Name: "test_wf",
		},
		RunID: "run-123",
		Node: &workflow.NodeDef{
			ID:      "node_a",
			Prompt:  "Process data",
			Outputs: map[string]workflow.OutputDef{"planned": {}, "blocked": {}},
		},
		NodeRun: &workflow.NodeRun{
			Attempt: 1,
			Inputs: []workflow.NodeInput{
				{FromNode: "prev_node", Outcome: "success", Content: "Data ready"},
			},
		},
		WorkflowInput: "Initial user input",
	}

	prompt := buildWorkflowSystemPrompt(req)

	if prompt == "" {
		t.Fatal("buildWorkflowSystemPrompt returned empty string")
	}
	if !strings.Contains(prompt, "<allowed_outcomes>\n- blocked\n- planned\n</allowed_outcomes>") {
		t.Fatalf("prompt does not contain sorted allowed outcomes: %s", prompt)
	}
}

func TestExecutorRunsHandoffThroughRealAgent(t *testing.T) {
	fake := &agenttest.FakeLLM{ToolCallsByTurn: [][]llm.ToolCall{{{
		ID:   "handoff-1",
		Type: "function",
		Function: llm.FunctionCall{
			Name:      "workflow_handoff",
			Arguments: `{"outcome":"planned","content":"ready"}`,
		},
	}}}}
	registry := agent.NewRegistry(nil)
	template := agent.AgentTemplate{ID: "worker", Name: "Worker", SystemPrompt: "Work carefully."}
	factory := agent.NewDefaultFactory(
		registry,
		fake,
		agenttools.Config{},
		nil,
		agent.WithTemplates([]agent.AgentTemplate{template}),
		agent.WithWorkDir(t.TempDir()),
	)
	executor := NewExecutor(factory, registry, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := executor.Execute(ctx, workflow.NodeRunRequest{
		RunID:          "run-1",
		Workflow:       &workflow.ParsedWorkflow{Name: "test"},
		Node:           &workflow.NodeDef{ID: "plan", Prompt: "Plan", Outputs: map[string]workflow.OutputDef{"planned": {}}},
		AgentRef:       workflow.AgentRef{Template: "worker"},
		NodeRun:        &workflow.NodeRun{ID: "plan-1", NodeID: "plan", Attempt: 1},
		WorkDir:        t.TempDir(),
		MaxOutputBytes: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Handoff == nil || result.Handoff.Outcome != "planned" {
		t.Fatalf("handoff = %+v, want planned", result.Handoff)
	}
	if got := len(registry.List()); got != 0 {
		t.Fatalf("registered agents after Execute = %d, want 0", got)
	}
}

func TestExecutorPreservesChildStreamError(t *testing.T) {
	fake := &agenttest.FakeLLM{Err: errors.New("llm unavailable")}
	registry := agent.NewRegistry(nil)
	template := agent.AgentTemplate{ID: "worker", Name: "Worker", SystemPrompt: "Work carefully."}
	factory := agent.NewDefaultFactory(
		registry,
		fake,
		agenttools.Config{},
		nil,
		agent.WithTemplates([]agent.AgentTemplate{template}),
		agent.WithWorkDir(t.TempDir()),
	)
	executor := NewExecutor(factory, registry, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := executor.Execute(ctx, workflow.NodeRunRequest{
		RunID:          "run-error",
		Workflow:       &workflow.ParsedWorkflow{Name: "test"},
		Node:           &workflow.NodeDef{ID: "plan", Prompt: "Plan", Outputs: map[string]workflow.OutputDef{"planned": {}}},
		AgentRef:       workflow.AgentRef{Template: "worker"},
		NodeRun:        &workflow.NodeRun{ID: "plan-error", NodeID: "plan", Attempt: 1},
		WorkDir:        t.TempDir(),
		MaxOutputBytes: 1024,
	})
	if err == nil || !strings.Contains(err.Error(), "llm unavailable") {
		t.Fatalf("Execute error = %v, want child stream error", err)
	}
}

func TestExecutorLetsAgentCorrectUnknownOutcome(t *testing.T) {
	fake := &agenttest.FakeLLM{ToolCallsByTurn: [][]llm.ToolCall{
		{{ID: "handoff-invalid", Type: "function", Function: llm.FunctionCall{Name: "workflow_handoff", Arguments: `{"outcome":"plan_ready","content":"ready"}`}}},
		{{ID: "handoff-valid", Type: "function", Function: llm.FunctionCall{Name: "workflow_handoff", Arguments: `{"outcome":"planned","content":"ready"}`}}},
	}}
	registry := agent.NewRegistry(nil)
	template := agent.AgentTemplate{ID: "worker", Name: "Worker", SystemPrompt: "Work carefully."}
	factory := agent.NewDefaultFactory(
		registry,
		fake,
		agenttools.Config{},
		nil,
		agent.WithTemplates([]agent.AgentTemplate{template}),
		agent.WithWorkDir(t.TempDir()),
	)
	executor := NewExecutor(factory, registry, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := executor.Execute(ctx, workflow.NodeRunRequest{
		RunID:          "run-correction",
		Workflow:       &workflow.ParsedWorkflow{Name: "test"},
		Node:           &workflow.NodeDef{ID: "plan", Prompt: "Plan", Outputs: map[string]workflow.OutputDef{"planned": {}}},
		AgentRef:       workflow.AgentRef{Template: "worker"},
		NodeRun:        &workflow.NodeRun{ID: "plan-correction", NodeID: "plan", Attempt: 1},
		WorkDir:        t.TempDir(),
		MaxOutputBytes: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Handoff == nil || result.Handoff.Outcome != "planned" {
		t.Fatalf("handoff = %+v, want corrected planned outcome", result.Handoff)
	}
}

func TestBuiltinQualityLoopRunsThroughRealAgentExecutor(t *testing.T) {
	outcomes := []string{"planned", "implemented", "complete", "approved", "passed", "accepted"}
	turns := make([][]llm.ToolCall, 0, len(outcomes))
	for i, outcome := range outcomes {
		turns = append(turns, []llm.ToolCall{{
			ID:   fmt.Sprintf("handoff-%d", i),
			Type: "function",
			Function: llm.FunctionCall{
				Name:      "workflow_handoff",
				Arguments: fmt.Sprintf(`{"outcome":%q,"content":"done"}`, outcome),
			},
		}})
	}
	fake := &agenttest.FakeLLM{ToolCallsByTurn: turns}
	registry := agent.NewRegistry(nil)
	templates := []agent.AgentTemplate{
		{ID: "andrej karpathy", Name: "Architect", SystemPrompt: "Plan and review."},
		{ID: "editor", Name: "Editor", SystemPrompt: "Implement changes."},
		{ID: "tester", Name: "Tester", SystemPrompt: "Test changes."},
	}
	factory := agent.NewDefaultFactory(
		registry,
		fake,
		agenttools.Config{},
		nil,
		agent.WithTemplates(templates),
		agent.WithWorkDir(t.TempDir()),
	)
	executor := NewExecutor(factory, registry, nil)
	wf, err := workflow.ParseWorkflow([]byte(workflow.EngineeringQualityLoopYAML))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	run, err := workflow.NewEngine(executor, workflow.DefaultEngineLimits()).Run(ctx, wf, "test", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != workflow.RunCompleted {
		t.Fatalf("run status = %s, want completed", run.Status)
	}
	if len(run.NodeRuns) != len(outcomes) {
		t.Fatalf("node runs = %d, want %d", len(run.NodeRuns), len(outcomes))
	}
	if got := len(registry.List()); got != 0 {
		t.Fatalf("registered agents after workflow = %d, want 0", got)
	}
}
