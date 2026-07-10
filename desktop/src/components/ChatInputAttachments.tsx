import { useTranslation } from '@/lib/i18n'
import { X, Loader2 } from 'lucide-react'

// ─── Types ───────────────────────────────────────────────────────────────────

export interface Attachment {
  id: string
  file: File
  name: string
  previewUrl: string
  status: 'uploading' | 'done' | 'failed'
  path?: string
  error?: string
}

// ─── Props ───────────────────────────────────────────────────────────────────

export interface ChatInputAttachmentsProps {
  attachments: Attachment[]
  selectedTarget?: {
    filePath: string
    selector: string
    text: string
    htmlHint: string
  } | null
  onClearSelectedTarget?: () => void
  onRemove: (id: string) => void
}

// ─── Component ───────────────────────────────────────────────────────────────

export function ChatInputAttachments({
  attachments,
  selectedTarget,
  onClearSelectedTarget,
  onRemove,
}: ChatInputAttachmentsProps) {
  const { t } = useTranslation()
  if (attachments.length === 0 && !selectedTarget) return null

  return (
    <div className="flex flex-wrap items-center gap-2 p-3 border-b border-border/40 bg-muted/5 rounded-t-xl">
      {attachments.map((att) => (
        <div
          key={att.id}
          className="relative group/thumb h-16 w-16 rounded-lg overflow-hidden border border-border bg-muted/30"
        >
          <img src={att.previewUrl} alt="preview" className="h-full w-full object-cover" />
          {att.status === 'uploading' && (
            <div className="absolute inset-0 bg-black/40 flex items-center justify-center">
              <Loader2 className="h-4 w-4 animate-spin text-white" />
            </div>
          )}
          {att.status === 'failed' && (
            <div
              className="absolute inset-0 bg-destructive/80 flex items-center justify-center"
              title={att.error}
            >
              <span className="text-[10px] text-white font-medium">{t('common.failed')}</span>
            </div>
          )}
          {/* Hover action bar: preview / copy / download / remove */}
          {att.status === 'done' && (
            <div className="absolute inset-0 bg-black/0 group-hover/thumb:bg-black/50 transition-all flex flex-col items-center justify-center gap-1 opacity-0 group-hover/thumb:opacity-100">
              {/* Open with system viewer */}
              <button
                title={t('common.openWithViewer')}
                onClick={() => {
                  if (att.path) {
                    // Electron: open file with system default app
                    const api = (window as any).electronAPI
                    if (api?.openPath) {
                      api.openPath(att.path)
                    } else {
                      window.open(att.previewUrl, '_blank')
                    }
                  }
                }}
                className="h-5 w-5 rounded bg-white/20 hover:bg-white/40 flex items-center justify-center text-white transition-colors"
              >
                <svg viewBox="0 0 16 16" fill="currentColor" className="h-3 w-3">
                  <path d="M6.5 1A1.5 1.5 0 005 2.5V3H2.5A1.5 1.5 0 001 4.5v8A1.5 1.5 0 002.5 14h11A1.5 1.5 0 0015 12.5v-8A1.5 1.5 0 0013.5 3H11v-.5A1.5 1.5 0 009.5 1h-3zm0 1h3a.5.5 0 01.5.5V3H6v-.5a.5.5 0 01.5-.5zm6.5 2a.5.5 0 01.5.5v.634l-4.5 2.25-4.5-2.25V4.5a.5.5 0 01.5-.5h8z"/>
                </svg>
              </button>
              {/* Copy to clipboard */}
              <button
                title={t('common.copyImage')}
                onClick={async () => {
                  try {
                    const res = await fetch(att.previewUrl)
                    const blob = await res.blob()
                    await navigator.clipboard.write([new ClipboardItem({ [blob.type]: blob })])
                  } catch {
                    // fallback: nothing
                  }
                }}
                className="h-5 w-5 rounded bg-white/20 hover:bg-white/40 flex items-center justify-center text-white transition-colors"
              >
                <svg viewBox="0 0 16 16" fill="currentColor" className="h-3 w-3">
                  <path d="M4 1.5H3a2 2 0 00-2 2V14a2 2 0 002 2h10a2 2 0 002-2V3.5a2 2 0 00-2-2h-1v1h1a1 1 0 011 1V14a1 1 0 01-1 1H3a1 1 0 01-1-1V3.5a1 1 0 011-1h1v-1z"/><path d="M9.5 1a.5.5 0 01.5.5v1a.5.5 0 01-.5.5h-3a.5.5 0 01-.5-.5v-1a.5.5 0 01.5-.5h3zm-3-1A1.5 1.5 0 005 1.5v1A1.5 1.5 0 006.5 4h3A1.5 1.5 0 0011 2.5v-1A1.5 1.5 0 009.5 0h-3z"/>
                </svg>
              </button>
              {/* Download */}
              <button
                title={t('common.download')}
                onClick={() => {
                  const a = document.createElement('a')
                  a.href = att.previewUrl
                  a.download = att.name
                  a.click()
                }}
                className="h-5 w-5 rounded bg-white/20 hover:bg-white/40 flex items-center justify-center text-white transition-colors"
              >
                <svg viewBox="0 0 16 16" fill="currentColor" className="h-3 w-3">
                  <path d="M.5 9.9a.5.5 0 01.5.5v2.1a1 1 0 001 1h12a1 1 0 001-1v-2.1a.5.5 0 011 0v2.1a2 2 0 01-2 2H2a2 2 0 01-2-2v-2.1a.5.5 0 01.5-.5z"/><path d="M7.646 11.854a.5.5 0 00.708 0l3-3a.5.5 0 00-.708-.708L8.5 10.293V1.5a.5.5 0 00-1 0v8.793L5.354 8.146a.5.5 0 10-.708.708l3 3z"/>
                </svg>
              </button>
            </div>
          )}
          {/* Remove button (always visible on hover, top-right) */}
          <button
            onClick={() => onRemove(att.id)}
            className="absolute top-1 right-1 h-4 w-4 rounded-full bg-black/60 hover:bg-black/80 flex items-center justify-center text-white opacity-0 group-hover/thumb:opacity-100 transition-opacity z-10"
            title={t('common.removeImage')}
          >
            <X className="h-2.5 w-2.5" />
          </button>
        </div>
      ))}

      {selectedTarget && (
        <div className="flex items-center gap-1.5 px-2.5 py-1 rounded-lg border border-primary/25 bg-primary/5 text-primary text-[11px] font-medium animate-in fade-in slide-in-from-left-2 duration-200 max-w-full min-w-0">
          <span className="font-semibold select-none flex-shrink-0">{'\u{1F310}'} {t('common.selectedDom')}</span>
          <code className="bg-primary/10 px-1 py-0.5 rounded text-[10px] font-mono max-w-[180px] min-w-0 truncate" title={selectedTarget.selector}>
            {selectedTarget.selector}
          </code>
          {selectedTarget.text && (
            <span className="text-muted-foreground truncate max-w-[120px] min-w-0" title={selectedTarget.text}>
              (&quot;{selectedTarget.text}&quot;)
            </span>
          )}
          <button
            onClick={(e) => {
              e.preventDefault();
              onClearSelectedTarget?.();
            }}
            className="p-0.5 hover:bg-primary/15 rounded-full text-primary transition-colors cursor-pointer flex-shrink-0"
            title={t('common.deselect')}
          >
            <X className="h-2.5 w-2.5" />
          </button>
        </div>
      )}
    </div>
  )
}
