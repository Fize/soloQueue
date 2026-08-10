# Workflow Editor Layout — Historical Design Note

> This is an implementation record, not a user guide. For current behavior,
> read [Workflows](../guides/workflows.md).

## Problem

The workflow editor renders the built-in engineering workflow as disconnected,
overlapping nodes, and the YAML editor collapses to its intrinsic textarea
height. The visual editor therefore cannot explain the workflow or edit its
definition reliably.

## Approach

- Parse both block and flow-style YAML output targets through the existing
  `yaml` dependency, while retaining position comments for visual edits.
- Use a deterministic, topology-aware layered layout for graphs without saved
  positions; keep saved positions unchanged.
- Give the YAML editor's flex child an explicit full-height wrapper so the
  textarea owns the available editor body height.
- Fit the React Flow viewport after initial graph/layout data is available and
  after an explicit auto-layout action.

## Dependencies

Existing `yaml`, `@xyflow/react`, Vitest, and the current workflow store/editor
components. No new dependency is introduced.

## Test Cases

- [ ] `yamlToGraph` parses flow-style `to: [target]` and creates the edge.
- [ ] The layout function places a linear workflow in non-overlapping,
      deterministic layers.
- [ ] The YAML editor body and textarea use the available flex height.
- [ ] The read-only DAG preview fits every rendered node into its viewport.

## Explicitly Out of Scope

Backend workflow execution, YAML schema changes, graph editing semantics,
automatic save, and delivery actions such as commit/push/PR.
