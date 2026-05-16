import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { PlanDetail } from './PlanDetail'
import { usePlanStore } from '@/stores/planStore'
import * as api from '@/lib/api'
import type { Plan } from '@/types'

vi.mock('@/stores/planStore', () => ({
  usePlanStore: vi.fn(),
}))

vi.mock('@/lib/api', () => ({
  getPlan: vi.fn(),
  toggleTodo: vi.fn(),
  deleteTodo: vi.fn(),
}))

vi.mock('./TodoList', () => ({
  TodoList: ({ onToggle, onDelete }: { onToggle: (id: string) => void; onDelete: (id: string) => void }) => (
    <div data-testid="todo-list">
      <button onClick={() => onToggle('t1')}>Toggle</button>
      <button onClick={() => onDelete('t2')}>Delete</button>
    </div>
  ),
}))

vi.mock('./FilePreview', () => ({
  FilePreview: ({ open }: { open: boolean }) =>
    open ? <div data-testid="file-preview">Preview</div> : null,
}))

const mockPlan: Plan = {
  id: 'p1',
  title: 'Test Plan',
  content: '## Hello\n\nThis is **markdown** content.',
  status: 'running',
  tags: 'bug,frontend',
  creator: 'user',
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-02T00:00:00Z',
  todo_items: [
    { id: 't1', plan_id: 'p1', content: 'Task 1', completed: true, sort_order: 0, depends_on: [], blockers: [], created_at: '' },
    { id: 't2', plan_id: 'p1', content: 'Task 2', completed: false, sort_order: 1, depends_on: [], blockers: [], created_at: '' },
  ],
}

const mockUpdatePlan = vi.fn()
const mockDeletePlan = vi.fn()

beforeEach(() => {
  vi.clearAllMocks()
  ;(usePlanStore as unknown as ReturnType<typeof vi.fn>).mockImplementation(
    (selector: (s: Record<string, unknown>) => unknown) =>
      selector({
        updatePlan: mockUpdatePlan,
        deletePlan: mockDeletePlan,
        plans: [mockPlan],
      })
  )
  vi.mocked(api.getPlan).mockResolvedValue(mockPlan)
})

describe('PlanDetail', () => {
  it('renders plan title and status', async () => {
    render(<PlanDetail plan={mockPlan} open={true} onClose={vi.fn()} />)
    await waitFor(() => {
      expect(screen.getByText('Test Plan')).toBeInTheDocument()
    })
    expect(screen.getByText('Running')).toBeInTheDocument()
  })

  it('shows edit and delete buttons', async () => {
    render(<PlanDetail plan={mockPlan} open={true} onClose={vi.fn()} />)
    await waitFor(() => {
      expect(screen.getByTitle('Edit')).toBeInTheDocument()
    })
    expect(screen.getByTitle('Delete')).toBeInTheDocument()
  })

  it('enters edit mode on Edit click', async () => {
    const user = userEvent.setup()
    render(<PlanDetail plan={mockPlan} open={true} onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByTitle('Edit')).toBeInTheDocument())
    await user.click(screen.getByTitle('Edit'))

    expect(screen.getByTitle('Save')).toBeInTheDocument()
    expect(screen.getByTitle('Cancel')).toBeInTheDocument()
  })

  it('saves edits', async () => {
    const user = userEvent.setup()
    mockUpdatePlan.mockResolvedValue(mockPlan)
    render(<PlanDetail plan={mockPlan} open={true} onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByTitle('Edit')).toBeInTheDocument())
    await user.click(screen.getByTitle('Edit'))

    const titleInput = screen.getByDisplayValue('Test Plan')
    await user.clear(titleInput)
    await user.type(titleInput, 'Updated Plan')

    await user.click(screen.getByTitle('Save'))
    await waitFor(() => {
      expect(mockUpdatePlan).toHaveBeenCalledWith('p1', expect.objectContaining({ title: 'Updated Plan' }))
    })
  })

  it('shows delete confirmation', async () => {
    const user = userEvent.setup()
    render(<PlanDetail plan={mockPlan} open={true} onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByTitle('Delete')).toBeInTheDocument())
    await user.click(screen.getByTitle('Delete'))

    expect(screen.getByText('Delete this plan? This action cannot be undone.')).toBeInTheDocument()
  })

  it('executes delete from confirmation', async () => {
    const user = userEvent.setup()
    mockDeletePlan.mockResolvedValue(undefined)
    const onClose = vi.fn()
    render(<PlanDetail plan={mockPlan} open={true} onClose={onClose} />)
    await waitFor(() => expect(screen.getByTitle('Delete')).toBeInTheDocument())
    await user.click(screen.getByTitle('Delete'))
    await user.click(screen.getByText('Delete'))

    await waitFor(() => {
      expect(mockDeletePlan).toHaveBeenCalledWith('p1')
    })
    expect(onClose).toHaveBeenCalled()
  })

  it('renders markdown content', async () => {
    render(<PlanDetail plan={mockPlan} open={true} onClose={vi.fn()} />)
    await waitFor(() => {
      expect(screen.getByText('Hello')).toBeInTheDocument()
    })
    expect(screen.getByText('markdown')).toBeInTheDocument()
  })

  it('shows task list', async () => {
    render(<PlanDetail plan={mockPlan} open={true} onClose={vi.fn()} />)
    await waitFor(() => {
      expect(screen.getByTestId('todo-list')).toBeInTheDocument()
    })
  })

  it('shows progress for tasks', async () => {
    render(<PlanDetail plan={mockPlan} open={true} onClose={vi.fn()} />)
    await waitFor(() => {
      expect(screen.getByText('1/2')).toBeInTheDocument()
    })
  })

  it('calls toggleTodo', async () => {
    vi.mocked(api.toggleTodo).mockResolvedValue({ id: 't1', plan_id: 'p1', content: '', completed: true, sort_order: 0, depends_on: [], blockers: [], created_at: '' })
    const user = userEvent.setup()
    render(<PlanDetail plan={mockPlan} open={true} onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByTestId('todo-list')).toBeInTheDocument())
    await user.click(screen.getByText('Toggle'))
    expect(api.toggleTodo).toHaveBeenCalledWith('p1', 't1')
  })
})
