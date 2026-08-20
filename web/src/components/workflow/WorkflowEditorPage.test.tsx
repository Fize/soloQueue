import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import { useWorkflowStore } from '@/stores/workflowStore'
import { fitGraphToViewport } from './DAGPreview'
import { WorkflowRunDialog, YAMLEditorView } from './WorkflowEditorPage'

vi.mock('@/lib/api/agent-api', () => ({
  listProjects: vi.fn().mockResolvedValue([]),
}))

describe('YAMLEditorView', () => {
  it('gives the textarea field the available editor height', () => {
    useWorkflowStore.setState({
      activeWorkflowYAML: 'name: test-workflow\n',
      activeWorkflowGraph: { nodes: [], edges: [] },
      activeWorkflowValidationError: null,
    })

    render(<YAMLEditorView />)

    expect(screen.getByRole('textbox').parentElement).toHaveClass('h-full')
  })
})

describe('fitGraphToViewport', () => {
  it('keeps every preview node inside a padded viewport', () => {
    const fitted = fitGraphToViewport([
      { id: 'start', x: -400, y: -100 },
      { id: 'finish', x: 1200, y: 500 },
    ], 240, 160)

    expect(fitted.scale).toBeGreaterThan(0)
    for (const point of fitted.points.values()) {
      expect(point.x).toBeGreaterThanOrEqual(24)
      expect(point.x).toBeLessThanOrEqual(216)
      expect(point.y).toBeGreaterThanOrEqual(24)
      expect(point.y).toBeLessThanOrEqual(136)
    }
  })
})

describe('WorkflowRunDialog', () => {
  it('submits goal and acceptance criteria without requiring a project', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const onOpenChange = vi.fn()

    render(
      <WorkflowRunDialog
        open
        running={false}
        onOpenChange={onOpenChange}
        onSubmit={onSubmit}
      />
    )

    const fields = screen.getAllByRole('textbox')
    fireEvent.change(fields[0], { target: { value: 'Review the release' } })
    fireEvent.change(fields[1], { target: { value: 'All checks pass\nSummary is published' } })
    fireEvent.click(screen.getByRole('button', { name: 'Start run' }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith({
      goal: 'Review the release',
      acceptance_criteria: ['All checks pass', 'Summary is published'],
    }, undefined))
  })
})
