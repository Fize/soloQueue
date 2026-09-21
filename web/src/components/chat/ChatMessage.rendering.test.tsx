import { useWorkedStateKeys } from "@/hooks/useWorkedStateKeys"
import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { ChatMessage, RuntimeStatus, SessionRuntimeState } from '@/types'
import { ChatMessageView } from '../ChatMessage'
import { groupSegments } from './groupSegments'

const state = vi.hoisted(() => ({
  activeSessionId: 'other-session',
  activeRequests: {} as Record<string, { sessionId: string; status: string }>,
  routeSessions: {} as Record<string, { requestId: string; agentInstanceId?: string }>,
  streamingSessions: {} as Record<string, boolean>,
  status: null as Pick<RuntimeStatus, 'sessions' | 'agent_streams'> | null,
}))
vi.mock('@/stores/chatStore', () => ({ useChatStore: (selector: (value: typeof state) => unknown) => selector(state) }))
vi.mock('@/stores/runtimeStore', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/stores/runtimeStore')>()
  return {
    ...actual,
    useRuntimeStore: (selector: (value: { isDesignMode: boolean; status: typeof state.status }) => unknown) => selector({ isDesignMode: false, status: state.status }),
  }
})
vi.mock('@/lib/i18n', () => ({ useTranslation: () => ({ t: (key: string) => key }) }))
vi.mock('@/components/ui/streamdown-preview', () => ({ StreamdownPreview: ({ content }: { content: string }) => <div>{content}</div> }))
vi.mock('@/components/AgentStreamView', () => ({ AgentStreamView: () => null }))
vi.mock('@/components/DelegationCard', () => ({
  DelegationCard: ({ requestId, agentInstanceId }: { requestId?: string; agentInstanceId?: string }) => (
    <div
      data-testid="history-delegation-card"
      data-request-id={requestId ?? ''}
      data-agent-instance-id={agentInstanceId ?? ''}
    />
  ),
}))
vi.mock('./ToolCallSegment', () => ({ ToolCallSegment: () => <div>tool step</div>, MessageImageGallery: () => null }))

const thinking: ChatMessage['segments'][number] = { type: 'thinking', text: 'Inspecting the request' }
function assistant(id: string, segments: ChatMessage['segments'] = [thinking]): ChatMessage {
  return { id: `msg-${id}`, role: 'assistant', segments }
}
function worked(container: HTMLElement) { return container.querySelector('details') as HTMLDetailsElement }

beforeEach(() => {
  state.activeSessionId = 'other-session'
  state.activeRequests = {}
  state.routeSessions = {}
  state.streamingSessions = {}
  state.status = null
})

function runtimeRequest(requestId: string, sessionId = 'qq-session', status: SessionRuntimeState['state'] = 'streaming'): SessionRuntimeState {
  return { session_id: sessionId, request_id: requestId, state: status, revision: 1, ctxwin_used: 0, ctxwin_limit: 0, delegating: status === 'delegating' }
}

function recoveredAssistant(requestId: string): ChatMessage & { requestId: string } {
  return { ...assistant(requestId), requestId }
}

