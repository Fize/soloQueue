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
    const distanceFromBottom = viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight
    if (distanceFromBottom > 48) {
      detachFollow()
    } else {
      resetFollow()
    }
  }, [detachFollow, resetFollow])

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

    const handleWheel = () => {
      detachFollow()
    }
    const handleTouchStart = () => {
      detachFollow()
    }
    viewport.addEventListener('scroll', syncFollowState, { passive: true })
    viewport.addEventListener('wheel', handleWheel, { passive: true })
    viewport.addEventListener('touchstart', handleTouchStart, { passive: true })

    return () => {
      viewport.removeEventListener('scroll', syncFollowState)
      viewport.removeEventListener('wheel', handleWheel)
      viewport.removeEventListener('touchstart', handleTouchStart)
    }
  }, [detachFollow, scrollRef, syncFollowState])

  return { scrollRef, contentRef, followOutput, resetFollow, detachFollow, syncFollowState }
}
