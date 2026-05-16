import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { PlanCard } from './PlanCard'
import type { Plan } from '@/types'

const basePlan: Plan = {
  id: 'p1',
  title: 'Test Plan',
  content: 'Some content',
  status: 'plan',
  tags: 'bug,frontend',
  creator: 'user',
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-02T00:00:00Z',
  todo_items: [
    { id: 't1', plan_id: 'p1', content: 'Task 1', completed: true, sort_order: 0, depends_on: [], blockers: [], created_at: '' },
    { id: 't2', plan_id: 'p1', content: 'Task 2', completed: false, sort_order: 1, depends_on: [], blockers: [], created_at: '' },
  ],
}

describe('PlanCard', () => {
  it('renders title and tags', () => {
    render(<PlanCard plan={basePlan} onClick={vi.fn()} />)
    expect(screen.getByText('Test Plan')).toBeInTheDocument()
    expect(screen.getByText('bug')).toBeInTheDocument()
    expect(screen.getByText('frontend')).toBeInTheDocument()
  })

  it('shows task progress', () => {
    render(<PlanCard plan={basePlan} onClick={vi.fn()} />)
    expect(screen.getByText('1/2 tasks')).toBeInTheDocument()
    expect(screen.getByText('50%')).toBeInTheDocument()
  })

  it('shows creator and date', () => {
    render(<PlanCard plan={basePlan} onClick={vi.fn()} />)
    expect(screen.getByText('@user')).toBeInTheDocument()
  })

  it('handles no todo items', () => {
    const plan = { ...basePlan, todo_items: undefined }
    render(<PlanCard plan={plan} onClick={vi.fn()} />)
    expect(screen.queryByText(/tasks/)).not.toBeInTheDocument()
  })

  it('handles overlay class', () => {
    const { container } = render(<PlanCard plan={basePlan} onClick={vi.fn()} isOverlay />)
    expect(container.firstChild).toHaveClass('scale-105')
  })
})
