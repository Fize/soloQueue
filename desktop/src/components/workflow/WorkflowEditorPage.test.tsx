import { render, screen } from '@testing-library/react'
import { useWorkflowStore } from '@/stores/workflowStore'
import { fitGraphToViewport } from './DAGPreview'
import { YAMLEditorView } from './WorkflowEditorPage'

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
