import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { AgentFlow } from './AgentFlow'
import { useAgentStore } from '@/stores/agentStore'

vi.mock('@/stores/agentStore', () => ({
  useAgentStore: vi.fn(),
}))

vi.mock('@/hooks/useAgents', () => ({
  useAgents: vi.fn(() => null),
}))

vi.mock('@xyflow/react', () => ({
  ReactFlow: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="react-flow">{children}</div>
  ),
  Background: () => null,
  Handle: () => null,
  Position: { Top: 'top', Bottom: 'bottom' },
  useNodesState: () => [[], vi.fn(), vi.fn()],
  useEdgesState: () => [[], vi.fn(), vi.fn()],
  useReactFlow: () => ({ fitView: vi.fn(), getNodes: vi.fn(() => []) }),
}))

vi.mock('./AgentDetailDialog', () => ({
  AgentDetailDialog: ({ open }: { open: boolean }) =>
    open ? <div data-testid="agent-detail-dialog">Dialog</div> : null,
}))

beforeEach(() => {
  vi.clearAllMocks()
})

describe('AgentFlow', () => {
  it('renders without crashing', () => {
    ;(useAgentStore as unknown as ReturnType<typeof vi.fn>).mockImplementation(
      (selector: (s: Record<string, unknown>) => unknown) =>
        selector({ agents: null, teams: null, teamsLoading: false, fetchTeams: vi.fn() })
    )
    const { container } = render(<AgentFlow />)
    expect(container).toBeTruthy()
  })
})
