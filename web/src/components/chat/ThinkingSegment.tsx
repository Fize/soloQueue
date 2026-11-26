import { useState, useEffect, useRef } from 'react'
import { ChevronRight, ChevronDown } from 'lucide-react'
import { useChatStore } from '@/stores/chatStore'
import { useRuntimeStore } from '@/stores/runtimeStore'
import { useTranslation } from '@/lib/i18n'
import { LongMarkdown } from '@/components/ui/long-markdown'
import type { ChatMessage } from '@/types'

/**
 * One thinking batch. Batches are rendered inside their worked group, so the
 * group keeps its own collapse state and this one only adds a second level:
 * the batch that is still streaming is open, every finished batch collapses.
 *
 * The body mounts only while open: a finished batch of a long answer is
 * otherwise a large amount of markdown that nobody is reading.
 */
export function ThinkingSegment({
  segment,
  isLastSegment = true,
  isStreaming,
  nested = false,
  expanded = false,
  onUserInteraction,
}: {
  segment: Extract<ChatMessage['segments'][number], { type: 'thinking' }>
  isLastSegment?: boolean
  /** Stream state of the owning message; falls back to the active session. */
  isStreaming?: boolean
  /** Rendered inside a worked group, which already draws the indent guide. */
  nested?: boolean
  /** The reader opened the enclosing group, so every batch of it is open. */
  expanded?: boolean
  onUserInteraction?: () => void
}) {
  const streamingSessions = useChatStore((s) => s.streamingSessions)
  const activeSessionId = useChatStore((s) => s.activeSessionId)
  const sessionStreaming = activeSessionId ? !!streamingSessions[activeSessionId] : false
  const streaming = isStreaming ?? sessionStreaming
  const isDesignMode = useRuntimeStore((s) => s.isDesignMode)
  const compact = isDesignMode
  const { t } = useTranslation()

  // A thinking segment is done when:
  //   a) there are subsequent segments (LLM moved on to content/tool_call), OR
  //   b) it's the last segment but the global stream has ended
  const isDone = !isLastSegment || !streaming
  const autoOpen = !isDone || expanded

  const [isOpen, setIsOpen] = useState(autoOpen)
  const hasManuallyToggled = useRef(false)

  // Sync isOpen with the automatic state only if not manually toggled
  useEffect(() => {
    if (!hasManuallyToggled.current) {
      setIsOpen(autoOpen)
    }
  }, [autoOpen])

  // Reset manual toggle when active session changes
  useEffect(() => {
    hasManuallyToggled.current = false
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setIsOpen(autoOpen)
  }, [activeSessionId, autoOpen])

  return (
    <details className="group/thinking" open={isOpen} data-thinking-batch>
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
        <span className="font-medium">{t('chat.thinking')}</span>
        <ChevronRight className="h-3 w-3 ml-auto group-open/thinking:hidden" />
        <ChevronDown className="h-3 w-3 ml-auto hidden group-open/thinking:block" />
      </summary>
      {isOpen && (
        <div
          className={
            nested
              ? 'mt-1 text-xs text-muted-foreground/75'
              : `${compact ? 'mt-1 ml-3 pl-2' : 'mt-1 ml-5 pl-3'} border-l-2 text-xs whitespace-pre-wrap leading-relaxed border-muted-foreground/20 text-muted-foreground/75`
          }
        >
          <LongMarkdown content={segment.text} isAnimating={!isDone} />
        </div>
      )}
    </details>
  )
}
