import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import userEvent from '@testing-library/user-event'
import { ExecutionDetails } from './ExecutionDetails'
import { DelegationProcess } from './DelegationProcess'
import { DispatchProcess } from './DispatchProcess'
import type { ObservedRequest, ObservedCall } from '@/lib/api/observability-api'
const api = vi.hoisted(() => ({
  getObservedRequest: vi.fn(),
  getObservedPrompt: vi.fn(),
  getObservedDispatch: vi.fn(),
  t: (key: string) => key,
}))
vi.mock('@/lib/api/observability-api', async () => ({
  ...(await vi.importActual('@/lib/api/observability-api')),
  getObservedRequest: api.getObservedRequest,
  getObservedPrompt: api.getObservedPrompt,
  getObservedDispatch: api.getObservedDispatch,
}))
vi.mock('@/lib/i18n', () => ({ useTranslation: () => ({ t: api.t }) }))
const call: ObservedCall = {
  call_id: 'call1',
  request_id: 'req1',
  run_id: 'run1',
  agent_id: 'instance1',
  provider_id: 'provider',
  model_id: 'model',
  usage_type: 'chat',
  status: 'success',
  started_at: '2026-10-26T00:00:00Z',
  finished_at: '2026-10-26T00:00:01Z',
  duration_ms: 1000,
  attempt_count: 2,
  retry_count: 1,
  attempts_reported: true,
  prompt_tokens: 10,
  completion_tokens: 20,
  total_tokens: 30,
  prompt_available: true,
  recording_status: 'complete',
  content: 'Intermediate output',
}
const data: ObservedRequest = {
  session_id: 'l1',
  request_id: 'req1',
  status: 'completed',
  recording_status: 'complete',
  started_at: call.started_at,
  finished_at: call.finished_at,
  duration_ms: 3000,
  call_count: 1,
  attempt_count: 2,
  calls: [call],
  dispatches: [],
  errors: [],
}
beforeEach(() => {
  vi.resetAllMocks()
  api.getObservedRequest.mockResolvedValue(structuredClone(data))
  api.getObservedPrompt.mockResolvedValue({
    call_id: 'call1',
    recording_status: 'complete',
    representation: 'instructions',
    messages: [],
    instructions: '<script>private prompt</script>',
  })
  api.getObservedDispatch.mockResolvedValue({
    dispatch_id: 'dlg1',
    root_dispatch_id: 'dlg1',
    executor: 'Worker',
    executor_instance_id: 'instance1',
    status: 'completed',
    created_at: call.started_at,
    updated_at: call.finished_at,
    events: [
      {
        sequence: 1,
        timestamp: call.started_at,
        type: 'tool_exec_done',
        payload: { result: 'Persisted tool result' },
      },
    ],
  })
})
it('shows actual call count and timing, then escaped actual prompt on demand', async () => {
  render(<ExecutionDetails sessionId="l1" requestId="req1" />)
  expect(await screen.findByText('observability.calls 1')).toBeInTheDocument()
  expect(api.getObservedPrompt).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: /observability.title/ }))
  const dialog = await screen.findByRole('dialog', { name: 'observability.traceTitle' })
  fireEvent.click(within(dialog).getByText('provider / model'))
  fireEvent.click(within(dialog).getByRole('button', { name: 'observability.prompt' }))
  expect(await within(dialog).findByText('<script>private prompt</script>')).toBeInTheDocument()
  expect(document.querySelector('script')).toBeNull()
  expect(api.getObservedPrompt).toHaveBeenCalledWith('l1', 'req1', 'call1', expect.any(AbortSignal))
  expect(within(dialog).getByRole('link', { name: 'observability.settings' })).toHaveAttribute(
    'href',
    '#/settings/storage?category=execution_details'
  )
})
it('reports expired details without synthesizing a prompt', async () => {
  api.getObservedRequest.mockResolvedValue({
    ...data,
    recording_status: 'expired',
    calls: [{ ...call, recording_status: 'expired', prompt_available: false }],
  })
  api.getObservedPrompt.mockResolvedValue({
    call_id: 'call1',
    recording_status: 'expired',
    representation: 'messages',
    messages: [],
  })
  render(<ExecutionDetails sessionId="l1" requestId="req1" />)
  fireEvent.click(screen.getByRole('button', { name: /observability.title/ }))
  const dialog = await screen.findByRole('dialog')
  expect((await within(dialog).findAllByText('observability.expired')).length).toBeGreaterThan(0)
  fireEvent.click(within(dialog).getByText('provider / model'))
  expect(
    within(dialog).queryByRole('button', { name: 'observability.prompt' })
  ).not.toBeInTheDocument()
  expect(await within(dialog).findByText('observability.promptExpired')).toBeInTheDocument()
})
it('loads more calls without repeating a call ID or changing the total count', async () => {
  api.getObservedRequest.mockImplementation((_sid, _rid, cursor) =>
    Promise.resolve(
      cursor
        ? {
            ...data,
            call_count: 2,
            calls: [call, { ...call, call_id: 'call2', model_id: 'second' }],
          }
        : { ...data, call_count: 2, next_cursor: 'cursor' }
    )
  )
  render(<ExecutionDetails sessionId="l1" requestId="req1" />)
  fireEvent.click(screen.getByRole('button', { name: /observability.title/ }))
  const dialog = await screen.findByRole('dialog')
  fireEvent.click(await within(dialog).findByRole('button', { name: 'observability.loadMore' }))
  expect(await within(dialog).findByText('provider / second')).toBeInTheDocument()
  expect(within(dialog).getAllByText('provider / model')).toHaveLength(1)
})
it('does not apply a previous request response after changing identity', async () => {
  let previous!: (value: ObservedRequest) => void
  api.getObservedRequest.mockImplementation((_sid, rid) =>
    rid === 'old'
      ? new Promise((resolve) => {
          previous = resolve
        })
      : Promise.resolve({ ...data, call_count: 4 })
  )
  const view = render(<ExecutionDetails sessionId="l1" requestId="old" />)
  view.rerender(<ExecutionDetails sessionId="l1" requestId="new" />)
  expect(await screen.findByText('observability.calls 4')).toBeInTheDocument()
  previous({ ...data, call_count: 99 })
  await waitFor(() => expect(screen.queryByText('observability.calls 99')).not.toBeInTheDocument())
})
it('provides retry on query failure and explicit unrecorded history', async () => {
  api.getObservedRequest.mockRejectedValueOnce(new Error('offline'))
  render(<ExecutionDetails sessionId="l1" requestId="req1" />)
  fireEvent.click(screen.getByRole('button', { name: /observability.title/ }))
  const dialog = await screen.findByRole('dialog')
  // Opening the dialog triggers a fresh read rather than retaining a stale failure.
  await waitFor(() => expect(api.getObservedRequest).toHaveBeenCalledTimes(2))
  expect(within(dialog).getByText('provider / model')).toBeInTheDocument()
})
it('resolves persisted delegation by exact tool call ID after runtime instance disappears', async () => {
  api.getObservedRequest.mockResolvedValue({
    ...data,
    dispatches: [
      { dispatch_id: 'dlg1', tool_call_id: 'tool1' },
      { dispatch_id: 'wrong', tool_call_id: 'tool2', executor: 'Worker' },
    ],
  })
  render(<DelegationProcess sessionId="l1" requestId="req1" toolCallId="tool1" />)
  expect(await screen.findByText('Worker')).toBeInTheDocument()
  expect(api.getObservedDispatch).toHaveBeenCalledWith(
    'l1',
    'dlg1',
    undefined,
    expect.any(AbortSignal)
  )
  fireEvent.click(screen.getByText(/tool_exec_done/))
  expect(screen.getByText(/Persisted tool result/)).toBeInTheDocument()
})

