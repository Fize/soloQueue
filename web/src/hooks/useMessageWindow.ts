import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import type { RefObject } from 'react'

/** Messages rendered on first paint, counted from the end of the transcript. */
export const INITIAL_VISIBLE_MESSAGES = 20
/** Messages mounted per idle slice. */
export const MOUNT_BATCH = 5
/** Set to false to render every message immediately. */
export const STAGGERED_MOUNT = true

interface UseMessageWindowOptions {
  /** Messages in the transcript. */
  total: number
  /** Identity of the open session; a change restarts the window. */
  sessionId?: string | null
  /** Chat viewport, used to hold the reading position while older messages mount. */
  scrollRef?: RefObject<HTMLDivElement | null>
  /** True when the viewport is detached from the bottom. */
  keepPosition?: boolean
}

/** Keep the block the reader is looking at in place after content is inserted above. */
function holdReadingPosition(viewport: HTMLDivElement, previousHeight: number) {
  const delta = viewport.scrollHeight - previousHeight
  if (delta > 0) viewport.scrollTop += delta
}

/**
 * Bounds how many messages render at once.
 *
 * A long session holds many large messages; mounting all of them parses every
 * markdown body in one commit and freezes the page. The newest messages render
 * immediately and the rest mount during idle slices, so the page stays
 * responsive while history fills in.
 */
export function useMessageWindow({ total, sessionId, scrollRef, keepPosition }: UseMessageWindowOptions) {
  const [count, setCount] = useState(() => Math.min(total, INITIAL_VISIBLE_MESSAGES))
  const [windowSession, setWindowSession] = useState(sessionId)
  const [windowTotal, setWindowTotal] = useState(total)
  const pendingHeight = useRef<number | null>(null)
  const totalRef = useRef(total)
  useEffect(() => {
    totalRef.current = total
  }, [total])

  // Derived during render so opening a session never paints the previous
  // session's window first.
  if (windowSession !== sessionId) {
    setWindowSession(sessionId)
    setWindowTotal(total)
    setCount(Math.min(total, INITIAL_VISIBLE_MESSAGES))
  } else if (windowTotal !== total) {
    setWindowTotal(total)
    // A transcript that grows while all of it is already rendered must not drop
    // the oldest messages; widen the window instead of recentering it.
    if (count < total && count >= total - INITIAL_VISIBLE_MESSAGES) setCount(total)
  }

  useEffect(() => {
    if (!STAGGERED_MOUNT || count >= total) return
    let cancelled = false
    const grow = () => {
      if (cancelled) return
      pendingHeight.current = scrollRef?.current?.scrollHeight ?? null
      setCount((current) => Math.min(total, current + MOUNT_BATCH))
    }
    const handle =
      typeof window.requestIdleCallback === 'function'
        ? window.requestIdleCallback(grow, { timeout: 250 })
        : window.setTimeout(grow, 50)
    return () => {
      cancelled = true
      if (typeof window.cancelIdleCallback === 'function') window.cancelIdleCallback(handle)
      else window.clearTimeout(handle)
    }
  }, [count, total, scrollRef])

  useLayoutEffect(() => {
    const viewport = scrollRef?.current
    const previous = pendingHeight.current
    pendingHeight.current = null
    if (!viewport || previous === null || !keepPosition) return
    holdReadingPosition(viewport, previous)
  }, [count, keepPosition, scrollRef])

  /** Show the whole transcript now, used after an explicit request for history. */
  const includeAll = useCallback(() => setCount(totalRef.current), [])

  const visibleCount = STAGGERED_MOUNT ? Math.min(count, total) : total
  return { visibleCount, includeAll }
}
