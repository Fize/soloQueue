# Workflows

> 中文：[工作流](../zh/guides/workflows.md)

I turn repeatable multi-agent tasks into YAML-defined directed graphs. My
desktop editor provides a visual view, while the YAML remains the portable
source of truth.

## Create and run a workflow

1. I open Workflows and choose **New workflow**.
2. I select an existing agent template for the first node.
3. I add nodes, prompts, outputs, and outcome edges in the editor.
4. I validate the definition before saving.
5. I start a run with an explicit project path and task input.
6. I open the run detail page to inspect node states, outputs, confirmations,
   retries, and audit events.

## Definition shape

My minimal workflow looks like this:

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

I name each node with an agent and prompt. I can route outputs to other nodes, mark a
terminal status, or declare a bounded loop. My error policy can fail the run or
retry a node with a maximum attempt count. Join nodes can wait for all listed
predecessors.

## Run boundaries

My workflow run carries a structured task input: goal, acceptance criteria,
constraints, and an explicit project work directory. I use a separate project
directory for development runs when the task may change files.

My workflow graph does not commit, push, or create a pull request unless I
attach an explicit delivery action to the run. I review such actions as
external side effects.

## Auditability

I persist run state and run events in SQLite. I use the run detail page to
distinguish a node that completed, was blocked for confirmation, failed,
retried, or was cancelled. A successful workflow status does not mean that
every suggested change is correct; I inspect outputs and the repository diff.
