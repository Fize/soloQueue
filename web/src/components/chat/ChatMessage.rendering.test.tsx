import chineseSamples from '@/testdata/chinese_samples.json'
import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { ChatMessage, RuntimeStatus } from '@/types'
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
function assistant(id: string, segments: ChatMessage['segments'] = [thinking], running = false): ChatMessage {
  return { id: `out:${id}:0`, requestId: id, role: 'assistant', segments, timestamp: '', running }
}
function worked(container: HTMLElement) { return container.querySelector('[data-worked]') as HTMLDetailsElement }

beforeEach(() => {
  state.activeSessionId = 'other-session'
  state.activeRequests = {}
  state.routeSessions = {}
  state.streamingSessions = {}
  state.status = null
})

describe('transcript-owned worked lifecycle', () => {
  it('opens the last worked group only while its request runs', () => {
    const { container, rerender } = render(<ChatMessageView message={assistant('live', [thinking], true)} sessionId="l1" />)
    expect(worked(container).open).toBe(true)
    rerender(<ChatMessageView message={assistant('live', [thinking], false)} sessionId="l1" />)
    expect(worked(container).open).toBe(false)
  })

  it('does not reopen a finished bubble because the page is streaming another request', () => {
    const { container } = render(<ChatMessageView message={assistant('finished')} sessionId="l1" isStreaming />)
    expect(worked(container).open).toBe(false)
  })
})

