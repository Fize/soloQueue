import { useCallback, useEffect, useRef } from 'react'
import type { RefObject } from 'react'

interface UseStickToBottomOptions {
  scrollRef?: RefObject<HTMLDivElement | null>
}

export function useStickToBottom(options?: UseStickToBottomOptions) {
  const internalScrollRef = useRef<HTMLDivElement>(null)
  const scrollRef = options?.scrollRef ?? internalScrollRef
  const activeScrollRef = useRef(scrollRef)
  const resizeObserverRef = useRef<ResizeObserver | null>(null)
  const isAttachedRef = useRef(true)
  const viewportHeightRef = useRef(0)

  const followOutput = useCallback(() => {
    const viewport = activeScrollRef.current.current
    if (!viewport || !isAttachedRef.current) return
    viewport.scrollTo({ top: viewport.scrollHeight, behavior: 'auto' })
  }, [])

  const resetFollow = useCallback(() => {
    isAttachedRef.current = true
  }, [])

  const detachFollow = useCallback(() => {
    isAttachedRef.current = false
  }, [])

  const syncFollowState = useCallback(() => {
    const viewport = activeScrollRef.current.current
    if (!viewport) return
    const resized = viewportHeightRef.current > 0 && viewportHeightRef.current !== viewport.clientHeight
    viewportHeightRef.current = viewport.clientHeight
    // A shrinking composer viewport can emit scroll before ResizeObserver.
    // Preserve bottom attachment until its new geometry has been followed.
    if (resized && isAttachedRef.current) {
      followOutput()
      return
    }
    const distanceFromBottom = viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight
    if (distanceFromBottom > 48) {
      detachFollow()
    } else {
      resetFollow()
    }
  }, [detachFollow, followOutput, resetFollow])

  const contentRef = useCallback((content: HTMLDivElement | null) => {
    resizeObserverRef.current?.disconnect()
    resizeObserverRef.current = null
    if (!content) return

    const observer = new ResizeObserver(() => {
      followOutput()
    })
    observer.observe(content)
    resizeObserverRef.current = observer
  }, [followOutput])

  useEffect(() => {
    const viewport = scrollRef.current
    if (!viewport) return

    viewportHeightRef.current = viewport.clientHeight
    const viewportObserver = new ResizeObserver(() => {
      viewportHeightRef.current = viewport.clientHeight
      followOutput()
    })
    viewportObserver.observe(viewport)

    const handleWheel = (event: WheelEvent) => {
      if (event.deltaY < 0) {
        detachFollow()
        return
      }
      // A downward wheel at the bottom can be an overscroll, which produces no
      // scroll event. Reconcile from the current geometry so it cannot leave a
      // bottom-attached viewport detached from later streamed output.
      syncFollowState()
    }
    const handleTouchStart = () => {
      // Touch direction is not known until scrolling begins. Reconcile the
      // current geometry now; the scroll event will detach once the viewport
      // actually moves away from the bottom.
      syncFollowState()
    }
    viewport.addEventListener('scroll', syncFollowState, { passive: true })
    viewport.addEventListener('wheel', handleWheel, { passive: true })
    viewport.addEventListener('touchstart', handleTouchStart, { passive: true })

    return () => {
      viewportObserver.disconnect()
      viewport.removeEventListener('scroll', syncFollowState)
      viewport.removeEventListener('wheel', handleWheel)
      viewport.removeEventListener('touchstart', handleTouchStart)
    }
    // The viewport is conditionally mounted after the empty chat state, so its
    // ref can change while all hook dependencies remain the same.
  })

  return { scrollRef, contentRef, followOutput, resetFollow, detachFollow, syncFollowState }
}