describe('runtime-owned worked lifecycle', () => {
  it('recovers request and delegated instance ownership for a historical running tool call', () => {
    state.status = {
      agent_streams: {
        'session-agent': {
          agent_id: 'session-agent',
          request_id: 'channel-live',
          processing: true,
          segments: [{
            type: 'tool_call',
            call_id: 'delegate-live',
            name: 'delegate',
            args: '{"target":"research"}',
            agent_instance_id: 'peer-instance',
            result: '',
            error: '',
            done: false,
            duration_ms: 0,
          }],
          iteration: 4,
        },
      },
    }
    render(
      <ChatMessageView
        message={assistant('historical-delegate', [{
          type: 'tool_call',
          callId: 'delegate-live',
          name: 'delegate',
          args: '{"target":"research"}',
          done: false,
        }])}
        sessionId="l1"
        sessionAgentId="session-agent"
      />,
    )

    expect(screen.getByTestId('history-delegation-card')).toHaveAttribute('data-request-id', 'channel-live')
    expect(screen.getByTestId('history-delegation-card')).toHaveAttribute('data-agent-instance-id', 'peer-instance')
  })

  it.each(['l1', 'l2:qq-account', 'l2:wechat-account', 'l2:telegram-account'])('uses the page-owned agent stream when %s has no route or local request', (sessionId) => {
    const requestId = `channel-${sessionId}`
    state.status = { sessions: { [sessionId]: { ...runtimeRequest('', sessionId, 'idle'), revision: 0 } }, agent_streams: {
      own: { agent_id: 'session-agent', request_id: requestId, processing: true, segments: [], iteration: 1 },
    } }
    const { container, rerender } = render(<ChatMessageView message={recoveredAssistant(requestId)} sessionId={sessionId} sessionAgentId="session-agent" />)
    expect(worked(container).open).toBe(true)
    // A subsequent route selection cannot move this request to another message.
    state.routeSessions[sessionId] = { requestId: 'another-request', agentInstanceId: 'other-agent' }
    rerender(<ChatMessageView message={recoveredAssistant(requestId)} sessionId={sessionId} sessionAgentId="session-agent" />)
    expect(worked(container).open).toBe(true)
    state.status.agent_streams!.own.processing = false
    state.activeRequests[requestId] = { sessionId, status: 'streaming' }
    rerender(<ChatMessageView message={recoveredAssistant(requestId)} sessionId={sessionId} sessionAgentId="session-agent" />)
    expect(worked(container).open).toBe(false)
  })

  it('rechecks stable recovered messages when the session agent becomes available', () => {
    const message = recoveredAssistant('late-agent')
    state.status = { agent_streams: { own: { agent_id: 'late-instance', request_id: 'late-agent', processing: true, segments: [], iteration: 1 } } }
    const { container, rerender } = render(<ChatMessageView message={message} sessionId="l1" />)
    expect(worked(container).open).toBe(false)
    rerender(<ChatMessageView message={message} sessionId="l1" sessionAgentId="late-instance" />)
    expect(worked(container).open).toBe(true)
    rerender(<ChatMessageView message={message} sessionId="l1" sessionAgentId="replacement-instance" />)
    expect(worked(container).open).toBe(false)
  })

  it('requires explicit request identity and rejects another session agent without a route', () => {
    state.status = { agent_streams: { own: { agent_id: 'own-agent', request_id: 'channel-isolation', processing: true, segments: [], iteration: 1 } } }
    const { container, rerender } = render(<ChatMessageView message={assistant('channel-isolation')} sessionId="l1" sessionAgentId="own-agent" />)
    expect(worked(container).open).toBe(false)
    rerender(<ChatMessageView message={recoveredAssistant('channel-isolation')} sessionId="l2:other" sessionAgentId="other-agent" />)
    expect(worked(container).open).toBe(false)
  })

  it.each(['starting', 'streaming', 'delegating', 'cancelling'] as const)('opens a recovered QQ request during %s without a local request record', (status) => {
    const requestId = `qq-${status}`
    state.status = { sessions: { [requestId]: runtimeRequest(requestId, 'qq-session', status) }, agent_streams: {} }
    const { container } = render(<ChatMessageView message={recoveredAssistant(requestId)} sessionId="qq-session" />)
    expect(worked(container).open).toBe(true)
  })

  it.each(['idle', 'error'] as const)('closes on runtime %s even when a cached processing stream remains', (status) => {
    const requestId = `qq-terminal-${status}`
    state.routeSessions['qq-session'] = { requestId, agentInstanceId: 'qq-agent' }
    state.status = {
      sessions: { [requestId]: runtimeRequest(requestId) },
      agent_streams: { [requestId]: { agent_id: 'qq-agent', request_id: requestId, processing: true, segments: [], iteration: 1 } },
    }
    const { container, rerender } = render(<ChatMessageView message={recoveredAssistant(requestId)} sessionId="qq-session" />)
    expect(worked(container).open).toBe(true)
    state.status.sessions![requestId] = runtimeRequest(requestId, 'qq-session', status)
    rerender(<ChatMessageView message={recoveredAssistant(requestId)} sessionId="qq-session" />)
    expect(worked(container).open).toBe(false)
  })

  it('checks each concurrent request and logical session instead of the selected route or global streaming flag', () => {
    state.routeSessions['qq-session'] = { requestId: 'concurrent-new' }
    state.status = { sessions: {
      first: runtimeRequest('concurrent-old', 'qq-session', 'idle'),
      second: runtimeRequest('concurrent-new'),
      third: runtimeRequest('other-session-request', 'another-session'),
      fourth: runtimeRequest('concurrent-running'),
    }, agent_streams: {} }
    const { container, rerender } = render(<ChatMessageView message={recoveredAssistant('concurrent-running')} sessionId="qq-session" />)
    expect(worked(container).open).toBe(true)
    for (const requestId of ['concurrent-old', 'other-session-request', 'historical-runtime']) {
      rerender(<ChatMessageView message={recoveredAssistant(requestId)} sessionId="qq-session" isStreaming />)
      expect(worked(container).open).toBe(false)
    }
  })

  it('opens a processing stream only when the route proves exact agent, request and session ownership', () => {
    state.routeSessions['qq-session'] = { requestId: 'stream-only', agentInstanceId: 'qq-agent' }
    state.status = { agent_streams: { stream: { agent_id: 'qq-agent', request_id: 'stream-only', processing: true, segments: [], iteration: 1 } } }
    const { container, rerender } = render(<ChatMessageView message={recoveredAssistant('stream-only')} sessionId="qq-session" />)
    expect(worked(container).open).toBe(true)
    state.status.agent_streams.stream.request_id = 'stale-request'
    rerender(<ChatMessageView message={recoveredAssistant('stream-only')} sessionId="qq-session" />)
    expect(worked(container).open).toBe(false)
    state.status.agent_streams.stream.request_id = 'stream-only'
    state.status.agent_streams.stream.agent_id = 'another-agent'
    rerender(<ChatMessageView message={recoveredAssistant('stream-only')} sessionId="qq-session" />)
    expect(worked(container).open).toBe(false)
    state.status.agent_streams.stream.agent_id = 'qq-agent'
    rerender(<ChatMessageView message={recoveredAssistant('stream-only')} sessionId="another-session" />)
    expect(worked(container).open).toBe(false)
  })

  it('keeps a queued local request closed despite runtime processing and respects manual close/open through completion', () => {
    const requestId = 'runtime-manual'
    state.activeRequests[requestId] = { sessionId: 'qq-session', status: 'queued' }
    state.status = { sessions: { [requestId]: runtimeRequest(requestId) }, agent_streams: {} }
    const { container, rerender } = render(<ChatMessageView message={recoveredAssistant(requestId)} sessionId="qq-session" />)
    expect(worked(container).open).toBe(false)
    state.activeRequests = {}
    rerender(<ChatMessageView message={recoveredAssistant(requestId)} sessionId="qq-session" />)
    expect(worked(container).open).toBe(true)
    fireEvent.click(screen.getByText('worked'))
    rerender(<ChatMessageView message={recoveredAssistant(requestId)} sessionId="qq-session" />)
    expect(worked(container).open).toBe(false)
    fireEvent.click(screen.getByText('worked'))
    state.status.sessions![requestId] = runtimeRequest(requestId, 'qq-session', 'idle')
    rerender(<ChatMessageView message={recoveredAssistant(requestId)} sessionId="qq-session" />)
    expect(worked(container).open).toBe(true)
  })
})

