import { createRef } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useStickToBottom } from './useStickToBottom'

describe('useStickToBottom', () => {
  const originalResizeObserver = globalThis.ResizeObserver
  const originalScrollIntoView = HTMLElement.prototype.scrollIntoView
  const originalScrollTo = HTMLElement.prototype.scrollTo

  afterEach(() => {
    globalThis.ResizeObserver = originalResizeObserver
    HTMLElement.prototype.scrollIntoView = originalScrollIntoView
    HTMLElement.prototype.scrollTo = originalScrollTo
  })

  it('scrolls only the supplied viewport when attached content grows', () => {
    let resizeCallback: ResizeObserverCallback | undefined
    const scrollTo = vi.fn()
    const scrollIntoView = vi.fn()
    HTMLElement.prototype.scrollTo = scrollTo
    HTMLElement.prototype.scrollIntoView = scrollIntoView
    globalThis.ResizeObserver = class {
      constructor(callback: ResizeObserverCallback) {
        resizeCallback = callback
      }

      observe() {}
      unobserve() {}
      disconnect() {}
    } as typeof ResizeObserver

    function Harness() {
      const { scrollRef, contentRef } = useStickToBottom()
      return (
        <div ref={scrollRef} data-testid="viewport">
          <div ref={contentRef}>stream output</div>
        </div>
      )
    }

    render(<Harness />)
    const viewport = screen.getByTestId('viewport')
    Object.defineProperty(viewport, 'scrollHeight', { configurable: true, value: 400 })

    resizeCallback?.([], {} as ResizeObserver)

    expect(scrollTo).toHaveBeenLastCalledWith({ top: 400, behavior: 'auto' })
    expect(scrollIntoView).not.toHaveBeenCalled()
  })

  it('observes content that mounts after the viewport', () => {
    let resizeCallback: ResizeObserverCallback | undefined
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

    function Harness({ showContent }: { showContent: boolean }) {
      const { scrollRef, contentRef } = useStickToBottom()
      return (
        <div ref={scrollRef} data-testid="viewport">
          {showContent && <div ref={contentRef}>stream output</div>}
        </div>
      )
    }

    const { rerender } = render(<Harness showContent={false} />)
    const viewport = screen.getByTestId('viewport')
    Object.defineProperty(viewport, 'scrollHeight', { configurable: true, value: 400 })

    rerender(<Harness showContent />)
    resizeCallback?.([], {} as ResizeObserver)

    expect(scrollTo).toHaveBeenLastCalledWith({ top: 400, behavior: 'auto' })
  })

  it('follows new output when the viewport is attached to the bottom', () => {
    const scrollTo = vi.fn()
    HTMLElement.prototype.scrollTo = scrollTo
    globalThis.ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    } as typeof ResizeObserver

    let followOutput: (() => void) | undefined
    function Harness() {
      const { scrollRef, contentRef, followOutput: follow } = useStickToBottom()
      followOutput = follow
      return (
        <div ref={scrollRef} data-testid="viewport">
          <div ref={contentRef}>stream output</div>
        </div>
      )
    }

    render(<Harness />)
    const viewport = screen.getByTestId('viewport')
    Object.defineProperty(viewport, 'scrollHeight', { configurable: true, value: 400 })

    expect(followOutput).toBeTypeOf('function')
    followOutput?.()

    expect(scrollTo).toHaveBeenLastCalledWith({ top: 400, behavior: 'auto' })
  })

  it('does not follow new output after the user scrolls away from the bottom', () => {
    const scrollTo = vi.fn()
    HTMLElement.prototype.scrollTo = scrollTo
    globalThis.ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    } as typeof ResizeObserver

    let followOutput: (() => void) | undefined
    function Harness() {
      const { scrollRef, contentRef, followOutput: follow } = useStickToBottom()
      followOutput = follow
      return (
        <div ref={scrollRef} data-testid="viewport">
          <div ref={contentRef}>stream output</div>
        </div>
      )
    }

    render(<Harness />)
    const viewport = screen.getByTestId('viewport')
    Object.defineProperties(viewport, {
      scrollHeight: { configurable: true, value: 400 },
      clientHeight: { configurable: true, value: 100 },
      scrollTop: { configurable: true, writable: true, value: 0 },
    })
    fireEvent.scroll(viewport)

    followOutput?.()

    expect(scrollTo).not.toHaveBeenCalled()
  })

  it('reattaches after the user returns within 48 pixels of the bottom', () => {
    const scrollTo = vi.fn()
    HTMLElement.prototype.scrollTo = scrollTo
    globalThis.ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    } as typeof ResizeObserver

    let followOutput: (() => void) | undefined
    function Harness() {
      const { scrollRef, contentRef, followOutput: follow } = useStickToBottom()
      followOutput = follow
      return (
        <div ref={scrollRef} data-testid="viewport">
          <div ref={contentRef}>stream output</div>
        </div>
      )
    }

    render(<Harness />)
    const viewport = screen.getByTestId('viewport')
    Object.defineProperties(viewport, {
      scrollHeight: { configurable: true, value: 400 },
      clientHeight: { configurable: true, value: 100 },
      scrollTop: { configurable: true, writable: true, value: 0 },
    })
    fireEvent.scroll(viewport)
    viewport.scrollTop = 270
    fireEvent.scroll(viewport)

    followOutput?.()

    expect(scrollTo).toHaveBeenLastCalledWith({ top: 400, behavior: 'auto' })
  })

  it('detaches immediately when the user starts a wheel interaction', () => {
    const scrollTo = vi.fn()
    HTMLElement.prototype.scrollTo = scrollTo
    globalThis.ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    } as typeof ResizeObserver

    let followOutput: (() => void) | undefined
    function Harness() {
      const { scrollRef, contentRef, followOutput: follow } = useStickToBottom()
      followOutput = follow
      return (
        <div ref={scrollRef} data-testid="viewport">
          <div ref={contentRef}>stream output</div>
        </div>
      )
    }

    render(<Harness />)
    fireEvent.wheel(screen.getByTestId('viewport'))

    followOutput?.()

    expect(scrollTo).not.toHaveBeenCalled()
  })

  it('detaches immediately when the user starts a touch interaction', () => {
    const scrollTo = vi.fn()
    HTMLElement.prototype.scrollTo = scrollTo
    globalThis.ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    } as typeof ResizeObserver

    let followOutput: (() => void) | undefined
    function Harness() {
      const { scrollRef, contentRef, followOutput: follow } = useStickToBottom()
      followOutput = follow
      return (
        <div ref={scrollRef} data-testid="viewport">
          <div ref={contentRef}>stream output</div>
        </div>
      )
    }

    render(<Harness />)
    fireEvent.touchStart(screen.getByTestId('viewport'))

    followOutput?.()

    expect(scrollTo).not.toHaveBeenCalled()
  })

  it('reattaches when the page explicitly resets following', () => {
    const scrollTo = vi.fn()
    HTMLElement.prototype.scrollTo = scrollTo
    globalThis.ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    } as typeof ResizeObserver

    let followOutput: (() => void) | undefined
    let resetFollow: (() => void) | undefined
    function Harness() {
      const {
        scrollRef,
        contentRef,
        followOutput: follow,
        resetFollow: reset,
      } = useStickToBottom()
      followOutput = follow
      resetFollow = reset
      return (
        <div ref={scrollRef} data-testid="viewport">
          <div ref={contentRef}>stream output</div>
        </div>
      )
    }

    render(<Harness />)
    const viewport = screen.getByTestId('viewport')
    Object.defineProperty(viewport, 'scrollHeight', { configurable: true, value: 400 })
    fireEvent.wheel(viewport)

    resetFollow?.()
    followOutput?.()

    expect(scrollTo).toHaveBeenLastCalledWith({ top: 400, behavior: 'auto' })
  })

  it('uses a scroll viewport owned by the parent component', () => {
    const scrollTo = vi.fn()
    HTMLElement.prototype.scrollTo = scrollTo
    globalThis.ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    } as typeof ResizeObserver

    const externalScrollRef = createRef<HTMLDivElement>()
    let followOutput: (() => void) | undefined
    function Harness() {
      const { contentRef, followOutput: follow } = useStickToBottom({
        scrollRef: externalScrollRef,
      })
      followOutput = follow
      return (
        <div ref={externalScrollRef} data-testid="viewport">
          <div ref={contentRef}>stream output</div>
        </div>
      )
    }

    render(<Harness />)
    const viewport = screen.getByTestId('viewport')
    Object.defineProperty(viewport, 'scrollHeight', { configurable: true, value: 400 })

    followOutput?.()

    expect(scrollTo).toHaveBeenLastCalledWith({ top: 400, behavior: 'auto' })
  })

  it('detaches when a nested interaction asks to preserve the viewport', () => {
    const scrollTo = vi.fn()
    HTMLElement.prototype.scrollTo = scrollTo
    globalThis.ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    } as typeof ResizeObserver

    let followOutput: (() => void) | undefined
    let detachFollow: (() => void) | undefined
    function Harness() {
      const {
        scrollRef,
        contentRef,
        followOutput: follow,
        detachFollow: detach,
      } = useStickToBottom()
      followOutput = follow
      detachFollow = detach
      return (
        <div ref={scrollRef} data-testid="viewport">
          <div ref={contentRef}>stream output</div>
        </div>
      )
    }

    render(<Harness />)
    detachFollow?.()

    followOutput?.()

    expect(scrollTo).not.toHaveBeenCalled()
  })
})
