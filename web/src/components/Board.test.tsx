import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Board } from './Board'
import { usePlanStore } from '@/stores/planStore'
import type { Plan } from '@/types'

vi.mock('@/stores/planStore', () => ({
  usePlanStore: vi.fn(),
}))

const mockNavigate = vi.fn()
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = (await importOriginal()) as Record<string, unknown>
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  }
})

vi.mock('./PlanCreateDialog', () => ({
  PlanCreateDialog: ({ open }: { open: boolean }) =>
    open ? <div data-testid="plan-create-dialog">Create Dialog</div> : null,
}))

const mockPlans: Plan[] = [
  {
    id: 'p1',
    title: 'Plan A',
    content: '',
    status: 'todo',
    tags: '',
    creator: 'user',
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    todo_items: [],
  },
  {
    id: 'p2',
    title: 'Plan B',
    content: '',
    status: 'running',
    tags: '',
    creator: 'user',
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    todo_items: [],
  },
  {
    id: 'p3',
    title: 'Plan C',
    content: '',
    status: 'done',
    tags: '',
    creator: 'user',
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    todo_items: [],
  },
]

beforeEach(() => {
  vi.clearAllMocks()
  mockNavigate.mockReset()
  ;(usePlanStore as unknown as ReturnType<typeof vi.fn>).mockImplementation(
    (selector: (s: Record<string, unknown>) => unknown) =>
      selector({
        plans: mockPlans,
        error: null,
        movePlan: vi.fn(),
        fetchPlans: vi.fn(),
      })
  )
})

describe('Board', () => {
  it('renders columns with correct plan counts', () => {
    render(<Board />)
    expect(screen.getByText('Kanban Board')).toBeInTheDocument()
    expect(screen.getByText('New Issue')).toBeInTheDocument()
  })

  it('opens create dialog on New Issue click', async () => {
    const user = userEvent.setup()
    render(<Board />)
    await user.click(screen.getByText('New Issue'))
    expect(screen.getByTestId('plan-create-dialog')).toBeInTheDocument()
  })

  it('shows error state with retry button', () => {
    ;(usePlanStore as unknown as ReturnType<typeof vi.fn>).mockImplementation(
      (selector: (s: Record<string, unknown>) => unknown) =>
        selector({
          plans: [],
          error: 'Failed to fetch',
          movePlan: vi.fn(),
          fetchPlans: vi.fn(),
        })
    )
    render(<Board />)
    expect(screen.getByText('Failed to fetch')).toBeInTheDocument()
    expect(screen.getByText('Retry')).toBeInTheDocument()
  })

  it('navigates to plan detail on mobile when a plan card is clicked', async () => {
    const originalWidth = window.innerWidth
    Object.defineProperty(window, 'innerWidth', { writable: true, configurable: true, value: 500 })
    const user = userEvent.setup()
    render(<Board />)
    const planCard = screen.getAllByText(/Plan [ABC]/)
    await user.click(planCard[0])
    expect(mockNavigate).toHaveBeenCalledWith('/kanban/p1')
    Object.defineProperty(window, 'innerWidth', {
      writable: true,
      configurable: true,
      value: originalWidth,
    })
  })

  it('opens detail dialog on desktop when a plan card is clicked', async () => {
    const originalWidth = window.innerWidth
    Object.defineProperty(window, 'innerWidth', { writable: true, configurable: true, value: 1024 })
    const user = userEvent.setup()
    render(<Board />)
    const planCard = screen.getAllByText(/Plan [ABC]/)
    await user.click(planCard[0])
    expect(mockNavigate).not.toHaveBeenCalled()
    Object.defineProperty(window, 'innerWidth', {
      writable: true,
      configurable: true,
      value: originalWidth,
    })
  })
})
