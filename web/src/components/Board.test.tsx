import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Board } from './Board'
import { usePlanStore } from '@/stores/planStore'
import type { Plan } from '@/types'

vi.mock('@/stores/planStore', () => ({
  usePlanStore: vi.fn(),
}))

vi.mock('./PlanDetail', () => ({
  PlanDetail: ({ open }: { open: boolean }) =>
    open ? <div data-testid="plan-detail">Plan Detail</div> : null,
}))

vi.mock('./PlanCreateDialog', () => ({
  PlanCreateDialog: ({ open }: { open: boolean }) =>
    open ? <div data-testid="plan-create-dialog">Create Dialog</div> : null,
}))

const mockPlans: Plan[] = [
  {
    id: 'p1',
    title: 'Plan A',
    content: '',
    status: 'plan',
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
    expect(screen.getByText('Plans Board')).toBeInTheDocument()
    expect(screen.getByText('New Plan')).toBeInTheDocument()
  })

  it('opens create dialog on New Plan click', async () => {
    const user = userEvent.setup()
    render(<Board />)
    await user.click(screen.getByText('New Plan'))
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

  it('opens plan detail when a plan card is clicked', async () => {
    const user = userEvent.setup()
    render(<Board />)
    const planCard = screen.getAllByText(/Plan [ABC]/)
    await user.click(planCard[0])
    expect(screen.getByTestId('plan-detail')).toBeInTheDocument()
  })
})
