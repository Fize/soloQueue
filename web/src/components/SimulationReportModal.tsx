import { MarkdownPreview as ReactMarkdown } from '@/components/ui/markdown-preview'
import { FileText, X } from 'lucide-react'
import {
  Dialog,
  DialogContent,
} from '@/components/ui/dialog'

import { useTranslation } from '@/lib/i18n'

interface SimulationReportModalProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  report: string | undefined
  topic: string | undefined
}

export function SimulationReportModal({
  open,
  onOpenChange,
  report,
  topic,
}: SimulationReportModalProps) {
  const { t } = useTranslation()
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        showCloseButton={false}
        className="max-w-[1000px] w-[80vw] h-[80vh] flex flex-col p-0 overflow-hidden bg-card border border-border rounded-xl"
      >
        <div className="flex flex-col h-full">
          {/* Header */}
          <div className="shrink-0 flex items-center justify-between px-6 py-4 border-b border-border/50">
            <div className="flex items-center gap-3">
              <FileText className="h-5 w-5 text-primary" />
              <h2 className="text-sm font-bold text-foreground">{t('simulation.reportTitle')}</h2>
              {topic && (
                <span className="text-xs text-muted-foreground font-mono truncate max-w-[300px]">
                  {topic}
                </span>
              )}
            </div>
            <button
              onClick={() => onOpenChange(false)}
              className="rounded-lg p-1.5 text-muted-foreground hover:text-foreground hover:bg-muted transition-colors cursor-pointer"
            >
              <X className="h-4 w-4" />
            </button>
          </div>

          {/* Report content */}
          <div className="flex-1 overflow-y-auto p-8 min-h-0 scroll-container select-text">
            <div className="max-w-3xl mx-auto">
              <div className="prose prose-sm dark:prose-invert max-w-none text-foreground/90 leading-relaxed font-sans">
                <ReactMarkdown>{report || ''}</ReactMarkdown>
              </div>
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
