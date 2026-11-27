import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { ChatMessage } from '@/types'
import { ChatMessageView } from './ChatMessage'

const mocks = vi.hoisted(() => ({
  rewindSession: vi.fn(),
  deleteSessionMessages: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
}))

vi.mock('@/stores/runtimeStore', () => ({
  useRuntimeStore: (selector: (state: { isDesignMode: boolean }) => unknown) =>
    selector({ isDesignMode: false }),
}))

vi.mock('@/stores/chatStore', () => ({
  useChatStore: (
    selector: (state: {
      activeSessionId: string
      routeSessions: Record<string, { requestId: string } | undefined>
      activeRequests?: Record<string, { sessionId: string }>
      rewindSession: typeof mocks.rewindSession
      deleteSessionMessages: typeof mocks.deleteSessionMessages
    }) => unknown
  ) =>
    selector({
      activeSessionId: 'session-1',
      routeSessions: { 'session-1': { requestId: 'request-active' } },
      activeRequests: {
        'request-active': { sessionId: 'session-1', status: 'streaming' },
        'req-background': { sessionId: 'session-1' },
        '860eb610-a88a-433a-937d-7620d7e84642': { sessionId: 'session-1' },
      },
      rewindSession: mocks.rewindSession,
      deleteSessionMessages: mocks.deleteSessionMessages,
    }),
}))

vi.mock('@/lib/i18n', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('sonner', () => ({
  toast: { success: mocks.toastSuccess, error: mocks.toastError },
}))

vi.mock('./chat/SegmentView', () => ({
  SegmentView: ({ segment, requestId }: { segment: { text?: string }; requestId?: string }) => (
    <span data-testid={`segment-${segment.text}`} data-request-id={requestId ?? ''}>
      {segment.text}
    </span>
  ),
  LoadingIndicator: () => <span>loading</span>,
}))

vi.mock('./chat/WorkedSegment', () => ({ WorkedSegment: () => null }))
vi.mock('./chat/DelegationGroupView', () => ({ DelegationGroupView: () => null }))
vi.mock('./chat/ToolCallSegment', () => ({ MessageImageGallery: () => null }))
vi.mock('@/components/ui/confirm-dialog', () => ({
  ConfirmDialog: ({
    open,
    onOpenChange,
    title,
    message,
    onConfirm,
    loading,
  }: {
    open: boolean
    onOpenChange: (open: boolean) => void
    title: string
    message: string
    onConfirm: () => void
    loading?: boolean
  }) =>
    open ? (
      <div role="dialog" aria-label={title}>
        <p>{message}</p>
        <button onClick={onConfirm} disabled={loading}>
          confirm dialog action
        </button>
        <button onClick={() => onOpenChange(false)} disabled={loading}>
          cancel dialog action
        </button>
      </div>
    ) : null,
}))

function message(role: ChatMessage['role'], text: string): ChatMessage {
  return {
    id: `${role}-1`,
    role,
    segments: [{ type: 'content', text }],
    timestamp: '2026-08-31T10:00:00Z',
  }
}

describe('ChatMessageView copy', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined })
  })

  it.each([
    ['user', 'Copy message', 'exact user text'],
    ['assistant', 'Copy response', 'exact assistant response'],
  ] as const)(
    'copies %s text through the DOM fallback when Clipboard API is unavailable',
    async (role, title, text) => {
      const copiedValues: string[] = []
      Object.defineProperty(document, 'execCommand', {
        configurable: true,
        value: vi.fn(() => {
          copiedValues.push((document.activeElement as HTMLTextAreaElement).value)
          return true
        }),
      })

      render(<ChatMessageView message={message(role, text)} />)
      fireEvent.click(screen.getByTitle(title))

      await waitFor(() => expect(mocks.toastSuccess).toHaveBeenCalledWith('common.copiedSuccess'))
      expect(copiedValues).toEqual([text])
      expect(mocks.toastError).not.toHaveBeenCalled()
    }
  )

  it('uses the DOM fallback when Clipboard API rejects', async () => {
    const writeText = vi.fn().mockRejectedValue(new Error('denied'))
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    })
    const execCommand = vi.fn(() => true)
    Object.defineProperty(document, 'execCommand', { configurable: true, value: execCommand })

    render(<ChatMessageView message={message('assistant', 'fallback response')} />)
    fireEvent.click(screen.getByTitle('Copy response'))

    await waitFor(() => expect(mocks.toastSuccess).toHaveBeenCalledWith('common.copiedSuccess'))
    expect(writeText).toHaveBeenCalledWith('fallback response')
    expect(execCommand).toHaveBeenCalledWith('copy')
  })

  it('shows failure feedback when both clipboard mechanisms fail', async () => {
    Object.defineProperty(document, 'execCommand', {
      configurable: true,
      value: vi.fn(() => false),
    })

    render(<ChatMessageView message={message('user', 'uncopied text')} />)
    fireEvent.click(screen.getByTitle('Copy message'))

    await waitFor(() => expect(mocks.toastError).toHaveBeenCalledWith('common.failedToCopy'))
    expect(mocks.toastSuccess).not.toHaveBeenCalled()
  })
})

