import { useState } from 'react'
import { ShieldAlert, Loader2, Check, X } from 'lucide-react'
import { useChatStore } from '@/stores/chatStore'
import { confirmSessionTool } from '@/lib/api'
import { toast } from 'sonner'
import type { ChatMessage } from '@/types'

export function ToolConfirmSegment({
  segment,
  isUser,
}: {
  segment: Extract<ChatMessage['segments'][number], { type: 'tool_confirm' }>
  isUser?: boolean
}) {
  const activeSessionId = useChatStore((s) => s.activeSessionId)
  const resolveToolConfirm = useChatStore((s) => s.resolveToolConfirm)
  const [submitting, setSubmitting] = useState(false)

  const handleConfirm = async (choice: string) => {
    if (!activeSessionId) return
    setSubmitting(true)
    try {
      await confirmSessionTool(activeSessionId, segment.callId, choice)
      resolveToolConfirm(activeSessionId, segment.callId, choice)
    } catch (err) {
      console.error('Failed to confirm tool:', err)
      toast.error('Failed to confirm tool')
    } finally {
      setSubmitting(false)
    }
  }

  const resolved = segment.resolved
  const choice = segment.choice

  return (
    <div
      className={`p-4 rounded-xl border flex flex-col gap-3 text-xs leading-relaxed w-full max-w-md my-2 ${
        isUser
          ? 'border-primary-foreground/15 bg-primary-foreground/5 text-primary-foreground'
          : 'border-amber-500/25 bg-amber-500/5 text-foreground'
      }`}
    >
      {/* Title */}
      <div className="flex items-center gap-2">
        <ShieldAlert
          className={`h-4 w-4 shrink-0 ${isUser ? 'text-primary-foreground' : 'text-amber-500'}`}
        />
        <span className="font-semibold uppercase tracking-wider text-[10px]">
          Execution Permission Required ({segment.name})
        </span>
      </div>

      {/* Prompt / Message */}
      <div
        className={`p-3 rounded-lg font-mono text-[11px] whitespace-pre-wrap break-words ${
          isUser ? 'bg-primary-foreground/10' : 'bg-muted/40'
        }`}
      >
        {segment.prompt}
      </div>

      {resolved ? (
        <div className="flex items-center gap-2 mt-1">
          {choice === '' ? (
            <>
              <div className="h-2 w-2 rounded-full bg-destructive" />
              <span className="font-medium text-destructive">Denied by user</span>
            </>
          ) : (
            <>
              <div className="h-2 w-2 rounded-full bg-success" />
              <span className="font-medium text-success">
                Approved {choice === 'allow-in-session' ? '(Always allow in this chat)' : ''}
              </span>
            </>
          )}
        </div>
      ) : (
        <div className="flex flex-col gap-3 mt-1">
          {/* Action buttons */}
          <div className="flex items-center gap-2">
            <button
              onClick={() => handleConfirm('yes')}
              disabled={submitting}
              className="px-3 py-1.5 rounded-lg font-medium bg-success text-white hover:bg-success disabled:opacity-50 transition-colors flex items-center gap-1 cursor-pointer"
            >
              {submitting ? (
                <Loader2 className="h-3 w-3 animate-spin" />
              ) : (
                <Check className="h-3.5 w-3.5" />
              )}
              Approve
            </button>
            {segment.allowInSession && (
              <button
                onClick={() => handleConfirm('allow-in-session')}
                disabled={submitting}
                className={`px-3 py-1.5 rounded-lg font-medium border disabled:opacity-50 transition-colors flex items-center gap-1 cursor-pointer ${
                  isUser
                    ? 'border-primary-foreground/25 hover:bg-primary-foreground/10 text-primary-foreground'
                    : 'border-border hover:bg-muted text-foreground'
                }`}
              >
                <Check className="h-3.5 w-3.5" />
                Approve in Session
              </button>
            )}
            <button
              onClick={() => handleConfirm('')}
              disabled={submitting}
              className={`px-3 py-1.5 rounded-lg font-medium border disabled:opacity-50 transition-colors flex items-center gap-1 cursor-pointer ${
                isUser
                  ? 'border-primary-foreground/25 hover:bg-primary-foreground/10 text-primary-foreground'
                  : 'border-border hover:bg-muted text-foreground'
              }`}
            >
              <X className="h-3.5 w-3.5" />
              Deny
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
