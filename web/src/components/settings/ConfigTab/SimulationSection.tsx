import { Activity } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { useTranslation } from '@/lib/i18n'
import type { SimulationConfig, LLMProvider, LLMModel } from '@/types'

interface SimulationSectionProps {
  config: SimulationConfig
  onChange: (config: SimulationConfig) => void
  onSave: () => void
  providers: LLMProvider[]
  models: LLMModel[]
}

export function SimulationSection({
  config,
  onChange,
  onSave,
  providers,
  models,
}: SimulationSectionProps) {
  const { t } = useTranslation()
  const currentHours = config.simulatedHours || 168
  const currentScale = config.timeScale || 300
  const currentMaxMs = config.defaultMaxWallClockMs || 18 * 60 * 1000
  const currentMaxMin = currentMaxMs / 60000
  const theoryMin = (currentHours * 60) / currentScale
  const multiplier = theoryMin > 0 ? currentMaxMin / theoryMin : 3.75

  return (
    <div className="rounded-xl border bg-card p-6 shadow-sm space-y-6">
      <div className="flex items-center justify-between border-b pb-3">
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-2">
            <Activity className="h-4 w-4 text-primary" />
            <h3 className="font-semibold text-foreground">{t('config.simTitle')}</h3>
          </div>
          <p className="text-xs text-muted-foreground mt-0.5 leading-relaxed">
            {t('config.simTitleDesc')}
          </p>
        </div>
        <Button size="sm" onClick={onSave}>
          {t('config.simSave')}
        </Button>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground">{t('config.simDefaultProvider')}</label>
          <Select
            value={config.defaultProviderId || ''}
            onChange={(v) => onChange({ ...config, defaultProviderId: v })}
            placeholder={t('config.simDefaultFastProvider')}
            options={[
              { value: '', label: t('config.simDefaultFastProvider') },
              ...providers.map((p) => ({ value: p.id, label: p.name })),
            ]}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground">{t('config.simDefaultModel')}</label>
          <Select
            value={config.defaultModelId || ''}
            onChange={(v) => onChange({ ...config, defaultModelId: v })}
            placeholder={t('config.simDefaultFastModel')}
            options={[
              { value: '', label: t('config.simDefaultFastModel') },
              ...models
                .filter((m) => m.enabled)
                .map((m) => ({
                  value: m.id,
                  label: `${m.name} (${m.providerId})`,
                })),
            ]}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground">{t('config.simDbPath')}</label>
          <Input
            type="text"
            placeholder={t('config.simDbPathPlaceholder')}
            value={config.dbPath || ''}
            onChange={(e) => onChange({ ...config, dbPath: e.target.value })}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground">{t('config.simTickInterval')}</label>
          <Input
            type="number"
            value={config.tickIntervalMs || 1000}
            onChange={(e) => onChange({ ...config, tickIntervalMs: Number(e.target.value) })}
          />
          <p className="text-[10px] text-muted-foreground leading-normal">
            {t('config.simTickIntervalDesc')}
          </p>
        </div>
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground">
            {t('config.simSimulatedHours', { hours: config.simulatedHours || 168 })}
          </label>
          <input
            type="range"
            min={6}
            max={168}
            step={6}
            value={config.simulatedHours || 168}
            onChange={(e) => {
              const newHours = parseInt(e.target.value) || 168
              const newTheoryMin = (newHours * 60) / currentScale
              const newMaxMin = Math.max(1, Math.min(1440, Math.round(multiplier * newTheoryMin)))
              onChange({
                ...config,
                simulatedHours: newHours,
                defaultMaxWallClockMs: newMaxMin * 60 * 1000,
              })
            }}
            className="w-full h-1.5 bg-muted rounded-lg appearance-none cursor-pointer accent-primary"
          />
          <p className="text-[10px] text-muted-foreground leading-normal">
            {t('config.simSimulatedHoursDesc')}
          </p>
        </div>
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground flex justify-between items-center">
            <span>{t('config.simMaxClock')}</span>
            <span className="text-primary font-mono font-bold">
              {config.defaultMaxWallClockMs ? Math.round(config.defaultMaxWallClockMs / 60000) : 18}{' '}
              {t('config.simMinUnit')}
              {config.defaultMaxWallClockMs && config.defaultMaxWallClockMs >= 3600000
                ? ` (${(config.defaultMaxWallClockMs / 3600000).toFixed(1)} ${t('config.simHoursUnit')})`
                : ''}
            </span>
          </label>
          <div className="flex items-center gap-3">
            <input
              type="range"
              min={1}
              max={180}
              value={
                config.defaultMaxWallClockMs
                  ? Math.min(Math.round(config.defaultMaxWallClockMs / 60000), 180)
                  : 18
              }
              onChange={(e) =>
                onChange({
                  ...config,
                  defaultMaxWallClockMs: parseInt(e.target.value) * 60 * 1000,
                })
              }
              className="flex-1 h-1.5 bg-muted rounded-lg appearance-none cursor-pointer accent-primary"
            />
            <Input
              type="number"
              min={1}
              max={1440}
              value={
                config.defaultMaxWallClockMs ? Math.round(config.defaultMaxWallClockMs / 60000) : 18
              }
              onChange={(e) => {
                const val = Math.max(1, Math.min(1440, parseInt(e.target.value) || 1))
                onChange({
                  ...config,
                  defaultMaxWallClockMs: val * 60 * 1000,
                })
              }}
              className="w-20 text-center text-xs h-8 py-1 px-2 shrink-0"
            />
          </div>
          <p className="text-[10px] text-muted-foreground leading-normal">
            {t('config.simMaxClockDesc')}
          </p>
        </div>
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground">{t('config.simTimeScale')}</label>
          <Select
            value={String(config.timeScale || 300)}
            onChange={(v) => {
              const newScale = parseInt(v) || 300
              const newTheoryMin = (currentHours * 60) / newScale
              const newMaxMin = Math.max(1, Math.min(1440, Math.round(multiplier * newTheoryMin)))
              onChange({
                ...config,
                timeScale: newScale,
                defaultMaxWallClockMs: newMaxMin * 60 * 1000,
              })
            }}
            options={[
              { value: '60', label: t('config.simTimeScale1m') },
              { value: '300', label: t('config.simTimeScale5m') },
              { value: '600', label: t('config.simTimeScale10m') },
              { value: '1800', label: t('config.simTimeScale30m') },
              { value: '3600', label: t('config.simTimeScale1h') },
            ]}
          />
          <p className="text-[10px] text-muted-foreground leading-normal">
            {t('config.simTimeScaleDesc')}
          </p>
        </div>
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground">{t('config.simDefaultLanguage')}</label>
          <Select
            value={config.language || 'zh'}
            onChange={(v) => onChange({ ...config, language: v })}
            options={[
              { value: 'zh', label: t('config.simLanguageChinese') },
              { value: 'en', label: t('config.simLanguageEnglish') },
            ]}
          />
          <p className="text-[10px] text-muted-foreground leading-normal">
            {t('config.simLanguageDesc')}
          </p>
        </div>
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground">
            {t('config.simReflection')}
          </label>
          <div className="flex items-center gap-2 mt-1">
            <Switch
              checked={config.enableReflection || false}
              onCheckedChange={(val) => onChange({ ...config, enableReflection: val })}
            />
            <span className="text-xs text-muted-foreground">
              {config.enableReflection ? t('common.enabled') : t('common.disabled')}
            </span>
          </div>
          <p className="text-[10px] text-muted-foreground leading-normal">
            {t('config.simReflectionDesc')}
          </p>
        </div>
      </div>
    </div>
  )
}