describe('ChatMessageView request-owned streams', () => {
  it('shows loading for a running empty assistant bubble', () => {
    render(
      <ChatMessageView
        message={{
          id: 'out:request-active:0',
          requestId: 'request-active',
          role: 'assistant',
          segments: [],
          timestamp: '2026-08-31T10:00:00Z',
          running: true,
        }}
        sessionId="session-1"
      />
    )

    expect(screen.getByText('loading')).toBeInTheDocument()
  })

  it('keeps an empty assistant bubble of a finished request hidden', () => {
    const { container } = render(
      <ChatMessageView
        message={{
          id: 'out:done:0',
          requestId: 'done',
          role: 'assistant',
          segments: [],
          timestamp: '2026-08-31T10:00:00Z',
        }}
        sessionId="session-1"
      />
    )

    expect(container).toBeEmptyDOMElement()
  })

  it('passes the bubble request identity to its segments', () => {
    render(
      <ChatMessageView
        message={{ ...message('assistant', 'channel delegation'), id: 'out:channel-run-id:0', requestId: 'channel-run-id' }}
        sessionId="session-1"
      />
    )

    expect(screen.getByTestId('segment-channel delegation')).toHaveAttribute('data-request-id', 'channel-run-id')
  })

  it('never infers a request from a message ID', () => {
    render(
      <ChatMessageView
        message={{ ...message('assistant', 'prefixed delegation'), id: 'msg-request-active' }}
        sessionId="session-1"
      />
    )

    expect(screen.getByTestId('segment-prefixed delegation')).toHaveAttribute('data-request-id', '')
  })

  it('labels a reply that continues after later messages', () => {
    render(
      <ChatMessageView
        message={{ ...message('assistant', 'delegation result'), requestId: 'r', continuation: true }}
        sessionId="session-1"
      />
    )

    expect(screen.getByText('chat.continuedReply')).toBeInTheDocument()
  })
})

describe('ChatMessageView compaction events', () => {
  it('renders a compaction result as a system row without avatar or role label', () => {
    render(
      <ChatMessageView
        message={{
          id: 'compact-1',
          role: 'assistant',
          requestId: 'req-compact',
          segments: [{ type: 'compact', text: 'compacted summary' }],
          timestamp: '2026-08-31T10:00:00Z',
        }}
        sessionId="session-1"
      />
    )

    expect(screen.getByTestId('segment-compacted summary')).toBeInTheDocument()
    expect(screen.queryByText('Assistant')).not.toBeInTheDocument()
  })
})

describe('ChatMessageView destructive actions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.rewindSession.mockResolvedValue(undefined)
    mocks.deleteSessionMessages.mockResolvedValue(undefined)
  })

  it.each([
    {
      actionTitle: 'chat.rewindAndEdit',
      dialogTitle: 'chat.rewindAndEdit',
      mutation: mocks.rewindSession,
      expectedArgs: ['session-1', '2026-08-31T10:00:00Z'],
    },
    {
      actionTitle: 'chat.deleteMessage',
      dialogTitle: 'chat.deleteMessage',
      mutation: mocks.deleteSessionMessages,
      expectedArgs: ['session-1', ['2026-08-31T10:00:00Z']],
    },
  ])(
    'uses a custom confirmation for $actionTitle and keeps cancel non-destructive',
    async ({ actionTitle, dialogTitle, mutation, expectedArgs }) => {
      const nativeConfirm = vi.spyOn(window, 'confirm').mockImplementation(() => {
        throw new Error('native confirm must not be called')
      })
      render(<ChatMessageView message={message('user', 'editable user text')} />)

      fireEvent.click(screen.getByTitle(actionTitle))
      expect(screen.getByRole('dialog', { name: dialogTitle })).toBeInTheDocument()
      fireEvent.click(screen.getByRole('button', { name: 'cancel dialog action' }))
      expect(mutation).not.toHaveBeenCalled()

      fireEvent.click(screen.getByTitle(actionTitle))
      fireEvent.click(screen.getByRole('button', { name: 'confirm dialog action' }))

      await waitFor(() => expect(mutation).toHaveBeenCalledWith(...expectedArgs))
      expect(nativeConfirm).not.toHaveBeenCalled()
    }
  )

  it('hands the rewound message and its attachments to the composer', async () => {
    const fill = vi.fn()
    window.addEventListener('fill-chat-input', fill)
    const files = [{ name: 'image.png', path: '/tmp/downloads/image.png' }]
    render(<ChatMessageView message={{ ...message('user', '为什么'), files }} />)

    fireEvent.click(screen.getByTitle('chat.rewindAndEdit'))
    fireEvent.click(screen.getByRole('button', { name: 'confirm dialog action' }))

    await waitFor(() => expect(fill).toHaveBeenCalled())
    const detail = (fill.mock.calls[0][0] as CustomEvent).detail
    expect(detail).toEqual({ text: '为什么', files })
    window.removeEventListener('fill-chat-input', fill)
  })
})
