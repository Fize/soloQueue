import { CircleCheck, CircleX, Download, Loader2, Mic, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Label } from '@/components/ui/label'
import { useTranslation } from '@/lib/i18n'
import type { SpeechConfig, SpeechStatus } from '@/types'

interface SpeechSectionProps {
  config: SpeechConfig
  onChange: (config: SpeechConfig) => void
  onSave: () => void
  status: SpeechStatus | null
  onInstall: () => void
  installing: boolean
  installError: string | null
  onDismissError: () => void
}

export function SpeechSection({ config, onChange, onSave, status, onInstall, installing, installError, onDismissError }: SpeechSectionProps) {
  const { t } = useTranslation()

  const statusIndicator = (label: string, ok: boolean) => (
    <span className={`inline-flex items-center gap-1 text-xs ${ok ? 'text-green-600 dark:text-green-400' : 'text-red-500'}`}>
      {ok ? <CircleCheck className="size-3.5" /> : <CircleX className="size-3.5" />}
      {label}
    </span>
  )

  return (
    <section className="rounded-xl border border-border bg-card p-6 shadow-sm space-y-6">
      <div className="flex items-center justify-between border-b border-border pb-3">
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-2">
            <Mic className="h-4 w-4 text-primary" />
            <h3 className="font-semibold text-foreground">{t('config.sectionSpeech')}</h3>
            {status && (
              <span className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ${
                status.ready ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400' : 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400'
              }`}>
                {status.ready ? t('config.speechReady') : t('config.speechNotReady')}
              </span>
            )}
          </div>
          <p className="text-xs text-muted-foreground mt-0.5 leading-relaxed">
            {t('config.speechDesc')}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {!status?.ready && config.enabled && (
            <Button size="sm" variant="outline" onClick={onInstall} disabled={installing}>
              {installing ? <Loader2 className="size-3.5 animate-spin" /> : <Download className="size-3.5" />}
              {installing ? t('config.speechInstalling') : t('config.speechInstall')}
            </Button>
          )}
          <Button size="sm" onClick={onSave}>
            {t('config.speechSave')}
          </Button>
        </div>
      </div>

      <div className="flex items-center justify-between gap-4">
        <div className="flex flex-col gap-1">
          <Label htmlFor="speech-enabled" className="text-sm font-medium">
            {t('config.speechEnabled')}
          </Label>
          <p className="text-xs text-muted-foreground">
            {t('config.speechEnabledDesc')}
          </p>
        </div>
        <Switch
          id="speech-enabled"
          checked={config.enabled}
          onCheckedChange={(checked) => onChange({ ...config, enabled: checked })}
        />
      </div>

      {config.enabled && (
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="speech-model" className="text-xs font-semibold text-muted-foreground">
              {t('config.speechModel')}
            </Label>
            <select
              id="speech-model"
              className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
              value={config.model || 'small'}
              onChange={(e) => onChange({ ...config, model: e.target.value })}
            >
              <option value="tiny">tiny (75 MB)</option>
              <option value="base">base (140 MB)</option>
              <option value="small">small (460 MB)</option>
              <option value="medium">medium (1.5 GB)</option>
            </select>
            <p className="text-xs text-muted-foreground">
              {t('config.speechModelDesc')}
            </p>
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="speech-model-dir" className="text-xs font-semibold text-muted-foreground">
              {t('config.speechModelDir')}
            </Label>
            <Input
              id="speech-model-dir"
              type="text"
              value={config.modelDir || ''}
              placeholder="~/.soloqueue/models"
              onChange={(e) => onChange({ ...config, modelDir: e.target.value })}
            />
            <p className="text-xs text-muted-foreground">
              {t('config.speechModelDirDesc')}
            </p>
          </div>
        </div>
      )}

      {status && config.enabled && (
        <div className="rounded-md border border-border bg-muted/30 p-3 space-y-1.5">
          <p className="text-xs font-semibold text-muted-foreground">{t('config.speechStatus')}</p>
          <div className="flex flex-wrap gap-x-6 gap-y-1">
            {statusIndicator('whisper-cli', status.whisperAvailable)}
            {statusIndicator(t('config.speechModelFile'), status.modelExists)}
          </div>
          {status.whisperAvailable && (
            <p className="text-xs text-muted-foreground/70 font-mono">{status.whisperBinary}</p>
          )}
          {status.modelExists && (
            <p className="text-xs text-muted-foreground/70 font-mono">{status.modelPath}</p>
          )}
        </div>
      )}

      {installError && (
        <div className="rounded-md border border-red-200 dark:border-red-800 bg-red-50 dark:bg-red-950/30 p-3">
          <div className="flex items-start justify-between gap-2">
            <div className="flex-1">
              <p className="text-xs font-semibold text-red-700 dark:text-red-400 mb-1">
                {t('config.speechInstallFailed')}
              </p>
              <pre className="text-xs text-red-600 dark:text-red-300 whitespace-pre-wrap font-mono leading-relaxed">
                {installError}
              </pre>
            </div>
            <Button size="icon-xs" variant="ghost" onClick={onDismissError} aria-label={t('common.dismiss')}>
              <X className="size-3.5" />
            </Button>
          </div>
        </div>
      )}
    </section>
  )
}
