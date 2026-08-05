package workflow

import (
	"testing"
	"time"
)

func TestBuiltinEngineeringQualityLoopParses(t *testing.T) {
	spec, ok := BuiltinWorkflowByName("engineering-quality-loop")
	if !ok {
		t.Fatal("built-in workflow not found")
	}
	wf, err := ParseWorkflow([]byte(spec.YAML))
	if err != nil {
		t.Fatal(err)
	}
	if len(wf.Nodes) != 10 || wf.Entry[0] != "analyze" {
		t.Fatalf("unexpected workflow: nodes=%d entry=%v", len(wf.Nodes), wf.Entry)
	}
	if got := wf.Defaults.WorkflowTimeout.Duration(); got != DefaultEngineLimits().MaxWorkflowTimeout || got != time.Hour {
		t.Fatalf("workflow timeout = %s, want engine maximum %s", got, DefaultEngineLimits().MaxWorkflowTimeout)
	}
}

func TestWorkflowTaskRequiresGoalAndAcceptance(t *testing.T) {
	if err := (WorkflowTask{}).Validate(); err == nil {
		t.Fatal("empty task should be rejected")
	}
	if err := (WorkflowTask{Goal: "goal"}).Validate(); err == nil {
		t.Fatal("task without acceptance criteria should be rejected")
	}
	if err := (WorkflowTask{Goal: "goal", AcceptanceCriteria: []string{"done"}}).Validate(); err != nil {
		t.Fatal(err)
	}
}
