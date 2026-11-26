import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { DelegationCard } from './DelegationCard'
import type { AgentStreamState } from '@/types'

const mocks = vi.hoisted(() => ({
  t: (key: string) => key,
  stream: null as AgentStreamState | null,
}))

vi.mock('@/lib/i18n', () => ({ useTranslation: () => ({ t: mocks.t }) }))
vi.mock('@/hooks/useAgentStream', () => ({ useAgentStream: () => mocks.stream }))
vi.mock('@/hooks/useDelegationIdentity', () => ({
  useDelegationIdentity: () => ({ dispatchId: 'dlg_1', instanceId: 'inst_1' }),
}))
vi.mock('@/stores/chatStore', () => ({
  useChatStore: (
    select: (state: { activeSessionId: string; activeRequests: Record<string, never> }) => unknown
  ) => select({ activeSessionId: 'l1', activeRequests: {} }),
}))
vi.mock('@/components/AgentStreamView', () => ({
  AgentStreamView: ({ state }: { state: AgentStreamState }) => (
    <div data-testid="stream-view">
      {state.segments.map((segment, index) => (
        <p key={index}>
          {segment.type}:{segment.type === 'tool_call' ? segment.name : segment.text}
        </p>
      ))}
    </div>
  ),
}))
vi.mock('@/components/ui/markdown-preview', () => ({
  MarkdownPreview: ({ content }: { content: string }) => <div>{content}</div>,
}))
vi.mock('./observability/DelegationHistory', () => ({
  DelegationHistory: () => <div data-testid="recorded-history" />,
}))

const card = () => (
  <DelegationCard
    name="delegate_editor"
    args={JSON.stringify({ target: 'editor', task: 'Polish the dialog' })}
    callId="call_1"
    done
    result="Final conclusion"
  />
)

it('keeps the finished delegation process visible instead of collapsing to the result', () => {
  mocks.stream = {
    agent_id: 'inst_1',
    request_id: 'req_1',
    dispatch_id: 'dlg_1',
    processing: false,
    iteration: 2,
    segments: [
      { type: 'thinking', text: 'weighing options' },
      {
        type: 'tool_call',
        call_id: 'c1',
        name: 'read_file',
        args: '{}',
        result: 'ok',
        error: '',
        done: true,
        duration_ms: 12,
      },
      { type: 'content', text: 'Final conclusion' },
    ],
  }
  render(card())
  fireEvent.click(screen.getByRole('button', { name: /editor/ }))

  expect(screen.getByText('thinking:weighing options')).toBeInTheDocument()
  expect(screen.getByText('tool_call:read_file')).toBeInTheDocument()
  expect(screen.getByText('content:Final conclusion')).toBeInTheDocument()
  expect(screen.queryByTestId('recorded-history')).not.toBeInTheDocument()
})

it('pins the task description toggle above the scrolling process body', () => {
  mocks.stream = null
  render(card())
  fireEvent.click(screen.getByRole('button', { name: /editor/ }))

  const toggle = screen.getByText('observability.taskDescription')
  expect(screen.getByTestId('recorded-history').parentElement?.contains(toggle)).toBe(false)
})