it('renders the initial empty thinking as worked and preserves its manual state when work arrives', () => {
  state.activeRequests['empty-thinking'] = { sessionId: 'l1', status: 'streaming' }
  const emptyThinking: ChatMessage['segments'][number] = { type: 'thinking', text: '' }
  const { container, rerender } = render(<ChatMessageView message={assistant('empty-thinking', [emptyThinking])} sessionId="l1" />)
  expect(container.querySelectorAll('details')).toHaveLength(1)
  expect(worked(container).open).toBe(true)
  expect(groupSegments([emptyThinking])).toMatchObject([
    { type: 'worked', id: 'worked-0', segments: [{ originalIndex: 0 }] },
  ])
  fireEvent.click(screen.getByText('worked'))
  expect(worked(container).open).toBe(false)
  rerender(<ChatMessageView message={assistant('empty-thinking', [emptyThinking, { type: 'tool_call', name: 'read_file', callId: 'empty-tool', args: '{}', done: false }])} sessionId="l1" />)
  expect(container.querySelectorAll('details')).toHaveLength(1)
  expect(worked(container).open).toBe(false)
  expect(groupSegments([emptyThinking, { type: 'tool_call', name: 'read_file', callId: 'empty-tool', args: '{}', done: false }])).toMatchObject([
    { type: 'worked', id: 'worked-0', segments: [{ originalIndex: 1 }] },
  ])
})

