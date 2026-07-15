import { useState } from 'react'
import { ShieldAlert, Loader2, Check, X } from 'lucide-react'
import { useChatStore } from '@/stores/chatStore'
import { confirmSessionTool } from '@/lib/api'
import { toast } from 'sonner'
import { useTranslation } from '@/lib/i18n'
import { Button } from '@/components/ui/button'
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
  const { t } = useTranslation()

  const handleConfirm = async (choice: string) => {
    if (!activeSessionId) return
    setSubmitting(true)
    try {
      await confirmSessionTool(activeSessionId, segment.callId, choice)
      resolveToolConfirm(activeSessionId, segment.callId, choice)
    } catch (err) {
      console.error('Failed to confirm tool:', err)
      toast.error(t('common.failedToConfirmTool'))
    } finally {
      setSubmitting(false)
    }
  }

  const resolved = segment.resolved
  const choice = segment.choice

  return (
    <div
      className={`p-4 rounded-xl border flex flex-col gap-3 text-xs leading-relaxed w-full max-w-md my-2 transition-colors ${
        resolved
          ? 'border-border/40 bg-muted/10 text-muted-foreground/80'
          : isUser
          ? 'border-primary-foreground/15 bg-primary-foreground/5 text-primary-foreground'
          : 'border-amber-500/25 bg-amber-500/5 text-foreground shadow-sm'
      }`}
    >
      {/* Title */}
      <div className="flex items-center gap-2">
        <ShieldAlert
          className={`h-4 w-4 shrink-0 ${
            resolved
              ? 'text-muted-foreground/60'
              : isUser
              ? 'text-primary-foreground'
              : 'text-amber-500'
          }`}
        />
        <span className="font-semibold uppercase tracking-wider text-[10px]">
          {t('common.executionPermissionRequired', { name: segment.name })}
        </span>
      </div>

      {/* Prompt / Message */}
      <pre
        className={`p-3 rounded-lg font-mono text-[11px] whitespace-pre-wrap break-all border overflow-x-auto max-h-[120px] overflow-y-auto ${
          isUser
            ? 'bg-primary-foreground/10 border-primary-foreground/10'
            : resolved
            ? 'bg-muted/30 border-border/20 text-muted-foreground/75'
            : 'bg-muted/40 border-border/40'
        }`}
      >
        {segment.prompt}
      </pre>

      {resolved ? (
        <div className="flex items-center gap-2 mt-1">
          {choice === '' ? (
            <>
              <div className="h-2 w-2 rounded-full bg-destructive" />
              <span className="font-medium text-destructive">{t('common.deniedByUser')}</span>
            </>
          ) : (
            <>
              <div className="h-2 w-2 rounded-full bg-success" />
              <span className="font-medium text-success">
                {t('common.approved')}{' '}
                {choice === 'allow-in-session' ? `(${t('common.alwaysAllowInChat')})` : ''}
              </span>
            </>
          )}
        </div>
      ) : (
        <div className="flex flex-col gap-3 mt-1">
          {/* Action buttons */}
          <div className="flex items-center gap-2">
            <Button
              variant="success"
              size="xs"
              onClick={() => handleConfirm('yes')}
              disabled={submitting}
              className="cursor-pointer"
            >
              {submitting ? (
                <Loader2 className="h-3 w-3 animate-spin" />
              ) : (
                <Check className="h-3 w-3" />
              )}
              {t('common.approve')}
            </Button>
            {segment.allowInSession && (
              <Button
                variant="outline"
                size="xs"
                onClick={() => handleConfirm('allow-in-session')}
                disabled={submitting}
                className={`cursor-pointer ${
                  isUser
                    ? 'border-primary-foreground/25 hover:bg-primary-foreground/10 text-primary-foreground'
                    : 'border-border hover:bg-muted text-foreground'
                }`}
              >
                <Check className="h-3 w-3" />
                {t('common.approveInSession')}
              </Button>
            )}
            <Button
              variant="outline"
              size="xs"
              onClick={() => handleConfirm('')}
              disabled={submitting}
              className={`cursor-pointer text-destructive border-border hover:bg-destructive/10 hover:text-destructive hover:border-destructive/20 ${
                isUser ? 'border-primary-foreground/25 text-primary-foreground hover:bg-primary-foreground/10' : ''
              }`}
            >
              <X className="h-3 w-3" />
              {t('common.deny')}
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}

