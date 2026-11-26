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
    drafts: [] as Array<Record<string, unknown>>,
    sessionsLoading: false,
    activeSessionId: 'alpha-session',
    streamingSessions: {},
    loadSessions: vi.fn().mockResolvedValue(undefined),
    createL2Session: vi.fn(),
    deleteL2Session: vi.fn(),
    setActiveSession: vi.fn(),
    beginDraft: vi.fn(),
    clearDraft: vi.fn(),
  },
  fetchLiveAgents: vi.fn().mockResolvedValue(undefined),
  listL2Groups: vi.fn().mockResolvedValue(['alpha', 'beta']),
  listProjects: vi.fn().mockResolvedValue([]),
  getTeams: vi.fn().mockResolvedValue({ teams: [] }),
}))

vi.mock('@/stores/chatStore', () => ({
  useChatStore: (selector: (state: typeof mocks.chatState) => unknown) =>
    selector(mocks.chatState),
  isDraftSessionId: (id?: string | null) => !!id && id.startsWith('draft:'),
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

  it('renders sessions in the order the store provides', async () => {
    // Deliberately anti-chronological createdAt values: the tree must not
    // re-sort them client-side, because the store owns the ordering rule
    // (last activity first) and the server already applied it.
    mocks.chatState.sessions = [
      { id: 'c-session', type: 'l2', group: 'alpha', name: 'Charlie', createdAt: '2026-01-01T00:00:00Z', last_active_at: '2026-11-26T12:00:00Z' },
      { id: 'a-session', type: 'l2', group: 'alpha', name: 'Alpha', createdAt: '2026-11-01T00:00:00Z', last_active_at: '2026-11-26T11:00:00Z' },
      { id: 'b-session', type: 'l2', group: 'alpha', name: 'Bravo', createdAt: '2026-06-01T00:00:00Z', last_active_at: '2026-11-26T10:00:00Z' },
    ]
    mocks.chatState.activeSessionId = 'a-session'

    const { container } = render(
      <MemoryRouter>
        <SessionTree />
      </MemoryRouter>
    )

    await waitFor(() => expect(container.querySelector('[data-session-id="c-session"]')).toBeInTheDocument())
    const order = Array.from(container.querySelectorAll('[data-session-id]')).map((el) =>
      el.getAttribute('data-session-id')
    )
    expect(order).toEqual(['c-session', 'a-session', 'b-session'])
  })

  it('shows an unstarted new chat as a placeholder row and drops it when discarded', async () => {
    mocks.chatState.sessions = [
      { id: 'alpha-session', type: 'l2', group: 'alpha', name: 'Alpha chat', createdAt: '2026-11-26T10:00:00Z' },
    ]
    mocks.chatState.drafts = [
      { id: 'draft:one', type: 'l2', group: 'alpha', name: '', draft: true, createdAt: '2026-11-26T12:00:00Z' },
    ]
    mocks.chatState.activeSessionId = 'draft:one'

    const { container, rerender } = render(
      <MemoryRouter>
        <SessionTree />
      </MemoryRouter>
    )

    await waitFor(() => expect(container.querySelector('[data-session-id="draft:one"]')).toBeInTheDocument())
    // The placeholder is distinguishable from a titled session.
    const draftRow = container.querySelector('[data-session-id="draft:one"]')
    expect(draftRow?.textContent).toContain('sessionTree.draftLabel')
    expect(draftRow?.textContent).toContain('sessionTree.draftBadge')

    // Discarding it removes the row without any backend call.
    act(() => {
      mocks.chatState.drafts = []
      mocks.chatState.activeSessionId = 'alpha-session'
      rerender(
        <MemoryRouter>
          <SessionTree />
        </MemoryRouter>
      )
    })

    await waitFor(() => expect(container.querySelector('[data-session-id="draft:one"]')).not.toBeInTheDocument())
    expect(mocks.chatState.clearDraft).not.toHaveBeenCalled()
  })

  it('creates a placeholder instead of calling the backend when a new chat is requested', async () => {
    const user = userEvent.setup()
    mocks.chatState.sessions = []
    mocks.chatState.drafts = []
    mocks.chatState.beginDraft = vi.fn().mockReturnValue('draft:created')

    render(
      <MemoryRouter>
        <SessionTree />
      </MemoryRouter>
    )

    await waitFor(() => expect(screen.getByRole('button', { name: 'alpha' })).toBeInTheDocument())
    const newButtons = screen.getAllByTitle(/New chat in alpha/)
    await user.click(newButtons[0])

    expect(mocks.chatState.beginDraft).toHaveBeenCalledWith('alpha', '')
    expect(mocks.chatState.createL2Session).not.toHaveBeenCalled()
  })
})