it('shows explicit expired delegation details while retaining available final events and status', async () => {
  api.getObservedDispatch.mockResolvedValue({
    dispatch_id: 'dlg1',
    root_dispatch_id: 'dlg1',
    executor: 'Worker',
    executor_instance_id: 'instance1',
    details_expired: true,
    status: 'completed',
    events: [
      {
        sequence: 2,
        timestamp: call.started_at,
        type: 'completed',
        payload: { result: 'Available delegation summary' },
      },
    ],
  })
  render(<DispatchProcess sessionId="l1" dispatchId="dlg1" />)
  expect(await screen.findByText('observability.dispatchExpired')).toBeInTheDocument()
  expect(screen.queryByText('observability.noProcess')).not.toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'observability.settings' })).toHaveAttribute(
    'href',
    '#/settings/storage?category=execution_details'
  )
  fireEvent.click(screen.getByText(/2 · completed/))
  expect(screen.getByText(/Available delegation summary/)).toBeInTheDocument()
})
it('distinguishes a truncated delegation payload from an ordinary empty process', async () => {
  api.getObservedDispatch.mockResolvedValue({
    dispatch_id: 'dlg1',
    root_dispatch_id: 'dlg1',
    executor: 'Worker',
    executor_instance_id: 'instance1',
    status: 'completed',
    events: [
      {
        sequence: 1,
        timestamp: call.started_at,
        type: 'tool_exec_done',
        payload: { truncated: true, original_bytes: 1048576 },
      },
    ],
  })
  render(<DispatchProcess sessionId="l1" dispatchId="dlg1" />)
  expect(await screen.findByText('observability.partial')).toBeInTheDocument()
  expect(screen.queryByText('observability.dispatchExpired')).not.toBeInTheDocument()
})

