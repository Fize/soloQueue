import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { PlanCreateDialog } from './PlanCreateDialog'
import { usePlanStore } from '@/stores/planStore'

vi.mock('@/stores/planStore', () => ({
  usePlanStore: vi.fn(),
}))

const mockCreatePlan = vi.fn()

beforeEach(() => {
  vi.clearAllMocks()
  ;(usePlanStore as unknown as ReturnType<typeof vi.fn>).mockImplementation(
    (selector: (s: Record<string, unknown>) => unknown) => selector({ createPlan: mockCreatePlan })
  )
})

describe('PlanCreateDialog', () => {
  it('renders form fields', () => {
    render(<PlanCreateDialog open={true} onClose={vi.fn()} />)
    expect(screen.getByPlaceholderText('Plan title')).toBeInTheDocument()
    expect(screen.getByDisplayValue('Plan')).toBeInTheDocument()
    expect(screen.getByText('Create')).toBeInTheDocument()
    expect(screen.getByText('Cancel')).toBeInTheDocument()
  })

  it('calls createPlan on submit', async () => {
    const user = userEvent.setup()
    mockCreatePlan.mockResolvedValue({ id: 'new' })
    const onClose = vi.fn()
    render(<PlanCreateDialog open={true} onClose={onClose} />)

    await user.type(screen.getByPlaceholderText('Plan title'), 'My New Plan')
    await user.click(screen.getByText('Create'))

    await waitFor(() => {
      expect(mockCreatePlan).toHaveBeenCalledWith(
        expect.objectContaining({ title: 'My New Plan', creator: 'user' })
      )
    })
    expect(onClose).toHaveBeenCalled()
  })

  it('shows error for empty title', async () => {
    const user = userEvent.setup()
    render(<PlanCreateDialog open={true} onClose={vi.fn()} />)
    await user.click(screen.getByText('Create'))
    const errors = screen.getAllByText('Title is required')
    expect(errors.length).toBeGreaterThanOrEqual(1)
  })

  it('shows error on create failure', async () => {
    const user = userEvent.setup()
    mockCreatePlan.mockRejectedValue(new Error('Server error'))
    render(<PlanCreateDialog open={true} onClose={vi.fn()} />)
    await user.type(screen.getByPlaceholderText('Plan title'), 'Test')
    await user.click(screen.getByText('Create'))
    await waitFor(() => {
      const errors = screen.getAllByText('Server error')
      expect(errors.length).toBeGreaterThanOrEqual(1)
    })
  })

  it('closes without creating', async () => {
    const user = userEvent.setup()
    const onClose = vi.fn()
    render(<PlanCreateDialog open={true} onClose={onClose} />)
    await user.click(screen.getByText('Cancel'))
    expect(onClose).toHaveBeenCalled()
  })
})