it('hides an empty thinking placeholder when the turn contains only content', () => {
  const groups = groupSegments([
    { type: 'thinking', text: '' },
    { type: 'content', text: 'Answer' },
  ])
  expect(groups).toEqual([
    { type: 'other', segment: { type: 'content', text: 'Answer' }, index: 1 },
  ])
})

it.each([
  { type: 'content', text: 'Answer' },
  { type: 'tool_call', name: 'delegate', callId: 'delegate-empty', args: '{}', done: false },
] satisfies ChatMessage['segments'])('anchors later work to the initial empty thinking across $type', (segment) => {
  const groups = groupSegments([{ type: 'thinking', text: '' }, segment, thinking])
  expect(groups).toHaveLength(2)
  expect(groups.find((group) => group.type === 'worked')).toMatchObject({
    type: 'worked',
    id: 'worked-0',
    segments: [{ originalIndex: 2 }],
  })
})

it('keeps interleaved work and content in arrival order inside one assistant message', () => {
  const segments = [
    { type: 'thinking', text: 'First thought' },
    { type: 'content', text: 'First answer. ' },
    { type: 'tool_call', name: 'read_file', callId: 'interleaved-tool', args: '{}', done: true },
    { type: 'thinking', text: 'Second thought' },
    { type: 'content', text: 'Second answer.' },
  ] satisfies ChatMessage['segments']

  const groups = groupSegments(segments)
  expect(groups).toHaveLength(4)
  expect(groups[0]).toMatchObject({
    type: 'worked',
    id: 'worked-0',
    isLast: false,
    segments: [{ originalIndex: 0 }],
  })
  expect(groups[1]).toMatchObject({
    type: 'other',
    index: 1,
    segment: { type: 'content', text: 'First answer. ' },
  })
  expect(groups[2]).toMatchObject({
    type: 'worked',
    id: 'worked-2',
    isLast: false,
    segments: [{ originalIndex: 2 }, { originalIndex: 3 }],
  })
  expect(groups[3]).toMatchObject({
    type: 'other',
    index: 4,
    segment: { type: 'content', text: 'Second answer.' },
  })

  const { container } = render(
    <ChatMessageView message={assistant('interleaved-stream', segments)} sessionId="l1" />,
  )
  expect(container.querySelectorAll('details')).toHaveLength(2)
  const firstAnswer = screen.getByText('First answer.')
  const secondWorked = container.querySelectorAll('details')[1]
  const secondAnswer = screen.getByText('Second answer.')
  expect(firstAnswer.compareDocumentPosition(secondWorked) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  expect(secondWorked.compareDocumentPosition(secondAnswer) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
})

it('keeps delegation segments on their original sides of intervening content', () => {
  const groups = groupSegments([
    { type: 'delegation', agentName: 'first', task: 'one', status: 'completed' },
    { type: 'content', text: 'Interim answer' },
    { type: 'tool_call', name: 'delegate', callId: 'second', args: '{}', done: true },
  ])

  expect(groups).toHaveLength(3)
  expect(groups[0]).toMatchObject({
    type: 'delegation_group',
    id: 'delegation-0',
    segments: [{ originalIndex: 0 }],
  })
  expect(groups[1]).toMatchObject({
    type: 'other',
    segment: { type: 'content', text: 'Interim answer' },
  })
  expect(groups[2]).toMatchObject({
    type: 'delegation_group',
    id: 'delegation-2',
    segments: [{ originalIndex: 2 }],
  })
})

it('does not merge adjacent content or delegation segments', () => {
  const groups = groupSegments([
    { type: 'content', text: 'A' },
    { type: 'content', text: 'B' },
    { type: 'tool_call', name: 'delegate', callId: 'delegate-a', args: '{}', done: true },
    { type: 'tool_call', name: 'delegate', callId: 'delegate-b', args: '{}', done: false },
  ])
  expect(groups.map((group) => group.type)).toEqual([
    'other', 'other', 'delegation_group', 'delegation_group',
  ])
  expect(groups.map((group) => group.type === 'other' ? group.index : group.id)).toEqual([
    0, 1, 'delegation-2', 'delegation-3',
  ])
})

it('renders work received after formal content below that content and keeps it open while running', () => {
  state.activeRequests['post-content-work'] = { sessionId: 'l1', status: 'streaming' }
  const { container } = render(<ChatMessageView message={assistant('post-content-work', [
    { type: 'content', text: '先给正式回复' },
    { type: 'thinking', text: '随后继续核实' },
    { type: 'tool_call', name: 'read_file', callId: 'after-content', args: '{}', done: false },
  ])} sessionId="l1" />)

  const answer = screen.getByText('先给正式回复')
  const laterWorked = worked(container)
  expect(answer.compareDocumentPosition(laterWorked) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  expect(laterWorked.open).toBe(true)
})

describe('channel attachment presentation', () => {
  it('hides the generated upload annotation in mixed QQ text and image messages', () => {
    render(<ChatMessageView message={{ id: 'qq-image', role: 'user', segments: [{ type: 'content', text: 'Please inspect this\n[User uploaded images, processed by visual recognition]' }], files: [{ name: 'download', path: '/downloads/download' }] }} />)
    expect(screen.getByText('Please inspect this')).toBeInTheDocument()
    expect(screen.queryByText(/processed by visual recognition/)).not.toBeInTheDocument()
  })
})

it('previews extensionless historical image attachments after decoding and retains readable fallbacks', () => {
  const { container } = render(<ChatMessageView message={{ id: 'qq-probe', role: 'user', segments: [{ type: 'content', text: 'Screenshot' }], files: [{ name: 'download', path: '/downloads/download' }, { name: 'report.pdf', path: '/downloads/report.pdf' }] }} />)
  const image = container.querySelector('img')
  expect(image).not.toBeNull()
  fireEvent.load(image!)
  expect(screen.getByRole('img', { name: 'download' })).toBeVisible()
  expect(screen.getByRole('img', { name: 'download' }).closest('a')).toHaveClass('border-border')
  expect(screen.getByRole('link', { name: 'report.pdf' })).toHaveClass('text-foreground')
  fireEvent.error(image!)
  expect(screen.queryByRole('img')).not.toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'download' })).toHaveClass('text-foreground')
})

