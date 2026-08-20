import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { SessionTree } from './SessionTree'

const mocks = vi.hoisted(() => ({
  chatState: {
    sessions: [
      { id: 'alpha-session', type: 'l2', group: 'alpha', name: 'Alpha chat' },
      { id: 'beta-session', type: 'l2', group: 'beta', name: 'Beta chat' },
    ],
    sessionsLoading: false,
    activeSessionId: 'alpha-session',
    streamingSessions: {},
    loadSessions: vi.fn().mockResolvedValue(undefined),
    createL2Session: vi.fn(),
    deleteL2Session: vi.fn(),
    setActiveSession: vi.fn(),
  },
  fetchLiveAgents: vi.fn().mockResolvedValue(undefined),
  listL2Groups: vi.fn().mockResolvedValue(['alpha', 'beta']),
  listProjects: vi.fn().mockResolvedValue([]),
  getTeams: vi.fn().mockResolvedValue({ teams: [] }),
}))

vi.mock('@/stores/chatStore', () => ({
  useChatStore: (selector: (state: typeof mocks.chatState) => unknown) =>
    selector(mocks.chatState),
}))

vi.mock('@/stores/agentStore', () => ({
  useAgentStore: (selector: (state: { fetchLiveAgents: typeof mocks.fetchLiveAgents }) => unknown) =>
    selector({ fetchLiveAgents: mocks.fetchLiveAgents }),
}))

vi.mock('@/stores/connectionStore', () => ({
  useConnectionStore: (
    selector: (state: { backendStatus: { running: boolean } }) => unknown
  ) => selector({ backendStatus: { running: true } }),
}))

vi.mock('@/lib/api', () => ({
  listL2Groups: mocks.listL2Groups,
  listProjects: mocks.listProjects,
  getTeams: mocks.getTeams,
}))

vi.mock('@/lib/i18n', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('@/components/ui/tooltip', () => ({
  TooltipProvider: ({ children }: { children: React.ReactNode }) => children,
  Tooltip: ({ children }: { children: React.ReactNode }) => children,
  TooltipTrigger: ({ children }: { children: React.ReactNode }) => children,
  TooltipContent: ({ children }: { children: React.ReactNode }) => children,
}))

describe('SessionTree', () => {
  beforeEach(() => {
    mocks.chatState.sessions = [
      { id: 'alpha-session', type: 'l2', group: 'alpha', name: 'Alpha chat' },
      { id: 'beta-session', type: 'l2', group: 'beta', name: 'Beta chat' },
    ]
    mocks.chatState.activeSessionId = 'alpha-session'
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
      callback(0)
      return 1
    })
    HTMLElement.prototype.scrollIntoView = vi.fn()
  })

  it('keeps manual expansion on refresh and refocuses only when the active session changes', async () => {
    const user = userEvent.setup()
    const renderTree = () => (
      <MemoryRouter>
        <SessionTree />
      </MemoryRouter>
    )
    const { container, rerender } = render(renderTree())

    await waitFor(() => expect(screen.getByRole('button', { name: 'beta' })).toBeInTheDocument())
    await waitFor(() =>
      expect(container.querySelector('[data-session-id="beta-session"]')).not.toBeInTheDocument()
    )

    await user.click(screen.getByRole('button', { name: 'beta' }))
    expect(container.querySelector('[data-session-id="beta-session"]')).toBeInTheDocument()

    act(() => {
      mocks.chatState.sessions = [...mocks.chatState.sessions]
      rerender(renderTree())
    })

    await waitFor(() =>
      expect(container.querySelector('[data-session-id="beta-session"]')).toBeInTheDocument()
    )

    act(() => {
      mocks.chatState.activeSessionId = 'beta-session'
      rerender(renderTree())
    })

    await waitFor(() =>
      expect(container.querySelector('[data-session-id="alpha-session"]')).not.toBeInTheDocument()
    )
    expect(container.querySelector('[data-session-id="beta-session"]')).toBeInTheDocument()
  })
})
