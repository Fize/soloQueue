import type { StorageCategoryId, StoragePolicy } from '@/lib/api/storage-api'
import { useTranslation } from '@/lib/i18n'

const categories: StorageCategoryId[] = ['runtime_logs', 'session_history', 'execution_details', 'cron_history']
export function validateStoragePolicy(policy: StoragePolicy): boolean {
  return Object.values(policy).every((category) => Object.entries(category).every(([key, value]) =>
    typeof value === 'boolean' || (typeof value === 'number' && Number.isInteger(value) && value >= (key.endsWith('file_mb') || key === 'max_turn_mb' ? 1 : 0) && value <= (key.endsWith('file_mb') || key === 'max_turn_mb' ? 1024 : key.endsWith('retention_days') ? 36500 : key === 'max_files' || key === 'http_max_files' || key === 'max_per_task' ? 100000 : 1048576))
  ))
}
export function tightensStoragePolicy(before: StoragePolicy, after: StoragePolicy): boolean {
  const tighter = (old: number, next: number) => next > 0 && (old === 0 || next < old)
  if (categories.some((id) => Object.entries(after[id]).some(([key, next]) =>
    key !== 'warning_mb' && key !== 'max_file_mb' && !key.startsWith('http_') && typeof next === 'number' &&
    tighter((before[id] as unknown as Record<string, number>)[key], next)
  ))) return true
  const old = before.runtime_logs
  const next = after.runtime_logs
  if ((!old.http_override && next.http_override) || tighter(old.http_retention_days, next.http_retention_days) || tighter(old.http_max_files, next.http_max_files)) return true
  const oldDays = old.http_override ? old.http_retention_days : old.retention_days
  const oldFiles = old.http_override ? old.http_max_files : old.max_files
  return tighter(oldDays, next.http_override ? next.http_retention_days : next.retention_days) ||
    tighter(oldFiles, next.http_override ? next.http_max_files : next.max_files)
}

function LimitField({ id, label, value, onChange, disabled, allowUnlimited = true, unlimitedLabel }: {
  id: string; label: string; value: number; onChange: (value: number) => void
  disabled: boolean; allowUnlimited?: boolean; unlimitedLabel?: string
}) {
  const { t } = useTranslation()
  const unlimited = allowUnlimited && value === 0
  return <div className="space-y-1.5">
    <label htmlFor={id} className="block text-xs font-medium">{label}</label>
    <input id={id} type="number" min={allowUnlimited ? 0 : 1} max={id.endsWith('file_mb') || id.endsWith('max_turn_mb') ? 1024 : id.endsWith('retention_days') ? 36500 : id.endsWith('max_files') || id.endsWith('max_per_task') ? 100000 : 1048576} step={1}
      className="h-9 w-full rounded-md border border-border bg-background px-3 text-sm disabled:opacity-50"
      value={Number.isNaN(value) ? '' : value} disabled={disabled || unlimited}
      onChange={(event) => onChange(event.target.value === '' ? NaN : Number(event.target.value))} />
    {allowUnlimited && <label className="flex items-center gap-2 text-xs text-muted-foreground">
      <input type="checkbox" checked={unlimited} disabled={disabled}
        onChange={(event) => onChange(event.target.checked ? 0 : 30)} />
      {unlimitedLabel || t('storage.unlimited')}
    </label>}
  </div>
}

export function StoragePolicyFields({ policy, onChange, disabled }: {
  policy: StoragePolicy; onChange: (policy: StoragePolicy) => void; disabled: boolean
}) {
  const { t } = useTranslation()
  return <div className="space-y-4">{categories.map((category) => {
    const values = policy[category]
    const set = (key: string, value: number | boolean) => onChange({ ...policy, [category]: { ...values, [key]: value } })
    const field = (key: string, allowUnlimited = true) => <LimitField key={key} id={`${category}-${key}`}
      label={t(`storage.${key === 'retention_days' ? 'retention' : key === 'max_files' && category !== 'runtime_logs' ? 'detailCount' : key}`)}
      value={(values as unknown as Record<string, number>)[key]} onChange={(value) => set(key, value)}
      disabled={disabled} allowUnlimited={allowUnlimited}
      unlimitedLabel={category === 'session_history' && key === 'retention_days' ? t('storage.longTerm') : undefined} />
    return <section id={category} key={category} aria-labelledby={`${category}-title`} className="rounded-lg border border-border/50 p-4 space-y-3">
      <h3 id={`${category}-title`} className="text-sm font-semibold">{t(`storage.${category}`)}</h3>
      {category === 'session_history' && <p className="text-xs text-muted-foreground">{t('storage.historyNote')}</p>}
      {category === 'execution_details' && <p className="text-xs text-muted-foreground">{t('storage.detailsNote')}</p>}
      <div className="grid gap-4 sm:grid-cols-2">
        {field('retention_days')}
        {category === 'session_history' ? field('warning_mb') : field('max_total_mb')}
      </div>
      <details className="text-xs">
        <summary className="cursor-pointer py-1 text-muted-foreground">{t('storage.advanced')}</summary>
        <div className="grid gap-4 sm:grid-cols-2 pt-3">
          {field('max_file_mb', false)}{category !== 'session_history' && field('max_files')}
          {category === 'execution_details' && field('max_turn_mb', false)}
          {category === 'cron_history' && field('max_per_task')}
        </div>
        <p className="mt-3 text-xs text-muted-foreground">{t('storage.filesNote')}</p>
        {category === 'runtime_logs' && <div className="mt-4 space-y-3">
          <label className="flex items-center gap-2"><input type="checkbox" checked={policy.runtime_logs.http_override}
            disabled={disabled} onChange={(event) => set('http_override', event.target.checked)} />{t('storage.httpOverride')}</label>
          {policy.runtime_logs.http_override && <div className="grid gap-4 sm:grid-cols-2">
            {field('http_max_file_mb', false)}{field('http_retention_days')}{field('http_max_files')}
          </div>}
        </div>}
      </details>
    </section>
  })}</div>
}
