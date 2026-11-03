import { request } from './core'
export type RecordingStatus = 'complete' | 'partial' | 'expired' | 'unrecorded'
export interface ObservedCall {
  kind?: 'tool'; tool_name?: string; arguments?: string
  call_id: string; request_id: string; request_ids?: string[]; run_id: string; agent_id: string
  dispatch_id?: string; parent_dispatch_id?: string; tool_call_id?: string
  provider_id: string; model_id: string; usage_type: string; status: string
  started_at: string; finished_at?: string; duration_ms: number; attempt_count: number
  retry_count: number; attempts_reported: boolean; prompt_tokens: number; completion_tokens: number
  total_tokens: number; prompt_available: boolean; recording_status: RecordingStatus
  content?: string; reasoning?: string; error?: string
  prompt_source_call_id?: string; metrics_only?: boolean
}
export interface DispatchEvent { sequence: number; timestamp: string; type: string; payload?: Record<string, unknown> }
export interface ObservedDispatch {
  details_expired?: boolean
  details_truncated?: boolean
  request_id?: string; tool_call_id?: string
  dispatch_id: string; parent_dispatch_id?: string; root_dispatch_id: string; executor: string
  executor_instance_id: string; status: string; created_at: string; updated_at: string
  events: DispatchEvent[]; next_cursor?: string
}
export interface ObservedRequest {
  generation?: string
  session_id: string; request_id: string; status: string; recording_status: RecordingStatus
  started_at: string; finished_at?: string; duration_ms: number; call_count: number; attempt_count: number
  duration_source?: 'request' | 'call_span' | 'unknown'; timing_complete?: boolean; attempts_reported?: boolean; metrics_only?: boolean
  calls: ObservedCall[]; tools?: ObservedCall[]; dispatches: ObservedDispatch[]; next_cursor?: string; errors: string[]
}
export interface ObservedPrompt {
  call_id: string; recording_status: RecordingStatus; representation: 'messages' | 'instructions'
  messages: { role: string; content: string }[]; instructions?: string
  snapshot_version?: number; logical_request?: unknown; wire_requests?: string[]; wire_recording_status?: RecordingStatus
}
function query(sessionId: string, values: Record<string, string | undefined>) {
  const params = new URLSearchParams({ session_id: sessionId })
  Object.entries(values).forEach(([key, value]) => { if (value) params.set(key, value) })
  return params.toString()
}
export const getObservedRequest = (sessionId: string, requestId: string, cursor?: string, signal?: AbortSignal) =>
  request<ObservedRequest>(`/observability/request?${query(sessionId, { request_id: requestId, cursor, limit: '50' })}`, { signal })
export const getObservedPrompt = (sessionId: string, requestId: string, callId: string, signal?: AbortSignal) =>
  request<ObservedPrompt>(`/observability/prompt?${query(sessionId, { request_id: requestId, call_id: callId })}`, { signal })
export const getObservedDispatch = (sessionId: string, dispatchId: string, cursor?: string, signal?: AbortSignal) =>
  request<ObservedDispatch>(`/observability/dispatch?${query(sessionId, { dispatch_id: dispatchId, cursor, limit: '50' })}`, { signal })
function formatDuration(ms: number, gap: string): string {
  if (!Number.isFinite(ms)) return '—'
  const total = Math.round(Math.max(0, ms))
  if (total < 1000) return `${total}${gap}ms`
  if (total < 60000) return `${(Math.floor(total / 100) / 10).toFixed(1)}${gap}s`
  const seconds = Math.floor(total / 1000)
  const tiers: [number, string][] = [
    [Math.floor(seconds / 86400), 'd'],
    [Math.floor(seconds / 3600) % 24, 'h'],
    [Math.floor(seconds / 60) % 60, 'm'],
    [seconds % 60, 's'],
  ]
  return tiers
    .filter(([value]) => value > 0)
    .map(([value, unit]) => `${value}${unit}`)
    .join(gap)
}
export function formatExecutionTime(ms: number): string { return formatDuration(ms, ' ') }
export function formatExecutionTimeCompact(ms: number): string { return formatDuration(ms, '') }
