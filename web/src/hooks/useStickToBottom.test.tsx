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

  it.each(['none', 'native', 'direct'])('keeps the bottom visible on viewport resize (early scroll: %s)', (earlyScroll) => {
    const callbacks = new Map<Element, () => void>()
    const disconnect = vi.fn()
    const scrollTo = vi.fn()
    HTMLElement.prototype.scrollTo = scrollTo
    globalThis.ResizeObserver = class {
      constructor(private callback: ResizeObserverCallback) {}
      observe(target: Element) {
        callbacks.set(target, () => this.callback([], this as unknown as ResizeObserver))
      }
      unobserve() {}
      disconnect = disconnect
    } as typeof ResizeObserver

    let syncFollowState: (() => void) | undefined
    function Harness() {
      const { scrollRef, contentRef, syncFollowState: sync } = useStickToBottom()
      syncFollowState = sync
      return <div ref={scrollRef} data-testid="viewport"><div ref={contentRef}>output</div></div>
    }

    const { unmount } = render(<Harness />)
    const viewport = screen.getByTestId('viewport')
    Object.defineProperties(viewport, {
      scrollHeight: { configurable: true, value: 1000 },
      clientHeight: { configurable: true, value: 400 },
      scrollTop: { configurable: true, writable: true, value: 600 },
    })
    callbacks.get(viewport)?.()
    scrollTo.mockClear()

    Object.defineProperty(viewport, 'clientHeight', { configurable: true, value: 200 })
    if (earlyScroll === 'native') fireEvent.scroll(viewport)
    if (earlyScroll === 'direct') syncFollowState?.()
    callbacks.get(viewport)?.()

    expect(callbacks.has(viewport)).toBe(true)
    expect(scrollTo).toHaveBeenLastCalledWith({ top: 1000, behavior: 'auto' })

    scrollTo.mockClear()
    fireEvent.wheel(viewport, { deltaY: -100 })
    viewport.scrollTop = 300
    fireEvent.scroll(viewport)
    Object.defineProperty(viewport, 'clientHeight', { configurable: true, value: 300 })
    callbacks.get(viewport)?.()
    expect(scrollTo).not.toHaveBeenCalled()
    expect(viewport.scrollTop).toBe(300)

    unmount()
    expect(disconnect).toHaveBeenCalledTimes(2)
  })

  it('observes and tracks a viewport mounted after the empty state', () => {
    const callbacks = new Map<Element, () => void>()
    const scrollTo = vi.fn()
    HTMLElement.prototype.scrollTo = scrollTo
    globalThis.ResizeObserver = class {
      constructor(private callback: ResizeObserverCallback) {}
      observe(target: Element) {
        callbacks.set(target, () => this.callback([], this as unknown as ResizeObserver))
      }
      unobserve() {}
      disconnect() {}
    } as typeof ResizeObserver

    function Harness({ empty }: { empty: boolean }) {
      const { scrollRef, contentRef } = useStickToBottom()
      return empty ? null : <div ref={scrollRef} data-testid="viewport"><div ref={contentRef}>output</div></div>
    }

    const { rerender } = render(<Harness empty />)
    rerender(<Harness empty={false} />)
    const viewport = screen.getByTestId('viewport')
    Object.defineProperties(viewport, {
      scrollHeight: { configurable: true, value: 1000 },
      clientHeight: { configurable: true, value: 400 },
      scrollTop: { configurable: true, writable: true, value: 600 },
    })
    callbacks.get(viewport)?.()
    expect(scrollTo).toHaveBeenLastCalledWith({ top: 1000, behavior: 'auto' })
    scrollTo.mockClear()
    fireEvent.wheel(viewport, { deltaY: -100 })
    Object.defineProperty(viewport, 'clientHeight', { configurable: true, value: 200 })
    callbacks.get(viewport)?.()
    expect(scrollTo).not.toHaveBeenCalled()
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

  it('keeps following after returning to the bottom and overscrolling downward', () => {
    const callbacks = new Map<Element, ResizeObserverCallback>()
    const scrollTo = vi.fn()
    HTMLElement.prototype.scrollTo = scrollTo
    globalThis.ResizeObserver = class {
      constructor(private callback: ResizeObserverCallback) {}
      observe(target: Element) {
        callbacks.set(target, this.callback)
      }
      unobserve() {}
      disconnect() {}
    } as typeof ResizeObserver

    function Harness() {
      const { scrollRef, contentRef } = useStickToBottom()
      return (
        <div ref={scrollRef} data-testid="viewport">
          <div ref={contentRef} data-testid="content">stream output</div>
        </div>
      )
    }

    render(<Harness />)
    const viewport = screen.getByTestId('viewport')
    const content = screen.getByTestId('content')
    Object.defineProperties(viewport, {
      scrollHeight: { configurable: true, value: 400 },
      clientHeight: { configurable: true, value: 100 },
      scrollTop: { configurable: true, writable: true, value: 0 },
    })

    fireEvent.scroll(viewport)
    viewport.scrollTop = 300
    fireEvent.scroll(viewport)
    fireEvent.wheel(viewport, { deltaY: 100 })

    Object.defineProperty(viewport, 'scrollHeight', { configurable: true, value: 500 })
    callbacks.get(content)?.([], {} as ResizeObserver)

    expect(scrollTo).toHaveBeenLastCalledWith({ top: 500, behavior: 'auto' })
  })

  it('keeps following after a touch starts while attached to the bottom', () => {
    const callbacks = new Map<Element, ResizeObserverCallback>()
    const scrollTo = vi.fn()
    HTMLElement.prototype.scrollTo = scrollTo
    globalThis.ResizeObserver = class {
      constructor(private callback: ResizeObserverCallback) {}
      observe(target: Element) {
        callbacks.set(target, this.callback)
      }
      unobserve() {}
      disconnect() {}
    } as typeof ResizeObserver

    function Harness() {
      const { scrollRef, contentRef } = useStickToBottom()
      return (
        <div ref={scrollRef} data-testid="viewport">
          <div ref={contentRef} data-testid="content">stream output</div>
        </div>
      )
    }

    render(<Harness />)
    const viewport = screen.getByTestId('viewport')
    const content = screen.getByTestId('content')
    Object.defineProperties(viewport, {
      scrollHeight: { configurable: true, value: 400 },
      clientHeight: { configurable: true, value: 100 },
      scrollTop: { configurable: true, writable: true, value: 300 },
    })

    fireEvent.scroll(viewport)
    fireEvent.touchStart(viewport)
    Object.defineProperty(viewport, 'scrollHeight', { configurable: true, value: 500 })
    callbacks.get(content)?.([], {} as ResizeObserver)

    expect(scrollTo).toHaveBeenLastCalledWith({ top: 500, behavior: 'auto' })
  })

  it('detaches immediately when the user wheels upward', () => {
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
    fireEvent.wheel(screen.getByTestId('viewport'), { deltaY: -100 })

    followOutput?.()

    expect(scrollTo).not.toHaveBeenCalled()
  })

  it('keeps a touch interaction detached while the viewport is away from the bottom', () => {
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
    fireEvent.touchStart(viewport)

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