describe('worked block lifecycle', () => {
  it('keeps the request-owned worked block open across content and closes when ownership ends', () => {
    state.activeRequests['live-l1'] = { sessionId: 'l1', status: 'streaming' }
    const { container, rerender } = render(<ChatMessageView message={assistant('live-l1')} sessionId="l1" />)
    expect(worked(container).open).toBe(true)
    rerender(<ChatMessageView message={assistant('live-l1', [thinking, { type: 'content', text: 'Answer' }])} sessionId="l1" />)
    expect(worked(container).open).toBe(false)
    rerender(<ChatMessageView message={assistant('historical')} sessionId="l1" isStreaming />)
    expect(worked(container).open).toBe(false)
    state.activeRequests = {}
    rerender(<ChatMessageView message={assistant('live-l1')} sessionId="l1" />)
    expect(worked(container).open).toBe(false)
  })
})

it('retains manual close through group growth, completion and session remount, without affecting another message', () => {
  state.activeRequests['manual-close'] = { sessionId: 'l1', status: 'streaming' }
  const { container, rerender, unmount } = render(<ChatMessageView message={assistant('manual-close')} sessionId="l1" />)
  fireEvent.click(screen.getByText('worked'))
  expect(worked(container).open).toBe(false)
  const expanded = assistant('manual-close', [thinking, { type: 'tool_call', name: 'read_file', callId: 'tool-1', args: '{}', done: false }])
  rerender(<ChatMessageView message={expanded} sessionId="l1" />)
  expect(worked(container).open).toBe(false)
  unmount()
  const remounted = render(<ChatMessageView message={expanded} sessionId="l1" />)
  expect(worked(remounted.container).open).toBe(false)
  remounted.rerender(<ChatMessageView message={assistant('manual-close')} sessionId="another-session" />)
  expect(worked(remounted.container).open).toBe(false)
  state.activeRequests['untouched'] = { sessionId: 'l1', status: 'streaming' }
  remounted.rerender(<ChatMessageView message={assistant('untouched')} sessionId="l1" />)
  expect(worked(remounted.container).open).toBe(true)
})

