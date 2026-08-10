# Workflows

Workflows turn a repeatable multi-agent task into a YAML-defined directed
graph. The desktop editor provides a visual view, while the YAML remains the
portable source of truth.

## Create and run a workflow

1. Open Workflows and choose **New workflow**.
2. Select an existing agent template for the first node.
3. Add nodes, prompts, outputs, and outcome edges in the editor.
4. Validate the definition before saving.
5. Start a run with an explicit project path and task input.
6. Open the run detail page to inspect node states, outputs, confirmations,
   retries, and audit events.

## Definition shape

A minimal workflow looks like this:

~~~yaml
name: docs-check
description: Review documentation and report actionable gaps
version: "1"
defaults:
  node_timeout: 20m
  workflow_timeout: 45m
  max_node_runs: 3
  max_output_bytes: 200000
agents:
  reviewer:
    template: reviewer
    model: ""
entry:
  - inspect
nodes:
  - id: inspect
    agent: reviewer
    prompt: Review the selected project and report concrete documentation gaps.
    outputs:
      completed:
        to: []
        terminal_status: completed
~~~

Each node names an agent and prompt. Outputs can route to other nodes, mark a
terminal status, or declare a bounded loop. Error policy can fail the run or
retry a node with a maximum attempt count. Join nodes can wait for all listed
predecessors.

## Run boundaries

A workflow run should carry a structured task input: goal, acceptance criteria,
constraints, and an explicit project work directory. Use a separate project
directory for development runs when the task may change files.

The workflow graph itself does not commit, push, or create a pull request
unless an explicit delivery action is attached to the run. Review such actions
as external side effects.

## Auditability

Run state and run events are persisted in SQLite. The run detail page is the
best place to distinguish a node that completed, was blocked for confirmation,
failed, retried, or was cancelled. A successful workflow status does not mean
that every suggested change is correct; inspect outputs and the repository
diff.