it('renders the initial empty thinking as worked and preserves its manual state when work arrives', () => {
  const emptyThinking: ChatMessage['segments'][number] = { type: 'thinking', text: '' }
  const { container, rerender } = render(<ChatMessageView message={assistant('empty-thinking', [emptyThinking], true)} sessionId="l1" />)
  expect(container.querySelectorAll('[data-worked]')).toHaveLength(1)
  expect(worked(container).open).toBe(true)
  expect(groupSegments([emptyThinking])).toMatchObject([
    { type: 'worked', id: 'worked-0', segments: [{ originalIndex: 0 }] },
  ])
  fireEvent.click(screen.getByText('worked'))
  expect(worked(container).open).toBe(false)
  rerender(<ChatMessageView message={assistant('empty-thinking', [emptyThinking, { type: 'tool_call', name: 'read_file', callId: 'empty-tool', args: '{}', done: false }], true)} sessionId="l1" />)
  expect(container.querySelectorAll('[data-worked]')).toHaveLength(1)
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
  expect(container.querySelectorAll('[data-worked]')).toHaveLength(2)
  const firstAnswer = screen.getByText('First answer.')
  const secondWorked = container.querySelectorAll('[data-worked]')[1]
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

it('keeps adjacent content separate and merges adjacent delegation segments', () => {
  const segments: ChatMessage['segments'] = [
    { type: 'content', text: 'A' },
    { type: 'content', text: 'B' },
    { type: 'tool_call', name: 'delegate', callId: 'delegate-a', args: '{}', done: true },
    { type: 'tool_call', name: 'delegate', callId: 'delegate-b', args: '{}', done: false },
  ]
  const groups = groupSegments(segments)

  expect(groups.map((group) => group.type)).toEqual([
    'other', 'other', 'delegation_group',
  ])
  expect(groups.map((group) => group.type === 'other' ? group.index : group.id)).toEqual([
    0, 1, 'delegation-2',
  ])
  expect(groups[2]).toMatchObject({
    type: 'delegation_group',
    segments: [{ originalIndex: 2 }, { originalIndex: 3 }],
  })

  render(<ChatMessageView message={assistant('adjacent-delegations', segments)} sessionId="l1" />)
  expect(screen.getByRole('button', { name: 'Delegated 2 tasks...' })).toBeInTheDocument()
})

it.each([
  ['thinking', { type: 'thinking', text: 'Checking results' }],
  ['ordinary tool call', { type: 'tool_call', name: 'read_file', callId: 'read', args: '{}', done: true }],
] satisfies [string, ChatMessage['segments'][number]][])('keeps delegation groups separate across %s', (_label, boundary) => {
  const groups = groupSegments([
    { type: 'delegation', agentName: 'first', task: 'one', status: 'completed' },
    boundary,
    { type: 'delegation', agentName: 'second', task: 'two', status: 'completed' },
  ])

  expect(groups.map((group) => group.type)).toEqual([
    'delegation_group', 'worked', 'delegation_group',
  ])
  expect(groups[0]).toMatchObject({ segments: [{ originalIndex: 0 }] })
  expect(groups[2]).toMatchObject({ segments: [{ originalIndex: 2 }] })
})

it('renders work received after formal content below that content and keeps it open while running', () => {
  const { container } = render(<ChatMessageView message={assistant('post-content-work', [
    { type: 'content', text: chineseSamples.ChatMessage_rendering_test_001 },
    { type: 'thinking', text: chineseSamples.ChatMessage_rendering_test_002 },
    { type: 'tool_call', name: 'read_file', callId: 'after-content', args: '{}', done: false },
  ], true)} sessionId="l1" />)

  const answer = screen.getByText(chineseSamples.ChatMessage_rendering_test_001)
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
  it('keeps the request-owned worked block open across content and closes when the request ends', () => {
    const { container, rerender } = render(<ChatMessageView message={assistant('live-l1', [thinking], true)} sessionId="l1" />)
    expect(worked(container).open).toBe(true)
    rerender(<ChatMessageView message={assistant('live-l1', [thinking, { type: 'content', text: 'Answer' }], true)} sessionId="l1" />)
    expect(worked(container).open).toBe(false)
    rerender(<ChatMessageView message={assistant('historical')} sessionId="l1" isStreaming />)
    expect(worked(container).open).toBe(false)
    rerender(<ChatMessageView message={assistant('live-l1')} sessionId="l1" />)
    expect(worked(container).open).toBe(false)
  })
})

it('retains manual close through group growth, completion and session remount, without affecting another message', () => {
  const { container, rerender, unmount } = render(<ChatMessageView message={assistant('manual-close', [thinking], true)} sessionId="l1" />)
  fireEvent.click(screen.getByText('worked'))
  expect(worked(container).open).toBe(false)
  const expanded = assistant('manual-close', [thinking, { type: 'tool_call', name: 'read_file', callId: 'tool-1', args: '{}', done: false }], true)
  rerender(<ChatMessageView message={expanded} sessionId="l1" />)
  expect(worked(container).open).toBe(false)
  unmount()
  const remounted = render(<ChatMessageView message={expanded} sessionId="l1" />)
  expect(worked(remounted.container).open).toBe(false)
  remounted.rerender(<ChatMessageView message={assistant('manual-close')} sessionId="another-session" />)
  expect(worked(remounted.container).open).toBe(false)
  remounted.rerender(<ChatMessageView message={assistant('untouched', [thinking], true)} sessionId="l1" />)
  expect(worked(remounted.container).open).toBe(true)
})

it('retains manual open after a running group and request complete', () => {
  const { container, rerender } = render(<ChatMessageView message={assistant('manual-open', [thinking], true)} sessionId="l1" />)
  fireEvent.click(screen.getByText('worked'))
  fireEvent.click(screen.getByText('worked'))
  const finished = assistant('manual-open', [thinking, { type: 'content', text: 'Answer' }], true)
  rerender(<ChatMessageView message={finished} sessionId="l1" />)
  expect(worked(container).open).toBe(true)
  rerender(<ChatMessageView message={{ ...finished, running: false }} sessionId="l1" />)
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
  const { container } = render(<ChatMessageView message={assistant('two-groups', [thinking, { type: 'content', text: 'Progress' }, thinking], true)} sessionId="session-2" isStreaming />)
  const groups = container.querySelectorAll('[data-worked]')
  expect(groups).toHaveLength(2)
  expect(groups[0].open).toBe(false)
  expect(groups[1].open).toBe(true)
  // The live batch stays nested inside the open worked group.
  const batches = groups[1].querySelectorAll('[data-thinking-batch]')
  expect(batches).toHaveLength(1)
  expect(batches[0].closest('[data-worked]')).toBe(groups[1])
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

it('retains manual state while a bubble grows under its stable transcript ID', () => {
  const { container, rerender } = render(<ChatMessageView message={assistant('growing', [thinking], true)} sessionId="l1" />)
  fireEvent.click(screen.getByText('worked'))
  expect(worked(container).open).toBe(false)
  rerender(<ChatMessageView message={assistant('growing', [thinking, { type: 'tool_call', name: 'read_file', callId: 'grow-call', args: '{}', done: true }], true)} sessionId="l1" />)
  expect(worked(container).open).toBe(false)
  rerender(<ChatMessageView message={assistant('growing', [thinking, { type: 'tool_call', name: 'read_file', callId: 'grow-call', args: '{}', done: true }])} sessionId="l1" />)
  expect(worked(container).open).toBe(false)
})

it('keeps manual state across remount for one session without leaking to another', () => {
  const first = render(<ChatMessageView message={assistant('navigation')} sessionId="navigation-own" />)
  fireEvent.click(screen.getByText('worked'))
  expect(worked(first.container).open).toBe(true)
  first.unmount()
  const other = render(<ChatMessageView message={assistant('navigation')} sessionId="navigation-other" />)
  expect(worked(other.container).open).toBe(false)
  other.unmount()
  const again = render(<ChatMessageView message={assistant('navigation')} sessionId="navigation-own" />)
  expect(worked(again.container).open).toBe(true)
})
