import { createRef } from 'react'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { AgentStreamState } from '@/types'
import { AgentStreamView } from './AgentStreamView'

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
})
