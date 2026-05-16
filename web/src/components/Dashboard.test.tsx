import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { Dashboard } from './Dashboard'
import { usePlanStore } from '@/stores/planStore'
import { useAgentStore } from '@/stores/agentStore'

vi.mock('@/stores/planStore', () => ({
  usePlanStore: vi.fn(),
}))

vi.mock('@/stores/agentStore', () => ({
  useAgentStore: vi.fn(),
}))

vi.mock('@/hooks/useRuntime', () => ({
  useRuntime: vi.fn(() => null),
}))

vi.mock('./AgentFlow', () => ({
  AgentFlow: () => <div data-testid="agent-flow">Agent Flow</div>,
}))

beforeEach(() => {
  vi.clearAllMocks()
  ;(usePlanStore as unknown as ReturnType<typeof vi.fn>).mockImplementation(
    (selector: (s: Record<string, unknown>) => unknown) =>
      selector({ plans: [] })
  )
  ;(useAgentStore as unknown as ReturnType<typeof vi.fn>).mockImplementation(
    (selector: (s: Record<string, unknown>) => unknown) =>
      selector({ agents: null })
  )
})

describe('Dashboard', () => {
  it('renders stats cards with zeros', () => {
    render(<Dashboard />)
    expect(screen.getByText('Total Plans')).toBeInTheDocument()
    expect(screen.getByText('Running')).toBeInTheDocument()
    expect(screen.getByText('Completed')).toBeInTheDocument()
    expect(screen.getByText('Active Agents')).toBeInTheDocument()
    expect(screen.getAllByText('0')).toHaveLength(4)
  })

  it('shows correct plan counts', () => {
    ;(usePlanStore as unknown as ReturnType<typeof vi.fn>).mockImplementation(
      (selector: (s: Record<string, unknown>) => unknown) =>
        selector({
          plans: [
            { id: '1', status: 'plan' },
            { id: '2', status: 'running' },
            { id: '3', status: 'running' },
            { id: '4', status: 'done' },
          ],
        })
    )
    render(<Dashboard />)
    const values = screen.getAllByText(/^[0-9]+$/)
    expect(values[0]).toHaveTextContent('4')
    expect(values[1]).toHaveTextContent('2')
    expect(values[2]).toHaveTextContent('1')
  })

  it('renders AgentFlow', () => {
    render(<Dashboard />)
    expect(screen.getByTestId('agent-flow')).toBeInTheDocument()
  })
})
