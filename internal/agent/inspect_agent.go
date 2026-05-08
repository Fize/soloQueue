package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ─── Registry-scoped query ──────────────────────────────────────────────

// RegistryInspectQuery returns a tools.InspectQueryFn that queries all agents
// registered in the given Registry. This is used for L1 (session agent).
func RegistryInspectQuery(reg *Registry) tools.InspectQueryFn {
	return func(ctx context.Context, agentID, template string) (*tools.InspectOutput, error) {
		if agentID != "" {
			return inspectByID(reg, agentID)
		}
		if template != "" {
			return inspectByTemplate(reg, template)
		}
		return inspectAll(reg)
	}
}

func inspectByID(reg *Registry, agentID string) (*tools.InspectOutput, error) {
	a, ok := reg.Get(agentID)
	if !ok {
		return nil, fmt.Errorf("agent %q not found", agentID)
	}
	s := agentToStatus(a)
	return &tools.InspectOutput{
		QueryType: "single",
		Single:    &s,
		Summary:   tools.SummaryFrom([]tools.AgentStatus{s}),
	}, nil
}

func inspectByTemplate(reg *Registry, template string) (*tools.InspectOutput, error) {
	agents := reg.GetByTemplate(template)
	if len(agents) == 0 {
		for _, a := range reg.List() {
			if matchAgentTemplate(a, template) {
				agents = append(agents, a)
			}
		}
		if len(agents) == 0 {
			return nil, fmt.Errorf("no agents found for template %q", template)
		}
	}
	statuses := agentsToStatuses(agents)
	teams := tools.GroupByTemplate(statuses)
	return &tools.InspectOutput{
		QueryType: "template",
		Teams:     teams,
		Summary:   tools.SummaryFrom(statuses),
	}, nil
}

func inspectAll(reg *Registry) (*tools.InspectOutput, error) {
	all := reg.List()
	if len(all) == 0 {
		return &tools.InspectOutput{
			QueryType: "all",
			Summary:   tools.StatusSummary{},
		}, nil
	}
	statuses := agentsToStatuses(all)
	teams := tools.GroupByTemplate(statuses)
	return &tools.InspectOutput{
		QueryType: "all",
		Teams:     teams,
		Summary:   tools.SummaryFrom(statuses),
	}, nil
}

// ─── Supervisor-scoped query ────────────────────────────────────────────

// SupervisorInspectQuery returns a tools.InspectQueryFn scoped to the children
// of a Supervisor. This is used for L2 (domain leader) agents.
func SupervisorInspectQuery(sv *Supervisor) tools.InspectQueryFn {
	return func(ctx context.Context, agentID, template string) (*tools.InspectOutput, error) {
		children := sv.Children()
		if agentID != "" {
			for _, c := range children {
				if c.InstanceID == agentID {
					s := agentToStatus(c)
					return &tools.InspectOutput{
						QueryType: "single",
						Single:    &s,
						Summary:   tools.SummaryFrom([]tools.AgentStatus{s}),
					}, nil
				}
			}
			return nil, fmt.Errorf("agent %q not found in supervisor children", agentID)
		}
		if len(children) == 0 {
			return &tools.InspectOutput{
				QueryType: "all",
				Summary:   tools.StatusSummary{},
			}, nil
		}
		statuses := agentsToStatuses(children)
		if template != "" {
			statuses = tools.FilterByTemplate(statuses, template)
			if len(statuses) == 0 {
				return nil, fmt.Errorf("no children found for template %q", template)
			}
		}
		teams := tools.GroupByTemplate(statuses)
		return &tools.InspectOutput{
			QueryType: "all",
			Teams:     teams,
			Summary:   tools.SummaryFrom(statuses),
		}, nil
	}
}

// ─── Conversion helpers ─────────────────────────────────────────────────

func agentToStatus(a *Agent) tools.AgentStatus {
	w := a.CurrentWork()
	return tools.AgentStatus{
		InstanceID:   a.InstanceID,
		TemplateID:   a.Def.ID,
		TemplateName: a.Def.Name,
		State:        w.State.String(),
		Prompt:       w.Prompt,
		Iteration:    w.Iteration,
		CurrentTool:  w.CurrentTool,
		Elapsed:      w.Elapsed,
		ErrorCount:   w.ErrorCount,
		LastError:    w.LastError,
	}
}

func agentsToStatuses(agents []*Agent) []tools.AgentStatus {
	out := make([]tools.AgentStatus, 0, len(agents))
	for _, a := range agents {
		out = append(out, agentToStatus(a))
	}
	return out
}

func matchAgentTemplate(a *Agent, pattern string) bool {
	pattern = strings.ToLower(pattern)
	if strings.Contains(strings.ToLower(a.Def.ID), pattern) {
		return true
	}
	if strings.Contains(strings.ToLower(a.Def.Name), pattern) {
		return true
	}
	return false
}
