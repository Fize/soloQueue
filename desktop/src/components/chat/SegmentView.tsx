import { AlertCircle } from 'lucide-react'
import { MarkdownPreview } from '@/components/ui/markdown-preview'
import { DelegationCard } from '@/components/DelegationCard'
import { ThinkingSegment } from './ThinkingSegment'
import { ToolCallSegment } from './ToolCallSegment'
import { ToolConfirmSegment } from './ToolConfirmSegment'
import { SubagentCard } from './WorkedSegment'
import { useTranslation } from '@/lib/i18n'
import type { ChatMessage } from '@/types'

export function LoadingIndicator() {
  const { t } = useTranslation()
  return (
    <div className="flex items-center gap-2 py-1">
      <span className="inline-flex gap-0.5">
        <span
          className={`h-1.5 w-1.5 rounded-full animate-bounce [animation-delay:0ms] bg-primary`}
        />
        <span
          className={`h-1.5 w-1.5 rounded-full animate-bounce [animation-delay:150ms] bg-primary`}
        />
        <span
          className={`h-1.5 w-1.5 rounded-full animate-bounce [animation-delay:300ms] bg-primary`}
        />
      </span>
      <span
        className={`text-sm text-muted-foreground/60`}
      >
        {t('chat.thinking')}
      </span>
    </div>
  )
}

function CompactSegment({ text }: { text: string }) {
  const { t } = useTranslation()
  return (
    <div className="text-xs text-muted-foreground/50 italic py-0.5">
      {t('common.contextCompacted')} {text}
    </div>
  )
}

export function SegmentView({
  segment,
  isUser,
  segmentIndex,
  segments,
  onUserInteraction,
}: {
  segment: ChatMessage['segments'][number]
  isUser?: boolean
  segmentIndex?: number
  segments?: ChatMessage['segments']
  onUserInteraction?: () => void
}) {
  const isLastSegment =
    segmentIndex != null && segments != null && segmentIndex === segments.length - 1
  switch (segment.type) {
    case 'content':
      return (
        <MarkdownPreview
          content={segment.text}
          className=""
        />
      )
    case 'compact':
      return (
        <CompactSegment
          text={segment.text}
        />
      )
    case 'thinking':
      return (
        <ThinkingSegment
          segment={segment}
          isLastSegment={isLastSegment}
          onUserInteraction={onUserInteraction}
        />
      )
    case 'tool_call':
      if (segment.name.startsWith('delegate_')) {
        const teamName = segment.name.substring(9).replace(/_/g, ' ')
        return (
          <DelegationCard
            name={teamName}
            args={segment.args}
            callId={segment.callId}
            done={segment.done}
            result={segment.result}
            error={segment.error}
            durationMs={segment.durationMs}
            agentInstanceId={segment.agentInstanceId}
          />
        )
      }
      if (segment.name === 'request_team_help') {
        let teamName = 'peer team'
        try {
          const args = JSON.parse(segment.args)
          if (args.team_name) teamName = args.team_name
        } catch {
          // ignore parse errors
        }
        return (
          <DelegationCard
            name={teamName}
            args={segment.args}
            callId={segment.callId}
            done={segment.done}
            result={segment.result}
            error={segment.error}
            durationMs={segment.durationMs}
          />
        )
      }
      return (
        <ToolCallSegment
          segment={segment}
          isUser={isUser}
          onUserInteraction={onUserInteraction}
        />
      )
    case 'tool_confirm':
      return <ToolConfirmSegment segment={segment} isUser={isUser} />
    case 'delegation':
      return <SubagentCard segment={segment} />
    case 'error':
      return (
        <div
          className={`flex items-start gap-2 p-3 rounded-lg text-sm ${isUser ? 'bg-destructive/20 text-destructive-foreground' : 'bg-destructive/5 border border-destructive/20 text-destructive/90'}`}
        >
          <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
          <span className="whitespace-pre-wrap">{segment.text}</span>
        </div>
      )
    default:
      return null
  }
}
