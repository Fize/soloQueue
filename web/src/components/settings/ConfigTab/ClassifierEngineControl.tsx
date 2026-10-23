import { useEffect, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { getClassifierStatus, runClassifierAction, updateClassifierConfig } from '@/lib/api/config-api'
import { useTranslation } from '@/lib/i18n'
import type { ClassifierConfig, ClassifierStatus } from '@/types'

const activeStates = ['preparing', 'downloading', 'loading']

/**
 * Engine switch for the classifier route. The choice is persisted immediately, so the worker
 * lifecycle follows the saved config rather than a draft.
 */
export function ClassifierEngineControl({
  config,
  onChange,
}: {
  config: ClassifierConfig
  onChange: (config: ClassifierConfig) => void
}) {
  const { t } = useTranslation()
  const [status, setStatus] = useState<ClassifierStatus | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [watch, setWatch] = useState(0)
  const lifecycle = useRef<{ controller: AbortController; sequence: number } | null>(null)

  useEffect(() => {
    if (config.engine !== 'laya') return
    const current = { controller: new AbortController(), sequence: 0 }
    lifecycle.current = current
    let timer: ReturnType<typeof setTimeout> | undefined
    const poll = async () => {
      if (current.controller.signal.aborted) return
      const sequence = ++current.sequence
      try {
        const next = await getClassifierStatus({ signal: current.controller.signal })
        if (current.controller.signal.aborted || current.sequence !== sequence) return
        setStatus(next)
        setError(null)
        if (activeStates.includes(next.state)) timer = setTimeout(poll, 1000)
      } catch (err) {
        if (current.controller.signal.aborted || current.sequence !== sequence) return
        setError((err as Error).message)
      }
    }
    void poll()
    return () => {
      current.controller.abort()
      if (timer) clearTimeout(timer)
    }
  }, [config.engine, watch])

  const act = async (action: 'retry' | 'cancel') => {
    // Status requested before an action must not replace its newer lifecycle state.
    lifecycle.current?.controller.abort()
    setBusy(true)
    setError(null)
    setActionError(null)
    try {
      setStatus(await runClassifierAction(action))
    } catch (err) {
      setActionError((err as Error).message)
    } finally {
      // Observe the worker after either outcome, including actions rejected by the server.
      setWatch((value) => value + 1)
      setBusy(false)
    }
  }

  const selectEngine = async (engine: 'llm' | 'laya') => {
    if (engine === config.engine) return
    onChange({ ...config, engine })
    setBusy(true)
    setError(null)
    setActionError(null)
    try {
      onChange(await updateClassifierConfig({ ...config, engine }))
      // An optimistic engine change can poll before the saved config starts the worker.
      setWatch((value) => value + 1)
    } catch (err) {
      onChange(config)
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const percent =
    status && status.totalBytes > 0
      ? Math.min(100, Math.round((status.downloadedBytes / status.totalBytes) * 100))
      : null

  return (
    <div className="space-y-2">
      <div
        role="group"
        aria-label={t('config.classifierEngine')}
        className="inline-flex rounded-md border bg-background p-0.5"
      >
        {(['laya', 'llm'] as const).map((engine) => (
          <button
            key={engine}
            type="button"
            disabled={busy}
            aria-pressed={config.engine === engine}
            aria-label={engine === 'llm' ? t('config.classifierEngineLlm') : t('config.classifierEngineLaya')}
            title={engine === 'laya' ? t('config.classifierLayaHint') : undefined}
            onClick={() => void selectEngine(engine)}
            className={`rounded px-2.5 py-1 text-xs font-medium transition-colors disabled:opacity-60 ${
              config.engine === engine
                ? 'bg-primary text-primary-foreground'
                : 'text-muted-foreground hover:text-foreground'
            }`}
          >
            {engine === 'llm' ? t('config.classifierEngineLlmShort') : t('config.classifierEngineLayaShort')}
          </button>
        ))}
      </div>

      {config.engine === 'laya' ? (
        <div className="space-y-1.5">
          <p className="text-xs text-muted-foreground" aria-live="polite">
            {error
              ? `${t('config.classifierStatusError')}: ${error}`
              : status
                ? `${status.state}${percent === null ? '' : ` ${percent}%`}${status.error ? ` — ${status.error}` : ''}`
                : t('config.classifierStatusUnknown')}
          </p>
          {actionError ? (
            <p className="text-xs text-muted-foreground" role="alert">
              {`${t('config.classifierStatusError')}: ${actionError}`}
            </p>
          ) : null}
          <div className="flex gap-2">
            <Button size="sm" variant="outline" className="h-7 px-2 text-xs" disabled={busy} onClick={() => void act('retry')}>
              {t('config.classifierRetry')}
            </Button>
            <Button size="sm" variant="ghost" className="h-7 px-2 text-xs" disabled={busy} onClick={() => void act('cancel')}>
              {t('config.classifierCancel')}
            </Button>
          </div>
        </div>
      ) : (
        <p className="text-xs text-muted-foreground">{t('config.classifierLlmHint')}</p>
      )}
    </div>
  )
}