it('retains manual open after a running group and request complete', () => {
  state.activeRequests['manual-open'] = { sessionId: 'l1', status: 'streaming' }
  const { container, rerender } = render(<ChatMessageView message={assistant('manual-open')} sessionId="l1" />)
  fireEvent.click(screen.getByText('worked'))
  fireEvent.click(screen.getByText('worked'))
  const finished = assistant('manual-open', [thinking, { type: 'content', text: 'Answer' }])
  rerender(<ChatMessageView message={finished} sessionId="l1" />)
  expect(worked(container).open).toBe(true)
  state.activeRequests = {}
  rerender(<ChatMessageView message={{ ...finished }} sessionId="l1" />)
  expect(worked(container).open).toBe(true)
})

it.each(['user', 'assistant'] as const)('preserves ordinary %s text that mentions upload annotations without an attached user file', (role) => {
  const text = 'Explain this marker: [User uploaded images, processed by visual recognition]'
  render(<ChatMessageView message={{ id: `literal-${role}`, role, segments: [{ type: 'content', text }], ...(role === 'assistant' ? { files: [{ name: 'notes.txt', path: '/notes.txt' }] } : {}) }} />)
  expect(screen.getByText(text)).toBeInTheDocument()
})

it('keeps only user content in mixed file/image prompt annotations and copied text', async () => {
  const writeText = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
  render(<ChatMessageView message={{ id: 'mixed-upload', role: 'user', segments: [{ type: 'content', text: 'Compare both\n\n[User uploaded files, saved locally:\n- Filename: notes.txt\n  Saved path: /notes.txt]\n\n[User uploaded images, processed by visual recognition]' }], files: [{ name: 'notes.txt', path: '/notes.txt' }] }} />)
  expect(screen.getByText('Compare both')).toBeInTheDocument()
  expect(screen.queryByText(/Saved path/)).not.toBeInTheDocument()
  fireEvent.click(screen.getByTitle('Copy message'))
  expect(writeText).toHaveBeenCalledWith('Compare both')
})

it('identifies images by saved path even when display name lacks the extension', () => {
  render(<ChatMessageView message={{ id: 'path-image', role: 'user', segments: [{ type: 'content', text: 'Look' }], files: [{ name: 'download', path: '/downloads/image.PNG' }] }} />)
  expect(screen.getByRole('img', { name: 'download' })).toBeVisible()
})

it('closes completed work before content and opens later work below it', () => {
  state.activeRequests['two-groups'] = { sessionId: 'session-2', status: 'streaming' }
  const { container } = render(<ChatMessageView message={assistant('two-groups', [thinking, { type: 'content', text: 'Progress' }, thinking])} sessionId="session-2" isStreaming />)
  const groups = container.querySelectorAll('details')
  expect(groups).toHaveLength(2)
  expect(groups[0].open).toBe(false)
  expect(groups[1].open).toBe(true)
})

it('keeps a queued request closed until execution starts', () => {
  state.activeRequests['queued-work'] = { sessionId: 'l1', status: 'queued' }
  const { container, rerender } = render(<ChatMessageView message={assistant('queued-work')} sessionId="l1" isStreaming />)
  expect(worked(container).open).toBe(false)
  state.activeRequests['queued-work'].status = 'streaming'
  rerender(<ChatMessageView message={assistant('queued-work')} sessionId="l1" />)
  expect(worked(container).open).toBe(true)
})

