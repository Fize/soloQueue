import { useEffect, useState } from 'react'
import type { CompactionStatus } from '@/types'
import { Check, Loader2, CircleAlert } from 'lucide-react'

const COMPLETION_NOTICE_MS = 5_000

function noticeExpiresAt(row: CompactionStatus) {
  if (row.status !== 'completed') return row.noticeExpiresAt ?? 0
  const finishedAt = Date.parse(row.finished_at ?? row.started_at)
  return Number.isFinite(finishedAt) ? finishedAt + COMPLETION_NOTICE_MS : 0
}

export function CompactionStatusRows({ rows }: { rows: CompactionStatus[] }) {
  const [now, setNow] = useState(Date.now)
  useEffect(() => {
    const deadlines = rows
      .filter((row) => row.status !== 'running')
      .map(noticeExpiresAt)
      .filter((deadline) => deadline > now)
    if (!deadlines.length) return
    const timer = setTimeout(
      () => setNow(Date.now()),
      Math.max(0, Math.min(...deadlines) - Date.now())
    )
    return () => clearTimeout(timer)
  }, [rows, now])

  const visible = rows.filter((row) => row.status === 'running' || noticeExpiresAt(row) > now)
  if (!visible.length) return null
  return (
    <div className="mx-auto max-w-3xl space-y-2 px-4" aria-live="polite">
      {visible.map((row) => (
        <div
          key={row.id}
          className="flex items-start gap-2 rounded-lg border border-border/50 bg-muted/30 px-3 py-2 text-sm text-muted-foreground"
        >
          {row.status === 'running' ? (
            <Loader2 className="mt-0.5 h-4 w-4 animate-spin" />
          ) : row.status === 'completed' ? (
            <Check className="mt-0.5 h-4 w-4 text-green-600" />
          ) : (
            <CircleAlert className="mt-0.5 h-4 w-4 text-destructive" />
          )}
          <div>
            <div>
              {row.status === 'running'
                ? 'Compressing conversation context…'
                : row.status === 'completed'
                  ? 'Context compression completed.'
                  : row.status === 'interrupted'
                    ? 'Context compression was interrupted.'
                    : 'Context compression failed.'}
            </div>
            {row.error && <div className="mt-1 text-xs text-destructive">{row.error}</div>}
            <time className="text-xs opacity-70">{new Date(row.started_at).toLocaleString()}</time>
          </div>
        </div>
      ))}
    </div>
  )
}