it('shows persisted dispatch truncation even when the truncated event is on another page', async () => {
  api.getObservedDispatch.mockResolvedValue({
    dispatch_id: 'dlg1',
    root_dispatch_id: 'dlg1',
    executor: 'Worker',
    executor_instance_id: 'instance1',
    status: 'completed',
    details_truncated: true,
    events: [],
  })
  render(<DispatchProcess sessionId="l1" dispatchId="dlg1" />)
  expect(await screen.findByText('observability.partial')).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'observability.settings' })).toBeInTheDocument()
})

it('shows parallel child work as a nested trace and opens the selected call without fetching prompts early', async () => {
  api.getObservedRequest.mockResolvedValue({
    ...data,
    call_count: 2,
    calls: [call, { ...call, call_id: 'child', model_id: 'child-model', dispatch_id: 'nested' }],
    dispatches: [
      {
        dispatch_id: 'parent',
        root_dispatch_id: 'parent',
        executor: 'Research',
        status: 'completed',
        created_at: call.started_at,
        updated_at: call.finished_at,
        events: [],
      },
      {
        dispatch_id: 'nested',
        parent_dispatch_id: 'parent',
        root_dispatch_id: 'parent',
        executor: 'Nested worker',
        status: 'completed',
        created_at: call.started_at,
        updated_at: call.finished_at,
        events: [],
      },
    ],
  })
  render(<ExecutionDetails sessionId="l1" requestId="req1" />)
  fireEvent.click(screen.getByRole('button', { name: /observability.title/ }))
  const dialog = await screen.findByRole('dialog')
  const trace = await within(dialog).findByRole('list', { name: 'observability.trace' })
  const research = within(trace)
    .getByRole('button', { name: /^Research/ })
    .closest('li')!
  const nested = within(research)
    .getByRole('button', { name: /^Nested worker/ })
    .closest('li')!
  expect(
    within(nested).getByRole('button', { name: /provider \/ child-model/ })
  ).toBeInTheDocument()
  expect(api.getObservedPrompt).not.toHaveBeenCalled()
  fireEvent.click(within(nested).getByRole('button', { name: /provider \/ child-model/ }))
  fireEvent.click(within(dialog).getByRole('button', { name: 'observability.prompt' }))
  await waitFor(() =>
    expect(api.getObservedPrompt).toHaveBeenCalledWith(
      'l1',
      'req1',
      'child',
      expect.any(AbortSignal)
    )
  )
})