it.each([true, false])('retains manual state %s across server history IDs and split message groups', async (open) => {
  const { preserveWorkedStateKeys } = await import('./preserveWorkedStateKeys')
  const live = assistant(`hydrate-${open}`, [thinking, { type: 'tool_call', name: 'read_file', callId: `hydration-call-${open}`, args: '{}', done: false }])
  state.activeRequests[`hydrate-${open}`] = { sessionId: 'l1', status: 'streaming' }
  const { container, rerender } = render(<ChatMessageView message={live} sessionId="l1" />)
  fireEvent.click(screen.getByText('worked'))
  if (open) fireEvent.click(screen.getByText('worked'))
  expect(worked(container).open).toBe(open)
  const history: ChatMessage = { id: `hist-${open}`, role: 'assistant', timestamp: '2026-09-18T00:00:00Z', segments: [{ type: 'tool_call', name: 'read_file', callId: `hydration-call-${open}`, args: '{}', done: true }] }
  const hydrated = preserveWorkedStateKeys([live], [history])[0]
  expect(hydrated.id).toBe(history.id)
  state.activeRequests = {}
  rerender(<ChatMessageView message={hydrated} sessionId="l1" />)
  expect(worked(container).open).toBe(open)
})

it.each([false, true])('hides shared channel attachment annotations with image suffix %s while preserving user text', (withImage) => {
  const text = 'Please compare the attachments. Keep my [ordinary note] exactly.'
  const annotation = '\n\n[User uploaded attachments:\n- Filename: notes.txt\n  Saved path: /notes.txt\n]'
  const imageAnnotation = withImage ? '\n[User uploaded images, processed by visual recognition]' : ''
  render(<ChatMessageView message={{ id: `bridge-attachments-${withImage}`, role: 'user', segments: [{ type: 'content', text: text + annotation + imageAnnotation }], files: [{ name: 'notes.txt', path: '/notes.txt' }] }} />)
  expect(screen.getByText(text)).toBeInTheDocument()
  expect(screen.queryByText(/User uploaded attachments:/)).not.toBeInTheDocument()
  expect(screen.queryByText(/processed by visual recognition/)).not.toBeInTheDocument()
})

it('keeps an ordinary inline mention of shared attachment annotations', () => {
  const text = 'Explain [User uploaded attachments: manual example] in this note.'
  render(<ChatMessageView message={{ id: 'literal-attachments', role: 'user', segments: [{ type: 'content', text }], files: [{ name: 'notes.txt', path: '/notes.txt' }] }} />)
  expect(screen.getByText(text)).toBeInTheDocument()
})

it('retains a manual open choice when recovered output hydrates under a history ID', () => {
  const segments: ChatMessage['segments'] = [{ type: 'thinking', text: 'Hydrating exact request' }, { type: 'tool_call', callId: 'hydration-call', name: 'read_file', args: '{}', done: true }]
  const live = { id: 'msg-hydration', requestId: 'hydration', role: 'assistant' as const, segments }
  const history = { id: 'hist-hydration', role: 'assistant' as const, segments }
  function Harness({ messages }: { messages: ChatMessage[] }) {
    const rendered = useWorkedStateKeys("hydration-session", messages)
    return <ChatMessageView message={rendered[0]} sessionId="hydration-session" />
  }
  const { container, rerender } = render(<Harness messages={[live]} />)
  fireEvent.click(screen.getByText('worked'))
  expect(worked(container).open).toBe(true)
  rerender(<Harness messages={[history]} />)
  expect(worked(container).open).toBe(true)
})

it('keeps recovered manual state across navigation without leaking to another session', () => {
  const segments: ChatMessage['segments'] = [{ type: 'tool_call', callId: 'navigation-call', name: 'read_file', args: '{}', done: true }]
  function Harness({ session, id }: { session: string; id: string }) {
    const messages = useWorkedStateKeys(session, [{ id, role: 'assistant', segments }])
    return <ChatMessageView message={messages[0]} sessionId={session} />
  }
  const first = render(<Harness session="navigation-own" id="msg-navigation" />)
  fireEvent.click(screen.getByText('worked'))
  expect(worked(first.container).open).toBe(true)
  first.unmount()
  const second = render(<Harness session="navigation-other" id="hist-navigation" />)
  expect(worked(second.container).open).toBe(false)
  second.unmount()
  const third = render(<Harness session="navigation-own" id="hist-navigation" />)
  expect(worked(third.container).open).toBe(true)
})

it('keeps manual state through the gap between stream completion and async history hydration', () => {
  const segments: ChatMessage['segments'] = [{ type: 'tool_call', callId: 'async-history-call', name: 'read_file', args: '{}', done: true }]
  function Harness({ id }: { id?: string }) {
    const messages = useWorkedStateKeys('async-history-session', id ? [{ id, role: 'assistant', segments }] : [])
    return messages[0] ? <ChatMessageView message={messages[0]} sessionId="async-history-session" /> : null
  }
  const { container, rerender } = render(<Harness id="msg-async-history" />)
  fireEvent.click(screen.getByText('worked'))
  expect(worked(container).open).toBe(true)
  rerender(<Harness />)
  rerender(<Harness id="hist-async-history" />)
  expect(worked(container).open).toBe(true)
})

