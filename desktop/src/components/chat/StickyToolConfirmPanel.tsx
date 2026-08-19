import { useState, useEffect } from 'react'
import { ShieldAlert, Loader2, Check, X } from 'lucide-react'
import { useChatStore } from '@/stores/chatStore'
import { confirmSessionTool } from '@/lib/api'
import { toast } from 'sonner'
import { useTranslation } from '@/lib/i18n'
import { Button } from '@/components/ui/button'
import { GlassCard } from '@/components/ui/glass-card'
import type { ChatSegment } from '@/types'

export function StickyToolConfirmPanel({
  pendingConfirm,
}: {
  pendingConfirm: Extract<ChatSegment, { type: 'tool_confirm' }>
}) {
  const activeSessionId = useChatStore((s) => s.activeSessionId)
  const resolveToolConfirm = useChatStore((s) => s.resolveToolConfirm)
  const [submitting, setSubmitting] = useState(false)
  const { t } = useTranslation()

  const handleConfirm = async (choice: string) => {
    if (!activeSessionId) return
    setSubmitting(true)
    try {
      await confirmSessionTool(activeSessionId, pendingConfirm.callId, choice)
      resolveToolConfirm(activeSessionId, pendingConfirm.callId, choice)
    } catch (err) {
      console.error('Failed to confirm tool:', err)
      toast.error(err instanceof Error && err.message ? err.message : t('common.failedToConfirmTool'))
    } finally {
      setSubmitting(false)
    }
  }

  // Keyboard shortcut listener
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // Ignore keyboard shortcuts if user is typing in any input, textarea, or contenteditable element
      if (
        document.activeElement &&
        (document.activeElement.tagName === 'INPUT' ||
          document.activeElement.tagName === 'TEXTAREA' ||
          document.activeElement.getAttribute('contenteditable') === 'true')
      ) {
        return
      }

      const key = e.key.toLowerCase()
      if (key === 'y' || e.key === 'Enter') {
        e.preventDefault()
        handleConfirm('yes')
      } else if (key === 'n' || e.key === 'Escape') {
        e.preventDefault()
        handleConfirm('')
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [activeSessionId, pendingConfirm.callId, handleConfirm])

  return (
    <div className="mx-auto w-full max-w-3xl px-4 py-2 animate-in fade-in slide-in-from-bottom-2 duration-200">
      <GlassCard
        variant="default"
        size="none"
        className="relative overflow-hidden border-amber-500/30 bg-gradient-to-br from-amber-500/10 via-background/80 to-background/95 shadow-xl rounded-xl p-4 flex flex-col gap-3"
      >
        {/* Glow indicator decoration */}
        <div className="absolute top-0 left-0 right-0 h-[2px] bg-gradient-to-r from-amber-500/40 via-amber-500 to-amber-500/40 animate-pulse" />

        {/* Header */}
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <div className="p-1.5 rounded-lg bg-amber-500/10 text-amber-500 animate-pulse shrink-0">
              <ShieldAlert className="h-4 w-4" />
            </div>
            <div>
              <h4 className="text-xs font-bold text-foreground font-mono uppercase tracking-wider">
                {t('common.executionPermissionRequired', { name: pendingConfirm.name })}
              </h4>
              <p className="text-[10px] text-muted-foreground mt-0.5">
                The agent requires authorization to proceed.
              </p>
            </div>
          </div>

          {/* Keyboard hints */}
          <div className="hidden sm:flex items-center gap-1.5 text-[9px] font-mono text-muted-foreground/60 select-none bg-muted/40 px-2 py-0.5 rounded-md">
            <span>[Enter/y] Approve</span>
            <span className="text-muted-foreground/20">•</span>
            <span>[Esc/n] Deny</span>
          </div>
        </div>

        {/* Prompt / Command content */}
        <div className="relative group">
          <pre className="text-[11px] leading-relaxed whitespace-pre-wrap break-all overflow-x-auto rounded-lg p-3 max-h-[160px] overflow-y-auto font-mono bg-muted/60 text-foreground border border-border/40">
            {pendingConfirm.prompt}
          </pre>
        </div>

        {/* Actions bar */}
        <div className="flex items-center justify-between mt-1 pt-2 border-t border-border/10">
          <div className="flex items-center gap-2">
            <Button
              variant="success"
              size="sm"
              onClick={() => handleConfirm('yes')}
              disabled={submitting}
              className="font-semibold cursor-pointer"
            >
              {submitting ? (
                <Loader2 className="h-3 w-3 animate-spin" />
              ) : (
                <Check className="h-3.5 w-3.5" />
              )}
              {t('common.approve')}
            </Button>
            {pendingConfirm.allowInSession && (
              <Button
                variant="outline"
                size="sm"
                onClick={() => handleConfirm('allow-in-session')}
                disabled={submitting}
                className="font-semibold border-border hover:bg-muted text-foreground cursor-pointer"
              >
                <Check className="h-3.5 w-3.5" />
                {t('common.approveInSession')}
              </Button>
            )}
          </div>

          <Button
            variant="outline"
            size="sm"
            onClick={() => handleConfirm('')}
            disabled={submitting}
            className="font-semibold text-destructive hover:bg-destructive/10 hover:text-destructive hover:border-destructive/20 border-border cursor-pointer"
          >
            <X className="h-3.5 w-3.5" />
            {t('common.deny')}
          </Button>
        </div>
      </GlassCard>
    </div>
  )
}
