import { expect, it } from 'vitest'
import { buildTrace } from './trace'
import type { ObservedRequest, ObservedCall, ObservedDispatch } from '@/lib/api/observability-api'
const start = '2026-10-26T00:00:00Z'
const end = '2026-10-26T00:00:10Z'
const request = (calls: ObservedCall[], dispatches: ObservedDispatch[] = []): ObservedRequest => ({
  session_id: 'l1',
  request_id: 'r',
  status: 'completed',
  recording_status: 'complete',
  started_at: start,
  finished_at: end,
  duration_ms: 10000,
  calls,
  dispatches,
  call_count: calls.length,
  attempt_count: 0,
  errors: [],
})
const call = (id: string, extra: Partial<ObservedCall> = {}): ObservedCall => ({
  call_id: id,
  request_id: 'r',
  run_id: 'run',
  agent_id: '',
  provider_id: 'test',
  model_id: id,
  usage_type: 'chat',
  status: 'success',
  started_at: start,
  finished_at: end,
  duration_ms: 10000,
  attempt_count: 1,
  retry_count: 0,
  attempts_reported: true,
  prompt_tokens: 1,
  completion_tokens: 1,
  total_tokens: 2,
  prompt_available: true,
  recording_status: 'complete',
  ...extra,
})
const dispatch = (id: string, parent?: string): ObservedDispatch => ({
  dispatch_id: id,
  parent_dispatch_id: parent,
  root_dispatch_id: id,
  executor: id,
  executor_instance_id: id,
  status: 'completed',
  created_at: start,
  updated_at: end,
  events: [],
})
it('keeps overlapping spans on the same clock instead of summing durations', () => {
  const trace = buildTrace(request([call('a'), call('b')]))
  expect(trace.end! - trace.start!).toBe(10000)
  expect(trace.root.children[0].start).toBe(trace.root.children[1].start)
  expect(trace.root.children[0].end).toBe(trace.root.children[1].end)
  expect(trace.root.end! - trace.root.start!).toBe(10000)
})
it('retains orphan and cyclic legacy spans without inventing links or timestamps', () => {
  const trace = buildTrace(
    request(
      [
        call('orphan', { dispatch_id: 'missing', started_at: 'bad', finished_at: undefined }),
        call('known', { agent_id: 'worker' }),
      ],
      [dispatch('a', 'b'), dispatch('b', 'a'), dispatch('worker')]
    )
  )
  expect(trace.nodes).toHaveLength(6)
  expect(trace.nodes.find((node) => node.id === 'call:orphan')).toMatchObject({
    missingParent: true,
    start: undefined,
    end: undefined,
  })
  expect(trace.root.children.find((node) => node.id === 'dispatch:a')?.missingParent).toBe(true)
  expect(trace.nodes.find((node) => node.id === 'dispatch:worker')?.children[0].id).toBe(
    'call:known'
  )
})

it('uses the exact dispatch link when a worker instance was reused by multiple subtasks', () => {
  const first = { ...dispatch('first'), executor_instance_id: 'reused' }
  const second = { ...dispatch('second'), executor_instance_id: 'reused' }
  const trace = buildTrace(
    request([call('linked', { agent_id: 'reused', dispatch_id: 'first' })], [first, second])
  )
  expect(trace.nodes.find((node) => node.id === 'call:linked')?.missingParent).toBe(false)
  expect(trace.nodes.find((node) => node.id === 'dispatch:first')?.children[0].id).toBe(
    'call:linked'
  )
})