it('returns keyboard focus to the conversation when the trace drawer closes', async () => {
  const user = userEvent.setup()
  render(<ExecutionDetails sessionId="l1" requestId="req1" />)
  const trigger = screen.getByRole('button', { name: /observability.title/ })
  await user.click(trigger)
  expect(await screen.findByRole('dialog')).toHaveAttribute('data-placement', 'right')
  await user.keyboard('{Escape}')
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  expect(trigger).toHaveFocus()
})
it('preserves a failed trace read and lets the user retry', async () => {
  api.getObservedRequest.mockRejectedValue(new Error('offline'))
  render(<ExecutionDetails sessionId="l1" requestId="req1" />)
  fireEvent.click(screen.getByRole('button', { name: /observability.title/ }))
  const dialog = await screen.findByRole('dialog')
  expect(await within(dialog).findByRole('alert')).toHaveTextContent('offline')
  api.getObservedRequest.mockResolvedValue(data)
  fireEvent.click(within(dialog).getByRole('button', { name: 'observability.retry' }))
  expect(
    await within(dialog).findByRole('list', { name: 'observability.trace' })
  ).toBeInTheDocument()
  expect(within(dialog).queryByRole('alert')).not.toBeInTheDocument()
})

it('scrolls the selected mobile node details after their contents have updated', async () => {
  const previousWidth = window.innerWidth
  const previousScroll = HTMLElement.prototype.scrollIntoView
  let visibleDetails = ''
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: 390 })
  HTMLElement.prototype.scrollIntoView = function () {
    visibleDetails = this.textContent || ''
  }
  try {
    render(<ExecutionDetails sessionId="l1" requestId="req1" />)
    fireEvent.click(screen.getByRole('button', { name: /observability.title/ }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(await within(dialog).findByText('provider / model'))
    expect(visibleDetails).toContain('provider / model')
  } finally {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: previousWidth })
    if (previousScroll) HTMLElement.prototype.scrollIntoView = previousScroll
    else delete (HTMLElement.prototype as Partial<HTMLElement>).scrollIntoView
  }
})

it('shows historical measured span and unknown attempts once, without an empty prompt action', async () => {
  api.getObservedRequest.mockResolvedValue({
    ...data,
    duration_source: 'call_span',
    duration_ms: 88445,
    attempts_reported: false,
    metrics_only: true,
    recording_status: 'unrecorded',
    calls: [
      {
        ...call,
        content: undefined,
        reasoning: undefined,
        prompt_available: false,
        metrics_only: true,
        attempts_reported: false,
        retry_count: 0,
        recording_status: 'unrecorded',
      },
    ],
  })
  render(<ExecutionDetails sessionId="l1" requestId="legacy" />)
  expect(await screen.findByText('observability.callSpan 88.4 s')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: /observability.title/ }))
  const dialog = await screen.findByRole('dialog')
  expect(await within(dialog).findAllByText('observability.metricsOnly')).toHaveLength(1)
  fireEvent.click(within(dialog).getByText('provider / model'))
  expect(
    within(dialog).queryByRole('button', { name: 'observability.prompt' })
  ).not.toBeInTheDocument()
  expect(within(dialog).getByText('observability.contentMissing')).toBeInTheDocument()
  expect(within(dialog).getByText('observability.reasoningMissing')).toBeInTheDocument()
  expect(api.getObservedPrompt).not.toHaveBeenCalled()
  expect(within(dialog).queryByText('observability.recordingNotice')).not.toBeInTheDocument()
  expect(within(dialog).getAllByText('observability.historicalMetrics')).toHaveLength(2)
})
it('references repeated instructions without fetching them and keeps changed instructions lazy', async () => {
  api.getObservedRequest.mockResolvedValue({
    ...data,
    call_count: 3,
    calls: [
      call,
      { ...call, call_id: 'second', model_id: 'same', prompt_source_call_id: 'call1' },
      { ...call, call_id: 'third', model_id: 'changed' },
    ],
  })
  render(<ExecutionDetails sessionId="l1" requestId="req1" />)
  fireEvent.click(screen.getByRole('button', { name: /observability.title/ }))
  const dialog = await screen.findByRole('dialog')
  fireEvent.click(await within(dialog).findByText('provider / same'))
  expect(within(dialog).getByText('observability.promptUnchanged')).toBeInTheDocument()
  expect(within(dialog).queryByText('call1')).not.toBeInTheDocument()
  expect(
    within(dialog).queryByRole('button', { name: 'observability.prompt' })
  ).not.toBeInTheDocument()
  expect(api.getObservedPrompt).not.toHaveBeenCalled()
  fireEvent.click(within(dialog).getByText('provider / changed'))
  fireEvent.click(within(dialog).getByRole('button', { name: 'observability.prompt' }))
  await waitFor(() =>
    expect(api.getObservedPrompt).toHaveBeenCalledWith(
      'l1',
      'req1',
      'third',
      expect.any(AbortSignal)
    )
  )
})
it('keeps unknown duration visibly unknown rather than displaying zero', async () => {
  api.getObservedRequest.mockResolvedValue({
    ...data,
    duration_source: 'unknown',
    duration_ms: 0,
    attempts_reported: false,
  })
  render(<ExecutionDetails sessionId="l1" requestId="req1" />)
  expect(
    await screen.findByText('observability.duration observability.unknown')
  ).toBeInTheDocument()
  expect(screen.queryByText(/0.0 s/)).not.toBeInTheDocument()
})

