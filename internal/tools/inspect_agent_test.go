package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// ─── InspectAgentTool: basic interface ──────────────────────────────────

func TestInspectAgentTool_Interface(t *testing.T) {
	tool := NewInspectAgentTool(nil)
	if tool.Name() != "inspect_agent" {
		t.Errorf("Name() = %q, want inspect_agent", tool.Name())
	}
	if !strings.Contains(tool.Description(), "Query agent status") {
		t.Error("Description should contain 'Query agent status'")
	}
	if tool.Parameters() == nil {
		t.Error("Parameters() should not be nil")
	}
}

func TestInspectAgentTool_NilQueryFn(t *testing.T) {
	tool := NewInspectAgentTool(nil)
	result, err := tool.Execute(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result, "query function not configured") {
		t.Errorf("result = %q, want error about missing query function", result)
	}
}

func TestInspectAgentTool_InvalidArgs(t *testing.T) {
	queryFn := func(ctx context.Context, agentID, template string) (*InspectOutput, error) {
		return nil, errors.New("unreachable")
	}
	tool := NewInspectAgentTool(queryFn)
	result, err := tool.Execute(context.Background(), `not json`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result, "invalid args") {
		t.Errorf("result = %q, want error about invalid args", result)
	}
}

func TestInspectAgentTool_Execute_QueryError(t *testing.T) {
	queryFn := func(ctx context.Context, agentID, template string) (*InspectOutput, error) {
		return nil, errors.New("simulated failure")
	}
	tool := NewInspectAgentTool(queryFn)
	result, _ := tool.Execute(context.Background(), `{}`)
	if !strings.Contains(result, "simulated failure") {
		t.Errorf("result = %q, want to contain 'simulated failure'", result)
	}
}

func TestInspectAgentTool_Execute_ReturnsValidJSON(t *testing.T) {
	queryFn := func(ctx context.Context, agentID, template string) (*InspectOutput, error) {
		return &InspectOutput{
			QueryType: "all",
			Teams: []TeamStatus{
				{TemplateID: "dev", TemplateName: "Dev", Agents: []AgentStatus{
					{InstanceID: "a1", TemplateID: "dev", TemplateName: "Dev", State: "idle"},
				}},
			},
			Summary: StatusSummary{Total: 1, Idle: 1},
		}, nil
	}
	tool := NewInspectAgentTool(queryFn)
	result, err := tool.Execute(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var output map[string]interface{}
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		t.Fatalf("result is not valid JSON: %v\nresult: %s", err, result)
	}
}

// ─── Conversion helpers ─────────────────────────────────────────────────

func TestGroupByTemplate(t *testing.T) {
	statuses := []AgentStatus{
		{InstanceID: "a1", TemplateID: "dev", TemplateName: "Dev"},
		{InstanceID: "a2", TemplateID: "dev", TemplateName: "Dev"},
		{InstanceID: "a3", TemplateID: "ops", TemplateName: "Ops"},
	}
	teams := GroupByTemplate(statuses)
	if len(teams) != 2 {
		t.Fatalf("teams count = %d, want 2", len(teams))
	}
	for _, team := range teams {
		if team.TemplateID == "dev" && len(team.Agents) != 2 {
			t.Errorf("dev team agents = %d, want 2", len(team.Agents))
		}
	}
}

func TestSummaryFrom(t *testing.T) {
	statuses := []AgentStatus{
		{State: "processing"},
		{State: "processing"},
		{State: "idle"},
		{State: "stopped"},
	}
	s := SummaryFrom(statuses)
	if s.Total != 4 {
		t.Errorf("Total = %d, want 4", s.Total)
	}
	if s.Running != 2 {
		t.Errorf("Running = %d, want 2", s.Running)
	}
	if s.Idle != 1 {
		t.Errorf("Idle = %d, want 1", s.Idle)
	}
	if s.Stopped != 1 {
		t.Errorf("Stopped = %d, want 1", s.Stopped)
	}
}

func TestFilterByTemplate(t *testing.T) {
	statuses := []AgentStatus{
		{TemplateID: "dev-leader", TemplateName: "Development Leader"},
		{TemplateID: "ops", TemplateName: "Operations"},
	}
	filtered := FilterByTemplate(statuses, "dev")
	if len(filtered) != 1 {
		t.Fatalf("filtered count = %d, want 1", len(filtered))
	}
	if filtered[0].TemplateID != "dev-leader" {
		t.Errorf("TemplateID = %q, want dev-leader", filtered[0].TemplateID)
	}
}
