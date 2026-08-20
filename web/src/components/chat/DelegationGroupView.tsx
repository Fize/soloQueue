import { useState, useEffect, useRef } from 'react'
import { ChevronDown, Loader2, Check } from 'lucide-react'
import { cn } from '@/lib/utils'
import { SegmentView } from './SegmentView'
import type { GroupedDelegation } from '../ChatMessage'
import type { ChatMessage } from '@/types'

export function DelegationGroupView({
  group,
  isUser,
  segments,
  isStreaming,
  onUserInteraction,
}: {
  group: GroupedDelegation
  isUser?: boolean
  segments?: ChatMessage['segments']
  isStreaming?: boolean
  onUserInteraction?: () => void
}) {
  const isRunning = group.segments.some((s) => {
    if (s.segment.type === 'tool_call') {
      return !s.segment.done
    } else if (s.segment.type === 'delegation') {
      return (s.segment as any).status === 'running'
    }
    return false
  })

  const [isExpanded, setIsExpanded] = useState(isRunning)
  const [userToggled, setUserToggled] = useState(false)
  const previousRunningRef = useRef<boolean>(isRunning)

  useEffect(() => {
    // If we transition from running (true) to done (false), and the user hasn't manually opened/closed it, auto collapse.
    if (previousRunningRef.current === true && isRunning === false) {
      if (!userToggled) {
        setIsExpanded(false)
      }
    }
    previousRunningRef.current = isRunning
  }, [isRunning, userToggled])

  const handleToggle = () => {
    setIsExpanded(!isExpanded)
    setUserToggled(true)
    onUserInteraction?.()
  }

  const numTasks = group.segments.length
  const title = `Delegated ${numTasks} task${numTasks !== 1 ? 's' : ''}...`

  return (
    <div className="my-2 rounded-[12px] border border-border/40 bg-card overflow-hidden">
      <button
        onClick={handleToggle}
        className={cn(
          'w-full flex items-center justify-between px-3 py-2.5 text-left transition-colors hover:bg-muted/40 outline-none focus-visible:ring-2 focus-visible:ring-primary/20',
          isExpanded ? 'border-b border-border/40 bg-muted/20' : ''
        )}
      >
        <div className="flex items-center gap-2.5">
          {isRunning ? (
            <Loader2 className="h-4 w-4 animate-spin text-signal" />
          ) : (
            <Check className="h-4 w-4 text-success" />
          )}
          <span className="text-xs font-medium text-foreground/80">{title}</span>
        </div>
        <ChevronDown
          className={cn(
            'h-4 w-4 text-muted-foreground/50 transition-transform duration-200',
            isExpanded ? 'rotate-180' : ''
          )}
        />
      </button>

      {isExpanded && (
        <div className="p-1.5 bg-muted/10">
          {group.segments.map((s) => (
            <SegmentView
              key={s.originalIndex}
              segment={s.segment}
              isUser={isUser}
              segmentIndex={s.originalIndex}
              segments={segments}
              isStreaming={isStreaming}
              onUserInteraction={onUserInteraction}
            />
          ))}
        </div>
      )}
    </div>
  )
}
