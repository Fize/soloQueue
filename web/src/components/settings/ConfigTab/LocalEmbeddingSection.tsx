import { useEffect, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { getEmbeddingStatus, runEmbeddingAction } from '@/lib/api/config-api'
import { useTranslation } from '@/lib/i18n'
import type { EmbeddingAction, EmbeddingConfig, EmbeddingStatus } from '@/types'

const activeStates = ['preparing', 'downloading', 'loading', 'reindexing']

export function LocalEmbeddingSection({ config, savedConfig, revision = 0, onChange }: {
  config: EmbeddingConfig
  savedConfig?: EmbeddingConfig | null
  revision?: number
  onChange: (config: EmbeddingConfig) => void
}) {
  const { t } = useTranslation()
  const [status, setStatus] = useState<EmbeddingStatus | null>(null)
  const [error, setError] = useState<'status' | 'action' | null>(null)
  const [busy, setBusy] = useState(false)
  const [refresh, setRefresh] = useState(0)
  const lifecycle = useRef<{ controller: AbortController; sequence: number; busy: boolean } | null>(null)
  // Only persisted changes restart the lifecycle subscription; draft edits never change the running model.
  const savedKey = JSON.stringify([savedConfig?.provider, savedConfig?.enabled, savedConfig?.local, revision])
  useEffect(() => {
    const current = { controller: new AbortController(), sequence: 0, busy: false }
    lifecycle.current = current
    let timer: ReturnType<typeof setTimeout> | undefined
    // A new persisted configuration invalidates the previous external service snapshot.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setStatus(null)
    setError(null)
    setBusy(false)
    const poll = async () => {
      if (!current.busy) {
        const sequence = ++current.sequence
        try {
          const next = await getEmbeddingStatus({ signal: current.controller.signal })
          if (!current.controller.signal.aborted && current.sequence === sequence) {
            setStatus(next)
            setError(null)
          }
        } catch {
          if (!current.controller.signal.aborted && current.sequence === sequence) {
            setStatus(null)
            setError('status')
          }
        }
      }
      if (!current.controller.signal.aborted) timer = setTimeout(poll, 2000)
    }
    void poll()
    return () => {
      current.controller.abort()
      clearTimeout(timer)
    }
  }, [savedKey, refresh])

  const act = async (action: EmbeddingAction) => {
    const current = lifecycle.current
    if (!current || current.busy || current.controller.signal.aborted) return
    current.busy = true
    const sequence = ++current.sequence
    setBusy(true)
    setError(null)
    try {
      const next = await runEmbeddingAction(action, { signal: current.controller.signal })
      if (!current.controller.signal.aborted && sequence === current.sequence) setStatus(next)
    } catch {
      if (!current.controller.signal.aborted) setError('action')
    } finally {
      current.busy = false
      if (!current.controller.signal.aborted) setBusy(false)
    }
  }
  const local = config.local || { model: 'multilingual-e5-small', device: 'auto' as const }
  const unsaved = config.provider !== savedConfig?.provider || config.enabled !== savedConfig?.enabled ||
    (local.model || 'multilingual-e5-small') !== (savedConfig?.local?.model || 'multilingual-e5-small')
  const actionable = savedConfig?.provider === 'onnx' && savedConfig.enabled && status?.provider === 'onnx'
  return (
    <section className="space-y-4 border-t pt-4 min-w-0" aria-label={t('config.embedLocalTitle')}>
      {config.provider === 'onnx' && <>
        <label className="flex flex-col gap-1.5 text-sm">
          {t('config.embedLocalModel')}
          <select className="rounded-md border bg-background px-3 py-2" value={local.model || 'multilingual-e5-small'}
            onChange={e => onChange({ ...config, local: { ...local, model: e.target.value } })}>
            <option value="multilingual-e5-small">multilingual-e5-small</option>
          </select>
        </label>
        <p className="text-xs text-muted-foreground">{t('config.embedLocalHelp')}</p>
      </>}
      {unsaved && <p className="text-sm">{t('config.embedLocalUnsaved')}</p>}
      <div className="space-y-2 text-sm break-words" aria-live="polite" aria-atomic="true">
        <h4 className="font-semibold">{t('config.embedLocalRunning')}</h4>
        {!status && !error && <p role="status">{t('common.loading')}</p>}
        {status && <>
          <p>{t('config.embedLocalState')}: {t(`config.embedState_${status.state}`)}</p>
          <p>{t('config.embedLocalModel')}: {status.model || '—'} · {status.provider}</p>
          <p>{t('config.embedLocalBackend')}: {status.backend || '—'} · {status.dimension}</p>
          {status.fallbackReason && <p>{t('config.embedLocalFallback')}: {status.fallbackReason}</p>}
          {status.state === 'downloading' && <>
            <p>{t('config.embedLocalDownload')}: {status.downloadedBytes.toLocaleString()} / {status.totalBytes > 0 ? status.totalBytes.toLocaleString() : t('config.embedLocalUnknown')} bytes</p>
            {status.totalBytes > 0 && <progress className="w-full" aria-label={t('config.embedLocalDownload')} max={status.totalBytes} value={Math.min(status.downloadedBytes, status.totalBytes)} />}
          </>}
          <p>{t('config.embedLocalCounts', { indexed: status.indexed, pending: status.pending, failed: status.failed })}</p>
          {status.state === 'disabled' && <p>{t('config.embedLocalDisabled')}</p>}
          {(status.state === 'error' || status.error) && <div role="alert">
            <p>{t('config.embedLocalRuntimeError')}</p>
            {status.error && <p className="text-xs">{status.error}</p>}
          </div>}
        </>}
      </div>
      {error && <p role="alert">{t(error === 'status' ? 'config.embedLocalStatusError' : 'config.embedLocalActionError')}</p>}
      <div className="flex flex-wrap gap-2">
        {error === 'status' && <Button variant="outline" onClick={() => setRefresh(v => v + 1)}>{t('config.embedRetry')}</Button>}
        {actionable && status && <>
          {['error', 'cancelled'].includes(status.state) && <Button disabled={busy} onClick={() => void act('retry')}>{t('config.embedRetry')}</Button>}
          {activeStates.includes(status.state) && <Button disabled={busy} variant="outline" onClick={() => void act('cancel')}>{t('config.embedCancel')}</Button>}
          {status.state === 'ready' && <Button disabled={busy} variant="outline" onClick={() => void act('reindex')}>{t('config.embedReindex')}</Button>}
        </>}
        {busy && <span role="status">{t('common.loading')}</span>}
      </div>
    </section>
  )
}
