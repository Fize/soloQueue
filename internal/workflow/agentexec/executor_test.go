package agentexec

import (
	"context"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/workflow"
)

func TestHandoffTool(t *testing.T) {
	tool := newHandoffTool()
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

func TestBuildWorkflowSystemPrompt(t *testing.T) {
	req := workflow.NodeRunRequest{
		Workflow: &workflow.ParsedWorkflow{
			Name: "test_wf",
		},
		RunID: "run-123",
		Node: &workflow.NodeDef{
			ID:     "node_a",
			Prompt: "Process data",
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
}
