import { createRef } from 'react'
import { render, screen, waitFor } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { DelegationHistory } from './DelegationHistory'
const api = vi.hoisted(() => ({ request: vi.fn(), dispatch: vi.fn(), t: (key: string) => key }))
vi.mock('@/lib/api/observability-api', () => ({ getObservedRequest: api.request, getObservedDispatch: api.dispatch }))
vi.mock('@/lib/i18n', () => ({ useTranslation: () => ({ t: api.t }) }))
vi.mock('@/components/AgentStreamView', () => ({ AgentStreamView: ({ state }: { state: { segments: { type: string; text?: string }[] } }) => <div>{state.segments.map((s, i) => <p key={i}>{s.type}: {s.text}</p>)}</div> }))
it('restores only the selected delegation across pages and keeps the final answer once', async () => {
  api.request.mockReset().mockResolvedValueOnce({ dispatches: [{ dispatch_id: 'dlg_1', tool_call_id: 'tool1' }], calls: [{ dispatch_id: 'dlg_1', call_id: 'a', started_at: '2026-01-01T00:00:00Z', reasoning: 'thinking', recording_status: 'complete' }, { dispatch_id: 'dlg_other', started_at: '2026-01-01T00:00:00Z', content: 'wrong', recording_status: 'complete' }], recording_status: 'complete', next_cursor: '50' }).mockResolvedValueOnce({ dispatches: [], calls: [{ dispatch_id: 'dlg_1', call_id: 'b', started_at: '2026-01-01T00:00:01Z', content: 'answer', recording_status: 'complete' }], recording_status: 'complete' })
  api.dispatch.mockResolvedValue({ events: [] })
  render(<DelegationHistory sessionId="l1" requestId="req1" toolCallId="tool1" result="answer" scrollRef={createRef<HTMLDivElement>()} />)
  expect(await screen.findByText('thinking: thinking')).toBeInTheDocument()
  expect(screen.getByText('content: answer')).toBeInTheDocument()
  expect(screen.queryByText('wrong')).not.toBeInTheDocument()
  expect(screen.queryByText('answer')).not.toBeInTheDocument()
  await waitFor(() => expect(api.request).toHaveBeenCalledTimes(2))
})
