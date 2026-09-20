import { createRef } from 'react'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { AgentStreamState } from '@/types'
import { AgentStreamView } from './AgentStreamView'

vi.mock('@/components/DelegationCard', () => ({
  DelegationCard: ({ requestId, agentInstanceId, result }: { requestId?: string; agentInstanceId?: string; result?: string }) => (
    <div
      data-testid="nested-delegation-card"
      data-request-id={requestId ?? ''}
      data-agent-instance-id={agentInstanceId ?? ''}
      data-result={result ?? ''}
    />
  ),
}))

describe('AgentStreamView', () => {
  const originalScrollIntoView = HTMLElement.prototype.scrollIntoView
  const originalScrollTo = HTMLElement.prototype.scrollTo
  const originalResizeObserver = globalThis.ResizeObserver
  let resizeCallback: ResizeObserverCallback | undefined

  afterEach(() => {
    if (originalScrollIntoView) {
      HTMLElement.prototype.scrollIntoView = originalScrollIntoView
    } else {
      delete (HTMLElement.prototype as Partial<HTMLElement>).scrollIntoView
    }
    if (originalScrollTo) {
      HTMLElement.prototype.scrollTo = originalScrollTo
    } else {
      delete (HTMLElement.prototype as Partial<HTMLElement>).scrollTo
    }
    if (originalResizeObserver) {
      globalThis.ResizeObserver = originalResizeObserver
    } else {
      delete (globalThis as Partial<typeof globalThis>).ResizeObserver
    }
    resizeCallback = undefined
  })

  it('keeps the supplied stream viewport pinned when content grows without scrolling ancestors', async () => {
    const scrollIntoView = vi.fn()
    const scrollTo = vi.fn()
    HTMLElement.prototype.scrollIntoView = scrollIntoView
    HTMLElement.prototype.scrollTo = scrollTo
    globalThis.ResizeObserver = class {
      constructor(callback: ResizeObserverCallback) {
        resizeCallback = callback
      }

      observe() {}
      unobserve() {}
      disconnect() {}
    } as typeof ResizeObserver

    const streamScrollRef = createRef<HTMLDivElement>()
    const state: AgentStreamState = {
      agent_id: 'agent-1',
      processing: true,
      segments: [],
      iteration: 1,
    }

    render(
      <div ref={streamScrollRef} data-testid="stream-viewport">
        <AgentStreamView state={state} scrollContainerRef={streamScrollRef} />
      </div>
    )

    const viewport = screen.getByTestId('stream-viewport')
    Object.defineProperty(viewport, 'scrollHeight', { configurable: true, value: 400 })

    resizeCallback?.([], {} as ResizeObserver)

    await waitFor(() => expect(scrollTo).toHaveBeenLastCalledWith({ top: 400, behavior: 'auto' }))
    expect(scrollIntoView).not.toHaveBeenCalled()
  })

  it('does not return to the bottom when a new segment arrives after the user scrolls up', () => {
    const scrollTo = vi.fn()
    HTMLElement.prototype.scrollTo = scrollTo
    globalThis.ResizeObserver = class {
      constructor(callback: ResizeObserverCallback) {
        resizeCallback = callback
      }

      observe() {}
      unobserve() {}
      disconnect() {}
    } as typeof ResizeObserver

    const streamScrollRef = createRef<HTMLDivElement>()
    const state: AgentStreamState = {
      agent_id: 'agent-1',
      processing: true,
      segments: [],
      iteration: 1,
    }

    const { rerender } = render(
      <div ref={streamScrollRef} data-testid="stream-viewport">
        <AgentStreamView state={state} scrollContainerRef={streamScrollRef} />
      </div>
    )

    const viewport = screen.getByTestId('stream-viewport')
    Object.defineProperties(viewport, {
      scrollHeight: { configurable: true, value: 400 },
      clientHeight: { configurable: true, value: 100 },
      scrollTop: { configurable: true, writable: true, value: 0 },
    })
    fireEvent.scroll(viewport)
    const callsBeforeNewSegment = scrollTo.mock.calls.length

    rerender(
      <div ref={streamScrollRef} data-testid="stream-viewport">
        <AgentStreamView
          state={{ ...state, segments: [{ type: 'thinking', text: 'new output' }] }}
          scrollContainerRef={streamScrollRef}
        />
      </div>
    )

    expect(scrollTo).toHaveBeenCalledTimes(callsBeforeNewSegment)
  })

  it('passes its request-scoped stream identity to nested delegation cards', () => {
    HTMLElement.prototype.scrollTo = vi.fn()
    globalThis.ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    } as typeof ResizeObserver
    const streamScrollRef = createRef<HTMLDivElement>()
    const state: AgentStreamState = {
      agent_id: 'agent-1',
      request_id: 'request-parent',
      processing: true,
      iteration: 1,
      segments: [{
        type: 'tool_call',
        call_id: 'call-1',
        name: 'delegate',
        args: '{"target":"research"}',
        agent_instance_id: 'child-instance-1',
        result: '',
        error: '',
        done: false,
        duration_ms: 0,
      }],
    }

    render(
      <div ref={streamScrollRef}>
        <AgentStreamView state={state} scrollContainerRef={streamScrollRef} />
      </div>,
    )

    expect(screen.getByTestId('nested-delegation-card')).toHaveAttribute('data-request-id', 'request-parent')
    expect(screen.getByTestId('nested-delegation-card')).toHaveAttribute('data-agent-instance-id', 'child-instance-1')
  })

  it('passes a completed nested delegation result after the stream is restored', () => {
    HTMLElement.prototype.scrollTo = vi.fn()
    globalThis.ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    } as typeof ResizeObserver
    const streamScrollRef = createRef<HTMLDivElement>()

    render(
      <div ref={streamScrollRef}>
        <AgentStreamView
          state={{
            agent_id: 'agent-1',
            request_id: 'request-parent',
            processing: false,
            iteration: 2,
            segments: [{
              type: 'tool_call',
              call_id: 'call-1',
              name: 'delegate',
              args: '{}',
              agent_instance_id: 'child-instance-1',
              result: 'completed nested result',
              error: '',
              done: true,
              duration_ms: 42,
            }],
          }}
          scrollContainerRef={streamScrollRef}
        />
      </div>,
    )

    fireEvent.click(screen.getByRole('button', { name: /Delegated 1 task/ }))
    expect(screen.getByTestId('nested-delegation-card')).toHaveAttribute('data-result', 'completed nested result')
  })
})
