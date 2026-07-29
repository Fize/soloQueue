import { Box, Monitor, ShieldCheck, TriangleAlert } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import type { SandboxConfig, SandboxRuntimeStatus } from '@/types'
import { useTranslation } from '@/lib/i18n'

interface SandboxSectionProps {
  config: SandboxConfig
  status: SandboxRuntimeStatus | null
  onChange: (config: SandboxConfig) => void
  onSave: () => void
}

export function SandboxSection({ config, status, onChange, onSave }: SandboxSectionProps) {
  const { t } = useTranslation()
  const selectRuntime = (runtime: 'host' | 'sandbox') => {
    onChange({
      ...config,
      runtime,
      enabled: runtime === 'sandbox',
      backend: config.backend || 'docker',
    })
  }

  return (
    <div className="rounded-xl border bg-card p-6 shadow-sm space-y-6">
      <div className="flex items-center justify-between border-b pb-3">
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-2">
            <Box className="h-4 w-4 text-primary" />
            <h3 className="font-semibold text-foreground">{t('config.sandboxTitle')}</h3>
            <span className="text-[10px] font-mono uppercase bg-amber-500/15 text-amber-600 dark:text-amber-400 px-2 py-0.5 rounded border border-amber-500/30">
              {t('config.sandboxBadge')}
            </span>
          </div>
          <p className="text-xs text-muted-foreground mt-0.5 leading-relaxed">
            {t('config.sandboxDesc')}
          </p>
        </div>
        <Button size="sm" onClick={onSave}>
          {t('config.saveConfig')}
        </Button>
      </div>

      {config.runtime === 'sandbox' && (
        <div className="flex items-center justify-between rounded-lg border border-border p-4">
          <div>
            <div className="text-sm font-medium">{t('config.sandboxNetwork')}</div>
            <p className="mt-1 text-xs text-muted-foreground">
              {t('config.sandboxNetworkDesc')}
            </p>
          </div>
          <Switch
            checked={config.network_enabled}
            onCheckedChange={(network_enabled) => onChange({ ...config, network_enabled })}
          />
        </div>
      )}

      <div className="grid gap-3 md:grid-cols-2">
        <button
          type="button"
          onClick={() => selectRuntime('host')}
          className={`rounded-lg border p-4 text-left transition-colors ${
            config.runtime === 'host'
              ? 'border-amber-500 bg-amber-500/10'
              : 'border-border hover:border-amber-500/50'
          }`}
        >
          <div className="flex items-center gap-2">
            <Monitor className="h-4 w-4 text-amber-500" />
            <span className="text-sm font-semibold">{t('config.runtimeHost')}</span>
          </div>
          <p className="mt-2 text-xs leading-relaxed text-muted-foreground">
            {t('config.runtimeHostDesc')}
          </p>
        </button>

        <button
          type="button"
          onClick={() => selectRuntime('sandbox')}
          className={`rounded-lg border p-4 text-left transition-colors ${
            config.runtime === 'sandbox'
              ? 'border-emerald-500 bg-emerald-500/10'
              : 'border-border hover:border-emerald-500/50'
          }`}
        >
          <div className="flex items-center gap-2">
            <ShieldCheck className="h-4 w-4 text-emerald-500" />
            <span className="text-sm font-semibold">{t('config.runtimeSandbox')}</span>
          </div>
          <p className="mt-2 text-xs leading-relaxed text-muted-foreground">
            {t('config.runtimeSandboxDesc')}
          </p>
        </button>
      </div>

      {status && (
        <div className="rounded-lg border bg-muted/30 px-4 py-3 text-xs">
          <div className="flex items-center justify-between gap-3">
            <span className="font-medium text-foreground">{t('config.sandboxStatus')}</span>
            <span className={`font-mono ${
              status.state === 'ready' ? 'text-emerald-600 dark:text-emerald-400' :
              status.state === 'failed' ? 'text-destructive' :
              'text-amber-600 dark:text-amber-400'
            }`}>
              {status.state}
            </span>
          </div>
          <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-muted-foreground">
            <span>{t('config.sandboxRuntime')}: {status.desired_runtime}</span>
            {status.backend && <span>{t('config.sandboxBackend')}: {status.backend}</span>}
            <span>
              {t('config.sandboxNetwork')}: {status.network_enabled ? t('config.sandboxNetworkOn') : t('config.sandboxNetworkOff')}
            </span>
            <span>
              {t('config.sandboxIsolation')}:{' '}
              {status.isolation_complete
                ? t('config.sandboxIsolationComplete')
                : t('config.sandboxIsolationIncomplete')}
            </span>
            {status.host_exceptions > 0 && (
              <span className="text-amber-600 dark:text-amber-400">
                {t('config.sandboxHostExceptions')}: {status.host_exceptions}
              </span>
            )}
          </div>
          {status.last_error && (
            <div className="mt-2 flex items-start gap-2 text-destructive">
              <TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              <span>{status.last_error}</span>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
