import { useEffect, useState } from 'react'
import { useTranslation } from '@/lib/i18n'
import { Button } from '@/components/ui/button'
import { getObservedPrompt, formatExecutionTime } from '@/lib/api/observability-api'
import type { ObservedCall, ObservedPrompt } from '@/lib/api/observability-api'

type CallProps = { call: ObservedCall; sessionId: string; requestId: string }

export function CallDetails(props: CallProps) {
  return (
    <details className="rounded-lg border border-border/50 p-3">
      <summary className="cursor-pointer text-xs">
        {props.call.provider_id || '—'} / {props.call.model_id || '—'}
      </summary>
      <div className="mt-3">
        <CallInspector {...props} />
      </div>
    </details>
  )
}

export function CallInspector({ call, sessionId, requestId }: CallProps) {
  const { t } = useTranslation()
  const [showPrompt, setShowPrompt] = useState(false)
  const [prompt, setPrompt] = useState<ObservedPrompt | null>(null)
  const [error, setError] = useState('')
  const [retry, setRetry] = useState(0)
  useEffect(() => {
    if (!showPrompt || !call.prompt_available || call.prompt_source_call_id) return
    const controller = new AbortController()
    void getObservedPrompt(sessionId, requestId, call.call_id, controller.signal)
      .then((value) => {
        if (!controller.signal.aborted) {
          setPrompt(value)
          setError('')
        }
      })
      .catch((cause) => {
        if (!controller.signal.aborted)
          setError(cause instanceof Error ? cause.message : t('observability.error'))
      })
    return () => controller.abort()
  }, [
    showPrompt,
    sessionId,
    requestId,
    call.call_id,
    call.prompt_available,
    call.prompt_source_call_id,
    retry,
    t,
  ])
  const status = (value: string) => t(`observability.${value === 'error' ? 'callError' : value}`)
  return (
    <div className="space-y-3 text-xs">
      <p className="text-muted-foreground">
        {t(`observability.${call.usage_type || 'unknown'}`)} ·{' '}
        {formatExecutionTime(call.duration_ms)} · {status(call.status)}
      </p>
      <dl className="grid gap-2 sm:grid-cols-2 break-words">
        <div>
          <dt className="text-muted-foreground">{t('observability.agent')}</dt>
          <dd className="font-mono">{call.agent_id || '—'}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">{t('observability.started')}</dt>
          <dd>{new Date(call.started_at).toLocaleString()}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">{t('observability.finished')}</dt>
          <dd>{call.finished_at ? new Date(call.finished_at).toLocaleString() : '—'}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">{t('observability.tokens')}</dt>
          <dd>
            {t('observability.input')}: {call.prompt_tokens} · {t('observability.output')}:{' '}
            {call.completion_tokens}
          </dd>
        </div>
        <div>
          <dt className="text-muted-foreground">{t('observability.attempts')}</dt>
          <dd>
            {call.attempts_reported ? call.attempt_count : t('observability.unknown')} ·{' '}
            {t('observability.retries')}: {call.attempts_reported ? call.retry_count : '—'}
          </dd>
        </div>
        <div>
          <dt className="text-muted-foreground">{t('observability.recording')}</dt>
          <dd>{status(call.recording_status)}</dd>
        </div>
      </dl>
      {(call.request_ids?.length || 0) > 1 && (
        <p className="text-muted-foreground">{t('observability.shared')}</p>
      )}
      {call.error && (
        <p role="alert" className="text-destructive whitespace-pre-wrap">
          {call.error}
        </p>
      )}
      {!call.metrics_only && call.recording_status !== 'complete' && (
        <p className="text-muted-foreground">{status(call.recording_status)}</p>
      )}
      {call.content && (
        <details>
          <summary className="cursor-pointer">{t('observability.content')}</summary>
          <pre className="mt-2 whitespace-pre-wrap break-words rounded bg-muted/40 p-3 font-sans">
            {call.content}
          </pre>
        </details>
      )}
      {call.reasoning && (
        <details>
          <summary className="cursor-pointer">{t('observability.reasoning')}</summary>
          <pre className="mt-2 whitespace-pre-wrap break-words rounded bg-muted/40 p-3 font-sans">
            {call.reasoning}
          </pre>
        </details>
      )}
      {!call.content && (
        <p className="text-muted-foreground">{t('observability.contentMissing')}</p>
      )}
      {!call.reasoning && (
        <p className="text-muted-foreground">{t('observability.reasoningMissing')}</p>
      )}
      {call.prompt_source_call_id ? (
        <p className="text-muted-foreground">{t('observability.promptUnchanged')}</p>
      ) : !call.prompt_available ? (
        !call.metrics_only && (
          <p className="text-muted-foreground">
            {t(
              call.recording_status === 'expired'
                ? 'observability.promptExpired'
                : 'observability.promptMissing'
            )}
          </p>
        )
      ) : (
        <Button
          variant="outline"
          size="xs"
          onClick={() => {
            setShowPrompt(!showPrompt)
            setPrompt(null)
            setError('')
          }}
          aria-expanded={showPrompt}
        >
          {t('observability.prompt')}
        </Button>
      )}
      {showPrompt && call.prompt_available && !call.prompt_source_call_id && (
        <section aria-label={t('observability.instructions')} className="space-y-2">
          {error ? (
            <div role="alert">
              {error}
              <Button variant="ghost" size="xs" onClick={() => setRetry((value) => value + 1)}>
                {t('observability.retry')}
              </Button>
            </div>
          ) : !prompt ? (
            <p role="status">{t('observability.loading')}</p>
          ) : prompt.recording_status === 'expired' || prompt.recording_status === 'unrecorded' ? (
            <p>
              {t(
                prompt.recording_status === 'expired'
                  ? 'observability.promptExpired'
                  : 'observability.promptMissing'
              )}
            </p>
          ) : (
            <>
              {prompt.recording_status === 'partial' && <p>{t('observability.partial')}</p>}
              {prompt.representation === 'instructions' ? (
                <pre className="whitespace-pre-wrap break-words rounded bg-muted/40 p-3">
                  {prompt.instructions}
                </pre>
              ) : (
                prompt.messages?.map((message, index) => (
                  <div key={index}>
                    <h5 className="font-medium">{message.role}</h5>
                    <pre className="mt-1 whitespace-pre-wrap break-words rounded bg-muted/40 p-3">
                      {message.content}
                    </pre>
                  </div>
                ))
              )}
            </>
          )}
        </section>
      )}
    </div>
  )
}
