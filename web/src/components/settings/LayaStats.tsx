import { useEffect, useState } from 'react'
import { getRouterStats, type RouterStat } from '@/lib/api/chat-api'
import { useTranslation } from '@/lib/i18n'
import { GlassCard } from '@/components/ui/glass-card'

// The existing router endpoint compares UTC CURRENT_TIMESTAMP values, not RFC3339 strings.
const routerDate = (instant: string) =>
  new Date(instant).toISOString().slice(0, 19).replace('T', ' ')

export function LayaStats({ from, to, team }: { from: string; to: string; team: string }) {
  const { t } = useTranslation()
  const [retry, setRetry] = useState(0)
  const key = JSON.stringify([from, to, team, retry])
  const [result, setResult] = useState<{
    key: string
    rows: RouterStat[] | null
    error: boolean
  } | null>(null)
  const rows = result?.key === key ? result.rows : null
  const error = result?.key === key && result.error

  useEffect(() => {
    const controller = new AbortController()
    void getRouterStats(
      'daily',
      team || undefined,
      routerDate(from),
      routerDate(to),
      controller.signal
    )
      .then((data) => {
        if (!controller.signal.aborted) setResult({ key, rows: data ?? [], error: false })
      })
      .catch(() => {
        if (!controller.signal.aborted) setResult({ key, rows: null, error: true })
      })
    return () => controller.abort()
  }, [from, to, team, key])

  const counts = new Map<string, { success: number; fallback: number }>([
    ['general', { success: 0, fallback: 0 }],
    ['engineering', { success: 0, fallback: 0 }],
    ['research', { success: 0, fallback: 0 }],
  ])
  let success = 0
  let fallback = 0
  for (const row of rows ?? []) {
    if (row.classification_source !== 'laya' && row.classification_source !== 'laya_unavailable')
      continue
    const count = counts.get(row.classification_level) ?? { success: 0, fallback: 0 }
    if (row.classification_source === 'laya') {
      success += row.count
      count.success += row.count
    } else {
      fallback += row.count
      count.fallback += row.count
    }
    counts.set(row.classification_level, count)
  }
  const taskLabels: Record<string, string> = {
    general: t('config.taskTypeGeneral'),
    engineering: t('config.taskTypeEngineering'),
    research: t('config.taskTypeResearch'),
  }

  return (
    <GlassCard
      variant="flat"
      className="min-w-0 p-4"
      role="region"
      aria-label={t('stats.layaTitle')}
    >
      <h3 className="text-sm font-semibold text-foreground">{t('stats.layaTitle')}</h3>
      <p className="mt-1 text-xs text-muted-foreground">{t('stats.layaScope')}</p>
      {error ? (
        <div role="alert" className="mt-4 flex items-center gap-3 text-sm text-destructive">
          <span>{t('stats.layaError')}</span>
          <button
            type="button"
            onClick={() => setRetry((value) => value + 1)}
            className="underline underline-offset-2"
          >
            {t('stats.retry')}
          </button>
        </div>
      ) : rows === null ? (
        <p role="status" className="mt-4 text-sm text-muted-foreground">
          {t('stats.layaLoading')}
        </p>
      ) : (
        <>
          <dl className="my-4 flex flex-wrap gap-3 rounded-lg bg-muted/30 p-3">
            <div className="min-w-0 flex-1 basis-28">
              <dt className="break-words text-xs text-muted-foreground">
                {t('stats.layaSuccess')}
              </dt>
              <dd data-testid="laya-success" className="mt-1 text-xl font-semibold tabular-nums">
                {success.toLocaleString()}
              </dd>
            </div>
            <div className="min-w-0 flex-1 basis-28">
              <dt className="break-words text-xs text-muted-foreground">
                {t('stats.layaFallback')}
              </dt>
              <dd
                data-testid="laya-fallback"
                className={`mt-1 text-xl font-semibold tabular-nums ${fallback > 0 ? 'text-amber-600 dark:text-amber-400' : ''}`}
              >
                {fallback.toLocaleString()}
              </dd>
            </div>
          </dl>
          {success + fallback === 0 ? (
            <p className="text-sm text-muted-foreground">{t('stats.layaEmpty')}</p>
          ) : (
            <div className="overflow-x-auto">
              <table
                className="w-full min-w-[220px] table-fixed text-left text-xs"
                aria-label={t('stats.layaDistribution')}
              >
                <caption className="mb-2 text-left font-medium">
                  {t('stats.layaDistribution')}
                </caption>
                <thead>
                  <tr className="border-b border-border text-muted-foreground">
                    <th scope="col" className="w-[40%] py-2">
                      {t('stats.layaTaskType')}
                    </th>
                    <th scope="col" className="py-2 text-right">
                      {t('stats.layaClassified')}
                    </th>
                    <th scope="col" className="py-2 pl-3 text-right">
                      {t('stats.layaFallback')}
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {Array.from(counts, ([task, count]) => (
                    <tr key={task} className="border-b border-border/50 last:border-0">
                      <td className="py-2">{taskLabels[task] ?? task}</td>
                      <td className="py-2 text-right tabular-nums">
                        {count.success.toLocaleString()}
                      </td>
                      <td
                        className={`py-2 pl-3 text-right tabular-nums ${count.fallback > 0 ? 'text-amber-600 dark:text-amber-400' : ''}`}
                      >
                        {count.fallback.toLocaleString()}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}
      <details className="mt-4 border-t border-border/60 pt-3 text-xs text-muted-foreground">
        <summary className="cursor-pointer">{t('stats.layaMethodology')}</summary>
        <p className="mt-2 leading-relaxed">{t('stats.layaMethodologyDesc')}</p>
      </details>
    </GlassCard>
  )
}
