import { useState, useEffect, useRef } from 'react'
import { ChevronRight, ChevronDown } from 'lucide-react'
import { useChatStore } from '@/stores/chatStore'
import { useRuntimeStore } from '@/stores/runtimeStore'
import type { ChatMessage } from '@/types'

export function ThinkingSegment({
  segment,
  isLastSegment = true,
  onUserInteraction,
}: {
  segment: Extract<ChatMessage['segments'][number], { type: 'thinking' }>
  isLastSegment?: boolean
  onUserInteraction?: () => void
}) {
  const streamingSessions = useChatStore((s) => s.streamingSessions)
  const activeSessionId = useChatStore((s) => s.activeSessionId)
  const streaming = activeSessionId ? !!streamingSessions[activeSessionId] : false
  const isDesignMode = useRuntimeStore((s) => s.isDesignMode)
  const compact = isDesignMode

  // A thinking segment is done when:
  //   a) there are subsequent segments (LLM moved on to content/tool_call), OR
  //   b) it's the last segment but the global stream has ended
  const isDone = !isLastSegment || (isLastSegment && !streaming)

  const [isOpen, setIsOpen] = useState(!isDone)
  const hasManuallyToggled = useRef(false)

  // Sync isOpen with isDone only if not manually toggled
  useEffect(() => {
    if (!hasManuallyToggled.current) {
      setIsOpen(!isDone)
    }
  }, [isDone])

  // Reset manual toggle when active session changes
  useEffect(() => {
    hasManuallyToggled.current = false
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setIsOpen(!isDone)
  }, [activeSessionId, isDone])

  return (
    <details className="group/thinking" open={isOpen}>
      <summary
        onClick={(e) => {
          e.preventDefault()
          hasManuallyToggled.current = true
          setIsOpen(!isOpen)
          onUserInteraction?.()
        }}
        className={`flex items-center gap-1.5 text-xs cursor-pointer transition-colors ${compact ? 'py-0.5' : 'py-1'} text-muted-foreground hover:text-foreground/70`}
      >
        {!isDone ? (
          <span className="relative flex h-2 w-2 shrink-0">
            <span
              className={`absolute inline-flex h-full w-full rounded-full opacity-75 animate-ping bg-primary`}
            />
            <span
              className={`relative inline-flex h-2 w-2 rounded-full bg-primary`}
            />
          </span>
        ) : (
          <div className="h-2 w-2 rounded-full bg-success/30 shrink-0 flex items-center justify-center">
            <div className="h-1 w-1 rounded-full bg-success" />
          </div>
        )}
        <span className="font-medium">thinking</span>
        <ChevronRight className="h-3 w-3 ml-auto group-open/thinking:hidden" />
        <ChevronDown className="h-3 w-3 ml-auto hidden group-open/thinking:block" />
      </summary>
      <div
        className={`${compact ? 'mt-1 ml-3 pl-2' : 'mt-1 ml-5 pl-3'} border-l-2 text-xs whitespace-pre-wrap leading-relaxed border-muted-foreground/20 text-muted-foreground/75`}
      >
        {segment.text}
      </div>
    </details>
  )
}
