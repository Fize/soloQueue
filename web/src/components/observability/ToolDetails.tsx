import type { ObservedCall } from '@/lib/api/observability-api'
import { useTranslation } from '@/lib/i18n'

export function ToolInspector({ tool }: { tool: ObservedCall }) {
  const { t } = useTranslation()
  const formatted = (text: string) => {
    try {
      return JSON.stringify(JSON.parse(text), null, 2)
    } catch {
      return text
    }
  }
  return (
    <div className="space-y-4 text-xs">
      <dl className="grid gap-3 break-words sm:grid-cols-2">
        <div>
          <dt className="text-muted-foreground">{t('observability.duration')}</dt>
          <dd>{tool.duration_ms} ms</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">{t('observability.recording')}</dt>
          <dd>{t(`observability.${tool.recording_status}`)}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">{t('observability.agent')}</dt>
          <dd className="font-mono">{tool.agent_id || '—'}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">{t('observability.started')}</dt>
          <dd>{new Date(tool.started_at).toLocaleString()}</dd>
        </div>
      </dl>
      {tool.recording_status !== 'complete' && (
        <p role="status">{t(`observability.${tool.recording_status}`)}</p>
      )}
      {tool.error && (
        <pre role="alert" className="whitespace-pre-wrap break-words text-destructive">
          {tool.error}
        </pre>
      )}
      <section aria-label={t('observability.toolArguments')}>
        <h4 className="mb-2 font-medium">{t('observability.toolArguments')}</h4>
        {(tool.arguments !== undefined && tool.recording_status === 'complete') ||
        tool.arguments ? (
          <pre className="whitespace-pre-wrap break-words rounded bg-muted/40 p-3">
            {formatted(tool.arguments || '')}
          </pre>
        ) : (
          <p>{t('observability.toolPayloadMissing')}</p>
        )}
      </section>
      <section aria-label={t('observability.toolResult')}>
        <h4 className="mb-2 font-medium">{t('observability.toolResult')}</h4>
        {tool.status === 'running' ? (
          <p role="status">{t('observability.running')}</p>
        ) : tool.recording_status === 'complete' || tool.content ? (
          <pre className="whitespace-pre-wrap break-words rounded bg-muted/40 p-3">
            {tool.content || ''}
          </pre>
        ) : (
          <p>{t('observability.toolPayloadMissing')}</p>
        )}
      </section>
    </div>
  )
}
