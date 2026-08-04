import { render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { AgentDetailPage } from './AgentDetailPage'

const mocks = vi.hoisted(() => ({
  receivedScrollRef: undefined as React.RefObject<HTMLDivElement | null> | undefined,
  navigate: vi.fn(),
  setSearchParams: vi.fn(),
  fetchLiveAgents: vi.fn(),
  fetchTeams: vi.fn(),
  fetchProfile: vi.fn(),
  fetchConfig: vi.fn(),
}))

const agentStoreState = {
  agents: {
    agents: [
      {
        id: 'worker-1',
        instance_id: 'instance-1',
        name: 'Worker One',
        state: 'processing',
        model_id: 'test-model',
        error_count: 0,
        iteration: 1,
        pending_delegations: 0,
        mailbox: 0,
      },
    ],
  },
  teams: { teams: [] },
  fetchLiveAgents: mocks.fetchLiveAgents,
  fetchTeams: mocks.fetchTeams,
  profile: null,
  profileLoading: false,
  config: null,
  configLoading: false,
  fetchProfile: mocks.fetchProfile,
  fetchConfig: mocks.fetchConfig,
}

vi.mock('react-router-dom', () => ({
  useParams: () => ({ id: 'instance-1' }),
  useNavigate: () => mocks.navigate,
  useSearchParams: () => [new URLSearchParams('tab=output'), mocks.setSearchParams],
}))

vi.mock('@/stores/agentStore', () => ({
  useAgentStore: (selector: (state: typeof agentStoreState) => unknown) =>
    selector(agentStoreState),
}))

vi.mock('@/stores/runtimeStore', () => ({
  useRuntimeStore: (selector: (state: { sidebarCollapsed: boolean; status: null }) => unknown) =>
    selector({ sidebarCollapsed: false, status: null }),
}))

vi.mock('@/hooks/useAgentStream', () => ({
  useAgentStream: () => ({
    agent_id: 'instance-1',
    processing: true,
    segments: [{ type: 'content', text: 'live output' }],
    iteration: 1,
  }),
}))

vi.mock('@/components/AgentStreamView', () => ({
  AgentStreamView: ({
    scrollContainerRef,
  }: {
    scrollContainerRef?: React.RefObject<HTMLDivElement | null>
  }) => {
    mocks.receivedScrollRef = scrollContainerRef
    return <div>live output</div>
  },
}))

vi.mock('@/lib/i18n', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

describe('AgentDetailPage', () => {
  it('passes the output ScrollArea viewport to AgentStreamView', async () => {
    render(<AgentDetailPage />)

    expect(await screen.findByText('live output')).toBeInTheDocument()
    await waitFor(() =>
      expect(mocks.receivedScrollRef?.current).toHaveAttribute(
        'data-slot',
        'scroll-area-viewport'
      )
    )
  })
})