it('does not transfer a manual choice to a new live request with identical thinking', () => {
  function Harness({ id }: { id: string }) {
    const messages = useWorkedStateKeys('repeated-live', [{ id, role: 'assistant', segments: [thinking] }])
    return <ChatMessageView message={messages[0]} sessionId="repeated-live" />
  }
  const { container, rerender } = render(<Harness id="msg-first-thinking" />)
  fireEvent.click(screen.getByText('worked'))
  expect(worked(container).open).toBe(true)
  rerender(<Harness id="msg-second-thinking" />)
  expect(worked(container).open).toBe(false)
})

it('keeps an established history alias through repeated thinking, updates and navigation', () => {
  const session = 'established-alias'
  const live = assistant('established-a')
  const history = { ...live, id: 'hist-established-a', timestamp: '2026-09-18T01:00:00Z' }
  const second = assistant('established-b')
  function Harness({ messages, sessionId = session }: { messages: ChatMessage[]; sessionId?: string }) {
    const rendered = useWorkedStateKeys(sessionId, messages)
    return <>{rendered.map((message) => <ChatMessageView key={message.id} message={message} sessionId={sessionId} />)}</>
  }
  const first = render(<Harness messages={[live]} />)
  fireEvent.click(screen.getByText('worked'))
  first.rerender(<Harness messages={[history]} />)
  expect(worked(first.container).open).toBe(true)
  first.rerender(<Harness messages={[history, second]} />)
  expect([...first.container.querySelectorAll('details')].map((node) => node.open)).toEqual([true, false])
  first.rerender(<Harness messages={[{ ...history }, { ...second, segments: [thinking, { type: 'content', text: 'Done' }] }]} />)
  expect([...first.container.querySelectorAll('details')].map((node) => node.open)).toEqual([true, false])
  first.unmount()
  const navigated = render(<Harness messages={[history, second]} sessionId="established-other-session" />)
  expect([...navigated.container.querySelectorAll('details')].map((node) => node.open)).toEqual([false, false])
  navigated.rerender(<Harness messages={[history, second]} />)
  expect([...navigated.container.querySelectorAll('details')].map((node) => node.open)).toEqual([true, false])
  // A never-seen history row cannot inherit either identical live request.
  navigated.rerender(<Harness messages={[history, { ...second, id: 'hist-ambiguous-new' }]} />)
  expect([...navigated.container.querySelectorAll('details')].map((node) => node.open)).toEqual([true, false])
  // Server history IDs are positional: reuse after deletion must not reuse A's alias.
  navigated.rerender(<Harness messages={[{ ...history, timestamp: '2026-09-18T02:00:00Z' }]} />)
  expect(worked(navigated.container).open).toBe(false)
  navigated.rerender(<Harness messages={[{ ...history, segments: [{ type: 'thinking', text: 'Different work' }] }]} />)
  expect(worked(navigated.container).open).toBe(false)
})

it.each(['msg-recycled-source', 'hist-0'])('isolates recycled positional history IDs from %s manual choices', (sourceId) => {
  const session = `recycled-${sourceId}`
  function Harness({ id, timestamp }: { id: string; timestamp: string }) {
    const messages = useWorkedStateKeys(session, [{ id, timestamp, role: 'assistant', segments: [thinking] }])
    return <ChatMessageView message={messages[0]} sessionId={session} />
  }
  const { container, rerender } = render(<Harness id={sourceId} timestamp="2026-09-18T01:00:00Z" />)
  fireEvent.click(screen.getByText('worked'))
  expect(worked(container).open).toBe(true)
  rerender(<Harness id="hist-0" timestamp="2026-09-18T01:00:00Z" />)
  expect(worked(container).open).toBe(true)
  rerender(<Harness id="hist-0" timestamp="2026-09-18T02:00:00Z" />)
  expect(worked(container).open).toBe(false)
})
