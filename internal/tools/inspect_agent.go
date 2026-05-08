package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ─── Data types ───────────────────────────────────────────────────────────

// AgentStatus is a per-agent status entry for inspect_agent output.
type AgentStatus struct {
	InstanceID   string `json:"instance_id"`
	TemplateID   string `json:"template_id"`
	TemplateName string `json:"template_name"`
	State        string `json:"state"`
	Prompt       string `json:"prompt,omitempty"`
	Iteration    int    `json:"iteration"`
	CurrentTool  string `json:"current_tool,omitempty"`
	Elapsed      string `json:"elapsed"`
	ErrorCount   int    `json:"error_count"`
	LastError    string `json:"last_error,omitempty"`
}

// TeamStatus groups agent statuses by template.
type TeamStatus struct {
	TemplateID   string        `json:"template_id"`
	TemplateName string        `json:"template_name"`
	Agents       []AgentStatus `json:"agents"`
}

// StatusSummary provides aggregate counts across agents.
type StatusSummary struct {
	Total   int `json:"total"`
	Running int `json:"running"`
	Idle    int `json:"idle"`
	Stopped int `json:"stopped"`
}

// InspectOutput bundles the result of an inspect_agent query.
type InspectOutput struct {
	QueryType string        `json:"query_type"`
	Single    *AgentStatus  `json:"single,omitempty"`
	Teams     []TeamStatus  `json:"teams,omitempty"`
	Summary   StatusSummary `json:"summary"`
}

// InspectQueryFn is the signature for the function that resolves inspect_agent
// queries. The factory wires either a Registry-scoped or Supervisor-scoped
// implementation.
type InspectQueryFn func(ctx context.Context, agentID, template string) (*InspectOutput, error)

// ─── Tool ─────────────────────────────────────────────────────────────────

// InspectAgentTool implements Tool for agent status introspection.
type InspectAgentTool struct {
	queryFn InspectQueryFn
}

// NewInspectAgentTool creates a new tool with the given query function.
func NewInspectAgentTool(fn InspectQueryFn) *InspectAgentTool {
	return &InspectAgentTool{queryFn: fn}
}

func (t *InspectAgentTool) Name() string        { return "inspect_agent" }
func (t *InspectAgentTool) Description() string  { return inspectAgentDesc }
func (t *InspectAgentTool) Parameters() json.RawMessage {
	return json.RawMessage(inspectAgentParams)
}

func (t *InspectAgentTool) Execute(ctx context.Context, args string) (result string, err error) {
	if t.queryFn == nil {
		return `{"error": "inspect_agent: query function not configured"}`, nil
	}

	var p inspectArgs
	if args != "" {
		if err := json.Unmarshal([]byte(args), &p); err != nil {
			return fmt.Sprintf(`{"error": "invalid args: %s"}`, err.Error()), nil
		}
	}

	output, err := t.queryFn(ctx, p.AgentID, p.Template)
	if err != nil {
		return fmt.Sprintf(`{"error": "%s"}`, err.Error()), nil
	}

	b, err := json.Marshal(output)
	if err != nil {
		return fmt.Sprintf(`{"error": "marshal: %s"}`, err.Error()), nil
	}
	return string(b), nil
}

type inspectArgs struct {
	AgentID  string `json:"agent_id,omitempty"`
	Template string `json:"template,omitempty"`
}

const inspectAgentDesc = `Query agent status and progress. Supports three modes:
- No arguments: returns status for all managed agents grouped by template.
- template="name": returns status for agents matching the given template (fuzzy match on template ID or name).
- agent_id="uuid": returns detailed status for a single agent instance.

Returns JSON with agent state, current prompt, iteration, tool, elapsed time, and error counts.`

const inspectAgentParams = `{
  "type": "object",
  "properties": {
    "agent_id": {
      "type": "string",
      "description": "Instance ID (UUID) of a specific agent to inspect"
    },
    "template": {
      "type": "string",
      "description": "Template name or ID to filter agents by (fuzzy match)"
    }
  },
  "additionalProperties": false
}`

// ─── Helpers (used by agent package factory functions) ─────────────────────

// GroupByTemplate groups agent statuses by template ID.
func GroupByTemplate(statuses []AgentStatus) []TeamStatus {
	groups := make(map[string]*TeamStatus)
	var keys []string
	for _, s := range statuses {
		id := s.TemplateID
		if existing, ok := groups[id]; ok {
			existing.Agents = append(existing.Agents, s)
		} else {
			groups[id] = &TeamStatus{
				TemplateID:   id,
				TemplateName: s.TemplateName,
				Agents:       []AgentStatus{s},
			}
			keys = append(keys, id)
		}
	}
	out := make([]TeamStatus, 0, len(keys))
	for _, k := range keys {
		out = append(out, *groups[k])
	}
	return out
}

// SummaryFrom computes aggregate counts from agent statuses.
func SummaryFrom(statuses []AgentStatus) StatusSummary {
	var s StatusSummary
	s.Total = len(statuses)
	for _, st := range statuses {
		switch st.State {
		case "processing":
			s.Running++
		case "idle":
			s.Idle++
		default:
			s.Stopped++
		}
	}
	return s
}

// FilterByTemplate filters statuses by fuzzy-matching template ID or name.
func FilterByTemplate(statuses []AgentStatus, pattern string) []AgentStatus {
	pattern = strings.ToLower(pattern)
	var out []AgentStatus
	for _, s := range statuses {
		if strings.Contains(strings.ToLower(s.TemplateID), pattern) ||
			strings.Contains(strings.ToLower(s.TemplateName), pattern) {
			out = append(out, s)
		}
	}
	return out
}
