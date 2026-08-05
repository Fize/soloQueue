package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
)

// WorkflowTask is the structured user intent shared by HTTP and agent-tool
// workflow starts. Goal and acceptance criteria are required at the boundary.
type WorkflowTask struct {
	Goal               string          `json:"goal"`
	AcceptanceCriteria []string        `json:"acceptance_criteria"`
	Constraints        []string        `json:"constraints,omitempty"`
	Delivery           DeliveryRequest `json:"delivery,omitempty"`
}

type DeliveryRequest struct {
	Commit      *CommitRequest      `json:"commit,omitempty"`
	Push        *PushRequest        `json:"push,omitempty"`
	PullRequest *PullRequestRequest `json:"pull_request,omitempty"`
}

type CommitRequest struct {
	Enabled bool   `json:"enabled"`
	Message string `json:"message,omitempty"`
}

type PushRequest struct {
	Enabled bool   `json:"enabled"`
	Remote  string `json:"remote,omitempty"`
	Branch  string `json:"branch,omitempty"`
}

type PullRequestRequest struct {
	Enabled bool   `json:"enabled"`
	Title   string `json:"title,omitempty"`
	Body    string `json:"body,omitempty"`
	Draft   bool   `json:"draft,omitempty"`
}

func (t WorkflowTask) Validate() error {
	if strings.TrimSpace(t.Goal) == "" {
		return fmt.Errorf("workflow_task_invalid: goal is required")
	}
	if len([]rune(t.Goal)) > 20000 {
		return fmt.Errorf("workflow_task_invalid: goal exceeds 20000 characters")
	}
	if len(t.AcceptanceCriteria) == 0 {
		return fmt.Errorf("workflow_task_invalid: at least one acceptance criterion is required")
	}
	for i, criterion := range t.AcceptanceCriteria {
		if strings.TrimSpace(criterion) == "" {
			return fmt.Errorf("workflow_task_invalid: acceptance_criteria[%d] is empty", i)
		}
		if len([]rune(criterion)) > 4000 {
			return fmt.Errorf("workflow_task_invalid: acceptance_criteria[%d] exceeds 4000 characters", i)
		}
	}
	for i, constraint := range t.Constraints {
		if len([]rune(constraint)) > 4000 {
			return fmt.Errorf("workflow_task_invalid: constraints[%d] exceeds 4000 characters", i)
		}
	}
	if t.Delivery.PullRequest != nil && t.Delivery.PullRequest.Enabled {
		if t.Delivery.Commit == nil || !t.Delivery.Commit.Enabled {
			return fmt.Errorf("workflow_delivery_invalid: pull request requires commit.enabled")
		}
		if t.Delivery.Push == nil || !t.Delivery.Push.Enabled {
			return fmt.Errorf("workflow_delivery_invalid: pull request requires push.enabled")
		}
	}
	return nil
}

func (t WorkflowTask) Normalized() WorkflowTask {
	out := t
	out.Goal = strings.TrimSpace(out.Goal)
	out.AcceptanceCriteria = compactStrings(out.AcceptanceCriteria)
	out.Constraints = compactStrings(out.Constraints)
	return out
}

func (t WorkflowTask) JSON() (string, error) {
	b, err := json.Marshal(t.Normalized())
	if err != nil {
		return "", fmt.Errorf("workflow_task: marshal: %w", err)
	}
	return string(b), nil
}

func (t WorkflowTask) PromptInput() string {
	var b strings.Builder
	b.WriteString("# Task Goal\n")
	b.WriteString(t.Goal)
	b.WriteString("\n\n# Acceptance Criteria\n")
	for _, criterion := range t.AcceptanceCriteria {
		b.WriteString("- ")
		b.WriteString(criterion)
		b.WriteByte('\n')
	}
	if len(t.Constraints) > 0 {
		b.WriteString("\n# Optional Constraints\n")
		for _, constraint := range t.Constraints {
			b.WriteString("- ")
			b.WriteString(constraint)
			b.WriteByte('\n')
		}
	}
	if t.Delivery.Commit != nil || t.Delivery.Push != nil || t.Delivery.PullRequest != nil {
		if raw, err := json.Marshal(t.Delivery); err == nil {
			b.WriteString("\n# Explicit Delivery Policy\n")
			b.Write(raw)
			b.WriteByte('\n')
		}
	} else {
		b.WriteString("\n# Explicit Delivery Policy\nNo commit, push, or pull request was requested.\n")
	}
	return b.String()
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}
