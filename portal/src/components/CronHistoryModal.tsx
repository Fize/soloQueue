import { useState, useEffect, useRef, useCallback } from 'react'
import { X, Clock, CheckCircle2, XCircle, AlertTriangle, Loader2 } from 'lucide-react'

interface ExecutionRecord {
  id: string
  task_id: string
  executed_at: string
  completed_at: string
  duration_ms: number
  status: string
  result_summary: string
  error_message: string
  task_type: string
  target_agent: string
  model_id: string
  provider_id: string
}

interface CronHistoryModalProps {
  taskId: string
  taskTitle: string
  onClose: () => void
  t: (key: string, v?: Record<string, string | number>) => string
}

export function CronHistoryModal({ taskId, taskTitle, onClose, t }: CronHistoryModalProps) {
  const overlayRef = useRef<HTMLDivElement>(null)
  const [records, setRecords] = useState<ExecutionRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [closing, setClosing] = useState(false)

  useEffect(() => {
    let cancelled = false
    fetch(`/api/cron/${taskId}/history?limit=20`)
      .then(r => { if (!r.ok) throw new Error(`HTTP ${r.status}`); return r.json() })
      .then(data => { if (!cancelled) { setRecords(data); setLoading(false) } })
      .catch(err => { if (!cancelled) { setError(String(err)); setLoading(false) } })
    return () => { cancelled = true }
  }, [taskId])

  // Lock body scroll
  useEffect(() => {
    document.body.classList.add('modal-open')
    return () => document.body.classList.remove('modal-open')
  }, [])

  // Close on ESC
  useEffect(() => {
    const handler = (e: KeyboardEvent) => { if (e.key === 'Escape') handleClose() }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  })

  const handleClose = useCallback(() => {
    setClosing(true)
    setTimeout(() => onClose(), 150)
  }, [onClose])

  const handleOverlayClick = (e: React.MouseEvent) => {
    if (e.target === overlayRef.current) handleClose()
  }

  return (
    <div
      ref={overlayRef}
      onClick={handleOverlayClick}
      className={`fixed inset-0 z-[100] flex items-center justify-center p-4 sm:p-6 ${closing ? 'modal-overlay-exiting' : 'modal-overlay-entering'}`}
      style={{ backgroundColor: 'rgba(0,0,0,0.6)', backdropFilter: 'blur(4px)' }}
    >
      <div
        className={`w-full max-w-2xl max-h-[80vh] flex flex-col rounded-xl overflow-hidden shadow-2xl ${closing ? 'modal-content-exiting' : 'modal-content-entering'}`}
        style={{ backgroundColor: 'var(--color-card)' }}
        onClick={e => e.stopPropagation()}
      >
        {/* Header with task title + close button */}
        <div className="px-5 py-4 border-b flex items-center justify-between shrink-0" style={{ borderColor: 'var(--color-border)' }}>
          <h2 className="text-base font-bold" style={{ color: 'var(--color-foreground)' }}>{t('cronHistory.title', { name: taskTitle })}</h2>
          <button onClick={handleClose} className="shrink-0 w-8 h-8 rounded-lg flex items-center justify-center cursor-pointer"
            style={{ color: 'var(--color-muted-foreground)' }}><X className="h-5 w-5" /></button>
        </div>
        {/* Scrollable body with records table */}
        <div className="flex-1 overflow-y-auto">
          {loading ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="h-6 w-6 animate-spin" style={{ color: 'var(--color-muted-foreground)' }} />
            </div>
          ) : error ? (
            <div className="flex flex-col items-center py-12 gap-2">
              <AlertTriangle className="h-6 w-6" style={{ color: 'var(--color-destructive)' }} />
              <span className="text-sm" style={{ color: 'var(--color-destructive)' }}>{error}</span>
            </div>
          ) : records.length === 0 ? (
            <div className="flex flex-col items-center py-12 gap-2">
              <Clock className="h-6 w-6" style={{ color: 'var(--color-muted-foreground)' }} />
              <span className="text-sm" style={{ color: 'var(--color-muted-foreground)' }}>{t('cronHistory.empty')}</span>
            </div>
          ) : (
            <table className="w-full text-left text-xs border-collapse">
              <thead>
                <tr style={{ backgroundColor: 'var(--color-surface-secondary)' }}>
                  <th className="px-4 py-2 font-semibold" style={{ color: 'var(--color-muted-foreground)' }}>{t('cronHistory.time')}</th>
                  <th className="px-4 py-2 font-semibold" style={{ color: 'var(--color-muted-foreground)' }}>{t('cronHistory.status')}</th>
                  <th className="px-4 py-2 font-semibold" style={{ color: 'var(--color-muted-foreground)' }}>{t('cronHistory.duration')}</th>
                  <th className="px-4 py-2 font-semibold" style={{ color: 'var(--color-muted-foreground)' }}>{t('cronHistory.summary')}</th>
                  <th className="px-4 py-2 font-semibold" style={{ color: 'var(--color-muted-foreground)' }}>{t('cronHistory.model')}</th>
                </tr>
              </thead>
              <tbody>
                {records.map(r => {
                  const statusIcon = r.status === 'success' ? <CheckCircle2 className="h-3.5 w-3.5" style={{ color: 'var(--color-success)' }} />
                    : r.status === 'failed' ? <XCircle className="h-3.5 w-3.5" style={{ color: 'var(--color-destructive)' }} />
                    : <AlertTriangle className="h-3.5 w-3.5" style={{ color: 'var(--color-warning)' }} />
                  return (
                    <tr key={r.id} style={{ borderTop: '1px solid var(--color-border)' }}>
                      <td className="px-4 py-2 font-mono" style={{ color: 'var(--color-foreground)' }}>{new Date(r.executed_at).toLocaleString()}</td>
                      <td className="px-4 py-2"><span className="inline-flex items-center gap-1">{statusIcon}{r.status}</span></td>
                      <td className="px-4 py-2 font-mono" style={{ color: 'var(--color-muted-foreground)' }}>{r.duration_ms}ms</td>
                      <td className="px-4 py-2 max-w-[200px] truncate" style={{ color: 'var(--color-muted-foreground)' }} title={r.result_summary}>{r.result_summary || '—'}</td>
                      <td className="px-4 py-2 font-mono text-[10px]" style={{ color: 'var(--color-muted-foreground)' }}>{r.provider_id}/{r.model_id}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </div>
  )
}
