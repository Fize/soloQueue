// SSE client: calls the streaming endpoint and yields typed events.
// Uses fetch + ReadableStream since EventSource doesn't support POST.

export interface SSEContentDelta {
  type: 'content_delta'
  delta: string
}
export interface SSEReasoningDelta {
  type: 'reasoning_delta'
  delta: string
}
export interface SSEToolStart {
  type: 'tool_start'
  call_id: string
  name: string
  args: string
}
export interface SSEToolDone {
  type: 'tool_done'
  call_id: string
  name: string
  result: string
  error: string
  duration_ms: number
}
export interface SSEToolConfirm {
  type: 'tool_confirm'
  call_id: string
  name: string
  prompt: string
}
export interface SSEDone {
  type: 'done'
  content: string
  reasoning_content: string
}
export interface SSEError {
  type: 'error'
  error: string
}
export interface SSEDelegationStart {
  type: 'delegation_start'
  num_tasks: number
}
export interface SSESessionName {
  type: 'session_name'
  name: string
}

export interface SSEDelegationDone {
  type: 'delegation_done'
  target_agent_id: string
}

export type SSEEvent =
  | SSEContentDelta
  | SSEReasoningDelta
  | SSEToolStart
  | SSEToolDone
  | SSEToolConfirm
  | SSEDone
  | SSEError
  | SSEDelegationStart
  | SSESessionName
  | SSEDelegationDone

export async function streamAsk(
  prompt: string,
  sessionId: string,
  signal?: AbortSignal,
): Promise<AsyncGenerator<SSEEvent, void, unknown>> {
  const response = await fetch('/api/session/ask/stream', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ prompt, session_id: sessionId }),
    signal,
  })

  if (!response.ok) {
    const err = await response.json().catch(() => ({ error: response.statusText }))
    throw new Error(err.error || `HTTP ${response.status}`)
  }

  const reader = response.body!.getReader()
  const decoder = new TextDecoder()

  return (async function* () {
    let buffer = ''
    let eventType = ''
    let dataStr = ''

    while (true) {
      const { done, value } = await reader.read()
      if (done) break

      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || '' // keep incomplete line in buffer

      for (const line of lines) {
        if (line.startsWith('event: ')) {
          eventType = line.slice(7).trim()
        } else if (line.startsWith('data: ')) {
          dataStr = line.slice(6)
          try {
            const parsed = JSON.parse(dataStr)
            if (eventType) {
              yield { type: eventType, ...parsed } as SSEEvent
            }
          } catch {
            // skip malformed data
          }
          eventType = ''
          dataStr = ''
        }
        // empty line = end of event, reset
      }
    }
  })()
}

// Re-export as a function (not generator) that returns an AbortController + generator.
export function createStream(
  prompt: string,
  sessionId: string,
): { abort: AbortController; events: Promise<AsyncGenerator<SSEEvent, void, unknown>> } {
  const abort = new AbortController()
  const events = streamAsk(prompt, sessionId, abort.signal)
  return { abort, events }
}
