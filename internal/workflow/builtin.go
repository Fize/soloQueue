package workflow

import (
	"context"
	"fmt"
	"strings"
)

type BuiltinWorkflowStatus string

const (
	BuiltinWorkflowAvailable BuiltinWorkflowStatus = "available"
	BuiltinWorkflowInstalled BuiltinWorkflowStatus = "installed"
	BuiltinWorkflowConflict  BuiltinWorkflowStatus = "conflict"
)

type BuiltinWorkflowSpec struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	YAML        string `json:"yaml"`
}

type BuiltinWorkflowView struct {
	Spec   BuiltinWorkflowSpec   `json:"spec"`
	Status BuiltinWorkflowStatus `json:"status"`
	Error  string                `json:"error,omitempty"`
}

type BuiltinWorkflowInstallResult struct {
	Name    string                `json:"name"`
	Status  BuiltinWorkflowStatus `json:"status"`
	Created bool                  `json:"created"`
}

const EngineeringQualityLoopYAML = `name: engineering-quality-loop
description: "Built-in engineering quality loop: develop, verify, fix, review, test, and final acceptance."
version: "1"
defaults:
  node_timeout: 20m
  workflow_timeout: 60m
  max_node_runs: 80
agents:
  architect:
    template: andrej karpathy
  explorer:
    template: explorer
  editor:
    template: editor
  tester:
    template: tester
entry:
  - analyze
nodes:
  - id: analyze
    agent: explorer
    prompt: |
      Analyze the repository and task. Map affected files, current behavior, risks, and a verification strategy.
      Do not modify files. Preserve the user's requested delivery policy.
    outputs:
      analyzed:
        to: [plan]
  - id: plan
    agent: architect
    prompt: |
      Turn the analysis into an implementation plan with explicit acceptance checks, rollback notes, and ownership.
      Do not commit, push, or open a PR unless the task delivery request explicitly requires it.
    outputs:
      planned:
        to: [develop]
  - id: develop
    agent: editor
    prompt: |
      Implement the approved plan in the isolated worktree. Keep changes scoped, preserve unrelated work, and run focused checks.
      Do not commit, push, or open a PR unless explicitly requested by the task.
    outputs:
      implemented:
        to: [completion_check]
  - id: completion_check
    agent: architect
    prompt: |
      Judge whether the goal and every acceptance criterion are complete using repository evidence and test output.
      If incomplete, describe exact gaps and files to fix; if complete, hand off for review.
    outputs:
      complete:
        to: [review]
      incomplete:
        to: [completion_fix]
        loop: true
        max_traversals: 3
  - id: completion_fix
    agent: editor
    prompt: |
      Fix only the gaps identified by completion_check, then report evidence for each acceptance criterion.
    outputs:
      fixed:
        to: [completion_check]
  - id: review
    agent: architect
    prompt: |
      Review the implementation for correctness, regressions, security, scope, and maintainability. Use the task constraints.
      Do not change files in this node.
    outputs:
      approved:
        to: [test]
      changes_requested:
        to: [review_fix]
        loop: true
        max_traversals: 3
  - id: review_fix
    agent: editor
    prompt: |
      Apply the review fixes, preserving the acceptance criteria and delivery policy, then summarize the changes.
    outputs:
      fixed:
        to: [review]
  - id: test
    agent: tester
    prompt: |
      Run the appropriate unit, integration, API, and focused end-to-end checks. Record commands, outputs, and failures.
    outputs:
      passed:
        to: [final_check]
      failed:
        to: [test_fix]
        loop: true
        max_traversals: 3
  - id: test_fix
    agent: editor
    prompt: |
      Diagnose and fix the test failures without weakening the acceptance criteria. Return to test after the fix.
    outputs:
      fixed:
        to: [test]
  - id: final_check
    agent: architect
    prompt: |
      Perform the final acceptance decision from the complete audit trail. Confirm goal, criteria, constraints, review, and tests.
      Delivery actions remain governed only by task.delivery.
    outputs:
      accepted:
        to: []
        terminal_status: completed
      blocked:
        to: []
        terminal_status: blocked
      failed:
        to: []
        terminal_status: failed
`

func BuiltinWorkflowCatalog() []BuiltinWorkflowSpec {
	return []BuiltinWorkflowSpec{{Name: "engineering-quality-loop", Description: "Built-in engineering quality loop: develop, verify, fix, review, test, and final acceptance.", Version: "1", YAML: EngineeringQualityLoopYAML}}
}

func BuiltinWorkflowByName(name string) (BuiltinWorkflowSpec, bool) {
	for _, spec := range BuiltinWorkflowCatalog() {
		if spec.Name == name {
			return spec, true
		}
	}
	return BuiltinWorkflowSpec{}, false
}

func (s *Store) ListBuiltinWorkflowStatuses(ctx context.Context) ([]BuiltinWorkflowView, error) {
	_ = ctx
	result := make([]BuiltinWorkflowView, 0)
	for _, spec := range BuiltinWorkflowCatalog() {
		view := BuiltinWorkflowView{Spec: spec, Status: BuiltinWorkflowAvailable}
		raw, err := s.ReadRaw(spec.Name)
		if err == nil {
			if string(raw) == spec.YAML {
				view.Status = BuiltinWorkflowInstalled
			} else {
				view.Status = BuiltinWorkflowConflict
				view.Error = "workflow exists with different content"
			}
		} else if !strings.Contains(err.Error(), "not found") {
			view.Error = err.Error()
		}
		result = append(result, view)
	}
	return result, nil
}

func (s *Store) InstallBuiltinWorkflows(ctx context.Context, names []string) ([]BuiltinWorkflowInstallResult, error) {
	_ = ctx
	if len(names) == 0 {
		return nil, fmt.Errorf("workflow_builtin_invalid: at least one workflow is required")
	}
	result := make([]BuiltinWorkflowInstallResult, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if seen[name] {
			continue
		}
		seen[name] = true
		spec, ok := BuiltinWorkflowByName(name)
		if !ok {
			return nil, fmt.Errorf("workflow_builtin_not_found: %s", name)
		}
		raw, err := s.ReadRaw(name)
		if err == nil {
			if string(raw) != spec.YAML {
				result = append(result, BuiltinWorkflowInstallResult{Name: name, Status: BuiltinWorkflowConflict})
				continue
			}
			result = append(result, BuiltinWorkflowInstallResult{Name: name, Status: BuiltinWorkflowInstalled})
			continue
		}
		if !strings.Contains(err.Error(), "not found") {
			return nil, err
		}
		if _, err := s.Save(name, []byte(spec.YAML)); err != nil {
			return nil, err
		}
		result = append(result, BuiltinWorkflowInstallResult{Name: name, Status: BuiltinWorkflowInstalled, Created: true})
	}
	return result, nil
}