it('shows independent main and child tool spans with escaped arguments, results and duration', async () => {
  api.getObservedRequest.mockResolvedValue({
    ...data,
    tools: [
      {
        ...call,
        call_id: 'main-tool',
        kind: 'tool',
        tool_name: 'Bash',
        arguments: '{"command":"printf main"}',
        content: '<script>main result</script>',
        duration_ms: 125,
      },
      {
        ...call,
        call_id: 'child-tool',
        kind: 'tool',
        tool_name: 'Read',
        arguments: '{"path":"child.txt"}',
        content: 'child result',
        dispatch_id: 'dlg-child',
      },
    ],
    dispatches: [
      {
        dispatch_id: 'dlg-child',
        root_dispatch_id: 'dlg-child',
        executor: 'Worker',
        executor_instance_id: 'worker',
        status: 'completed',
        created_at: call.started_at,
        updated_at: call.finished_at,
        events: [],
      },
    ],
  })
  render(<ExecutionDetails sessionId="l1" requestId="req1" />)
  await screen.findByText('observability.calls 1')
  fireEvent.click(screen.getByRole('button', { name: /observability.title/ }))
  const dialog = await screen.findByRole('dialog')
  fireEvent.click(within(dialog).getByText('Bash'))
  expect(
    within(dialog).getByRole('region', { name: 'observability.toolArguments' })
  ).toHaveTextContent('printf main')
  expect(within(dialog).getByText('125 ms')).toBeInTheDocument()
  expect(within(dialog).getByText('<script>main result</script>')).toBeInTheDocument()
  expect(document.querySelector('script')).toBeNull()
  fireEvent.click(within(dialog).getByText('Read'))
  expect(within(dialog).getByText('child result')).toBeInTheDocument()
})

it('refreshes a running tool loaded from a later page when the execution completes', async () => {
  let complete = false
  api.getObservedRequest.mockImplementation((_s, _r, cursor) =>
    Promise.resolve(
      cursor
        ? {
            ...data,
            calls: [],
            tools: [
              {
                ...call,
                kind: 'tool',
                call_id: 'later',
                tool_name: 'Read',
                arguments: '{}',
                status: complete ? 'success' : 'running',
                content: complete ? 'Completed later tool' : '',
              },
            ],
          }
        : { ...data, next_cursor: 'next', tools: [] }
    )
  )
  const view = render(<ExecutionDetails sessionId="l1" requestId="req1" />)
  await screen.findByText('observability.calls 1')
  fireEvent.click(screen.getByRole('button', { name: /observability.title/ }))
  const dialog = await screen.findByRole('dialog')
  fireEvent.click(await within(dialog).findByRole('button', { name: 'observability.loadMore' }))
  fireEvent.click(await within(dialog).findByText('Read'))
  complete = true
  view.rerender(<ExecutionDetails sessionId="l1" requestId="req1" running />)
  expect(await within(dialog).findByText('Completed later tool')).toBeInTheDocument()
})
