import type { ObservedCall, ObservedDispatch, ObservedRequest } from '@/lib/api/observability-api'

export interface TraceNode {
  id: string
  kind: 'request' | 'call' | 'dispatch' | 'tool'
  label: string
  status: string
  start?: number
  end?: number
  warning: boolean
  missingParent?: boolean
  tool?: ObservedCall
  call?: ObservedCall
  dispatch?: ObservedDispatch
  children: TraceNode[]
}

export function traceTime(value?: string): number | undefined {
  const time = value ? Date.parse(value) : NaN
  return Number.isFinite(time) && time > 0 ? time : undefined
}

export function buildTrace(data: ObservedRequest, now = Date.now()) {
  const interval = (start?: string, finish?: string, duration?: number, active = false) => {
    const from = traceTime(start)
    const until =
      traceTime(finish) ??
      (from !== undefined && active
        ? now
        : from !== undefined && duration !== undefined && Number.isFinite(duration) && duration >= 0
          ? from + duration
          : undefined)
    return {
      start: from,
      end: from !== undefined && until !== undefined && until >= from ? until : undefined,
    }
  }
  const active = (status: string) => ['running', 'queued', 'persistence_pending'].includes(status)
  const root: TraceNode = {
    id: 'request',
    kind: 'request',
    label: '',
    status: data.metrics_only ? 'historicalMetrics' : data.status,
    ...interval(data.started_at, data.finished_at, data.duration_ms, active(data.status)),
    warning: !data.metrics_only && data.recording_status !== 'complete',
    children: [],
  }
  const dispatches = new Map<string, TraceNode>()
  for (const dispatch of data.dispatches) {
    if (dispatches.has(dispatch.dispatch_id)) continue
    dispatches.set(dispatch.dispatch_id, {
      id: `dispatch:${dispatch.dispatch_id}`,
      kind: 'dispatch',
      label: dispatch.executor,
      status: dispatch.status,
      dispatch,
      ...interval(
        dispatch.created_at,
        active(dispatch.status) ? undefined : dispatch.updated_at,
        undefined,
        active(dispatch.status)
      ),
      warning: !!dispatch.details_expired || !!dispatch.details_truncated,
      children: [],
    })
  }
  for (const node of dispatches.values()) {
    const parentID = node.dispatch!.parent_dispatch_id
    let parent = parentID ? dispatches.get(parentID) : undefined
    // Broken legacy links and cycles stay visible without inventing a parent span.
    const seen = new Set([node.id])
    let ancestor = parent
    while (ancestor) {
      if (seen.has(ancestor.id) || seen.size > 64) {
        parent = undefined
        break
      }
      seen.add(ancestor.id)
      ancestor = dispatches.get(ancestor.dispatch!.parent_dispatch_id || '')
    }
    node.missingParent = !!parentID && !parent
    ;(parent || root).children.push(node)
  }
  const seenCalls = new Set<string>()
  for (const call of data.calls) {
    if (seenCalls.has(call.call_id)) continue
    seenCalls.add(call.call_id)
    const candidates = call.agent_id
      ? [...dispatches.values()].filter(
          (node) => node.dispatch?.executor_instance_id === call.agent_id
        )
      : []
    const parent = call.dispatch_id
      ? dispatches.get(call.dispatch_id)
      : candidates.length === 1
        ? candidates[0]
        : undefined
    const node: TraceNode = {
      id: `call:${call.call_id}`,
      kind: 'call',
      label: `${call.provider_id || '—'} / ${call.model_id || '—'}`,
      status: call.status,
      call,
      ...interval(call.started_at, call.finished_at, call.duration_ms, active(call.status)),
      warning: (!call.metrics_only && call.recording_status !== 'complete') || call.retry_count > 0,
      missingParent: !parent && (!!call.dispatch_id || candidates.length > 1),
      children: [],
    }
    ;(parent || root).children.push(node)
  }
  for (const tool of data.tools || []) {
    const candidates = tool.agent_id
      ? [...dispatches.values()].filter(
          (node) => node.dispatch?.executor_instance_id === tool.agent_id
        )
      : []
    const parent = tool.dispatch_id
      ? dispatches.get(tool.dispatch_id)
      : candidates.length === 1
        ? candidates[0]
        : undefined
    const node: TraceNode = {
      id: `tool:${tool.call_id}`,
      kind: 'tool',
      label: tool.tool_name || '—',
      status: tool.status,
      tool,
      ...interval(tool.started_at, tool.finished_at, tool.duration_ms, active(tool.status)),
      warning: tool.recording_status !== 'complete',
      missingParent: !parent && (!!tool.dispatch_id || candidates.length > 1),
      children: [],
    }
    ;(parent || root).children.push(node)
  }
  const nodes: TraceNode[] = []
  const sort = (node: TraceNode) => {
    nodes.push(node)
    node.children.sort(
      (a, b) => (a.start ?? Infinity) - (b.start ?? Infinity) || a.id.localeCompare(b.id)
    )
    node.children.forEach(sort)
  }
  sort(root)
  let start: number | undefined
  let end: number | undefined
  for (const node of nodes) {
    for (const time of [node.start, node.end]) {
      if (time === undefined) continue
      start = Math.min(start ?? time, time)
      end = Math.max(end ?? time, time)
    }
  }
  return { root, nodes, start, end }
}

export function isTraceError(status: string) {
  return ['error', 'failed', 'panic', 'interrupted', 'cancelled', 'timeout'].includes(status)
}
