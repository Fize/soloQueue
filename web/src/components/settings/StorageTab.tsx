import { useCallback, useEffect, useRef, useState } from 'react'
import { HardDrive, RefreshCw } from 'lucide-react'
import { useTranslation } from '@/lib/i18n'
import { Button } from '@/components/ui/button'
import { getStorage, updateStorage, previewStorage, cleanupStorage, formatStorageBytes } from '@/lib/api/storage-api'
import type { StorageSnapshot, StoragePolicy, StoragePreview } from '@/lib/api/storage-api'
import { StoragePolicyFields, tightensStoragePolicy, validateStoragePolicy } from './StoragePolicyFields'
import { StoragePreviewDialog } from './StoragePreviewDialog'

export function StorageTab() {
  const { t } = useTranslation()
  const [snapshot, setSnapshot] = useState<StorageSnapshot | null>(null)
  const [policy, setPolicy] = useState<StoragePolicy | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [legacy, setLegacy] = useState(false)
  const [preview, setPreview] = useState<StoragePreview | null>(null)
  const [previewMode, setPreviewMode] = useState<'save' | 'cleanup'>('save')
  const mounted = useRef(true)
  const operation = useRef(false)
  const generation = useRef(0)
  const dirty = !!snapshot && !!policy && JSON.stringify(snapshot.policy) !== JSON.stringify(policy)

  const load = useCallback(async (keepEdits = true) => {
    const current = ++generation.current
    setLoading(true)
    try {
      const value = await getStorage()
      if (!mounted.current || current !== generation.current) return
      setSnapshot(value)
      setPolicy((old) => keepEdits && old ? old : value.policy)
      setError('')
    } catch (cause) {
      if (mounted.current && current === generation.current) setError(cause instanceof Error ? cause.message : 'Storage unavailable')
    } finally {
      if (mounted.current && current === generation.current) setLoading(false)
    }
  }, [])
  useEffect(() => { mounted.current = true; void Promise.resolve().then(() => { if (mounted.current) void load(false) }); return () => { mounted.current = false } }, [load])

  useEffect(() => {
    if (!snapshot) return
    const category = new URLSearchParams(window.location.hash.split('?')[1] || '').get('category')
    if (category && ['runtime_logs', 'session_history', 'execution_details', 'cron_history'].includes(category)) {
      document.getElementById(category)?.scrollIntoView?.({ block: 'start', behavior: 'smooth' })
    }
  }, [snapshot])

  async function act(action: () => Promise<void>) {
    if (operation.current) return
    operation.current = true; setBusy(true); setError(''); setNotice('')
    try { await action() } catch (cause) {
      if (mounted.current) setError(cause instanceof Error ? cause.message : t('storage.error'))
    } finally { operation.current = false; if (mounted.current) setBusy(false) }
  }
  function review(mode: 'save' | 'cleanup') {
    if (!snapshot || !policy) return
    void act(async () => {
      if (mode === 'save' && !validateStoragePolicy(policy)) { setError(t('storage.validation')); return }
      const value = await previewStorage(snapshot.revision, mode === 'save' ? policy : undefined, mode === 'cleanup' && legacy)
      if (mounted.current) { setPreviewMode(mode); setPreview(value) }
    })
  }
  function save() {
    if (!snapshot || !policy) return
    if (!validateStoragePolicy(policy)) { setError(t('storage.validation')); return }
    if (tightensStoragePolicy(snapshot.policy, policy)) { review('save'); return }
    void act(async () => {
      const value = await updateStorage(policy, snapshot.revision)
      if (mounted.current) { generation.current++; setSnapshot(value); setPolicy(value.policy); setNotice(t('storage.saved')) }
    })
  }
  function confirm() {
    if (!preview) return
    void act(async () => {
      if (previewMode === 'save') {
        const value = await updateStorage(preview.policy, preview.revision, preview.preview_id)
        if (mounted.current) { generation.current++; setSnapshot(value); setPolicy(value.policy); setNotice(t('storage.saved')) }
      } else {
        const result = await cleanupStorage(preview.preview_id, preview.revision)
        if (mounted.current) {
          setNotice(`${t('storage.cleanupDone')} ${formatStorageBytes(result.freed_bytes)}`)
          if (result.errors?.length) setError(result.errors.join('\n'))
          const value = await getStorage()
          if (mounted.current) { setSnapshot(value); setPolicy((old) => old || value.policy) }
        }
      }
      if (mounted.current) setPreview(null)
    })
  }
  if (!snapshot && loading) return <p role="status" className="text-sm text-muted-foreground">{t('common.loading')}</p>
  if (!snapshot || !policy) return <div role="alert" className="space-y-3"><p>{t('storage.loadError')} {error}</p><Button onClick={() => void load(false)}>{t('storage.retry')}</Button></div>
  const categories = snapshot.usage.categories || []
  const date = (value?: string) => value && !value.startsWith('0001-') ? new Date(value).toLocaleString() : t('storage.never')
  return <div className="space-y-6">
    <header className="flex flex-wrap items-start justify-between gap-3">
      <div><h2 className="flex items-center gap-2 text-base font-semibold"><HardDrive className="size-4" />{t('storage.title')}</h2><p className="mt-1 text-xs text-muted-foreground">{t('storage.intro')}</p></div>
      <Button variant="outline" size="sm" disabled={loading || busy} onClick={() => void load(dirty)}><RefreshCw className={loading ? 'animate-spin' : ''} />{t('storage.refresh')}</Button>
    </header>
    {error && !preview && <div role="alert" className="whitespace-pre-wrap rounded-md border border-destructive/30 p-3 text-sm text-destructive">{error}</div>}
    {notice && <p role="status" className="text-sm text-success">{notice}</p>}
    <section aria-label={t('storage.total')} className="rounded-lg bg-muted/30 p-4 space-y-3">
      <div className="flex flex-wrap justify-between gap-2"><strong>{t('storage.total')}: {formatStorageBytes(snapshot.usage.total_bytes)}</strong><span className="text-xs text-muted-foreground">{t('storage.sampled')}: {date(snapshot.usage.sampled_at)}</span></div>
      <p className="text-xs text-muted-foreground">{t('storage.files')}: {formatStorageBytes(snapshot.usage.file_bytes)} · {t('storage.database')}: {formatStorageBytes(snapshot.usage.database_bytes)}</p>
      <div className="grid gap-3 sm:grid-cols-2">{categories.map((category) => <div key={category.id} className="min-w-0 rounded-md border border-border/40 bg-card p-3 text-xs">
        <div className="flex justify-between gap-2"><span>{t(`storage.${category.id}`)}</span><strong>{formatStorageBytes(category.bytes)}</strong></div>
        <p className="mt-1 text-muted-foreground">{category.files} {t('storage.files')}</p>
        {['runtime_logs', 'execution_details', 'cron_history'].includes(category.id) && <p className="mt-1 text-muted-foreground">{t('storage.max_total_mb')}: {(snapshot.policy[category.id as 'runtime_logs' | 'execution_details' | 'cron_history'].max_total_mb || 0) > 0 ? formatStorageBytes(snapshot.policy[category.id as 'runtime_logs' | 'execution_details' | 'cron_history'].max_total_mb * 1024 ** 2) : t('storage.unlimited')}</p>}
        {category.id === 'session_history' && <p className="mt-1 text-muted-foreground">{t('storage.warning_mb')}: {snapshot.policy.session_history.warning_mb ? formatStorageBytes(snapshot.policy.session_history.warning_mb * 1024 ** 2) : t('storage.unlimited')}</p>}
        {category.oldest_at && <p className="mt-1 break-words text-muted-foreground">{t('storage.oldest')}: {date(category.oldest_at)}</p>}
        {category.warning && <p className="mt-1 text-warning">{t('storage.warning_mb')}</p>}
      </div>)}</div>
      {snapshot.usage.total_bytes === 0 && <p className="text-xs text-muted-foreground">{t('storage.empty')}</p>}
      {!!snapshot.usage.errors?.length && <div role="alert" className="text-xs text-destructive">{t('storage.partial')}<ul>{snapshot.usage.errors.map((item, i) => <li key={i}>{item}</li>)}</ul></div>}
    </section>
    <StoragePolicyFields policy={policy} onChange={(value) => { setPolicy(value); setPreview(null); setNotice('') }} disabled={busy || loading} />
    <div className="flex flex-wrap items-center gap-3"><Button size="sm" disabled={!dirty || busy || loading} onClick={save}>{busy ? t('storage.busy') : t('storage.save')}</Button>{dirty && <span className="text-xs text-muted-foreground">{t('storage.unsaved')}</span>}</div>
    <p className="text-xs text-muted-foreground">{t(snapshot.restart_required ? 'storage.restart' : 'storage.live')}</p>
    <section className="border-t border-border/50 pt-5 space-y-3">
      <p className="text-xs text-muted-foreground">{t('storage.lastCleanup')}: {date(snapshot.last_cleanup?.finished_at)} · {t('storage.nextCleanup')}: {date(snapshot.next_cleanup_at)}</p>
      {snapshot.last_cleanup && <p className="text-xs">{t('storage.deleted')}: {snapshot.last_cleanup.deleted_files} · {t('storage.freed')}: {formatStorageBytes(snapshot.last_cleanup.freed_bytes)}</p>}
      {!!snapshot.last_cleanup?.errors?.length && <p role="alert" className="text-xs text-destructive">{snapshot.last_cleanup.errors.join('\n')}</p>}
      <p className="text-xs text-muted-foreground">{t('storage.legacyNote')}</p>
      {!!snapshot.usage.legacy_files?.length && <details className="text-xs"><summary className="cursor-pointer">{t('storage.legacy')}</summary><ul className="mt-2 space-y-2">{snapshot.usage.legacy_files.map((file) => <li key={file.name} className="flex flex-wrap justify-between gap-2"><code className="break-all">{file.name}</code><span>{formatStorageBytes(file.bytes)} · {date(file.modified_at)}</span></li>)}</ul></details>}
      <label className="flex items-center gap-2 text-xs"><input type="checkbox" checked={legacy} disabled={busy} onChange={(event) => setLegacy(event.target.checked)} />{t('storage.includeLegacy')}</label>
      <Button variant="outline" size="sm" disabled={busy || loading || dirty} onClick={() => review('cleanup')}>{t('storage.previewCleanup')}</Button>
    </section>
    <StoragePreviewDialog key={preview?.preview_id || 'closed'} preview={preview} error={error} mode={previewMode} busy={busy} onClose={() => setPreview(null)} onConfirm={confirm} />
  </div>
}
