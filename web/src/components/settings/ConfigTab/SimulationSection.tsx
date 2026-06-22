import { Database } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
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
  const currentHours = config.simulatedHours || 48
  const currentScale = config.timeScale || 600
  const currentMaxMs = config.defaultMaxWallClockMs || 18 * 60 * 1000
  const currentMaxMin = currentMaxMs / 60000
  const theoryMin = (currentHours * 60) / currentScale
  const multiplier = theoryMin > 0 ? currentMaxMin / theoryMin : 3.75

  return (
    <div className="rounded-xl border bg-card p-6 shadow-sm space-y-6">
      <div className="flex items-center justify-between border-b pb-3">
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-2">
            <Database className="h-4 w-4 text-primary" />
            <h3 className="font-semibold text-foreground">Simulation Config</h3>
          </div>
          <p className="text-xs text-muted-foreground mt-0.5 leading-relaxed">
            Simulation Config: Define default settings for multi-agent simulation environments.
          </p>
        </div>
        <Button size="sm" onClick={onSave}>
          Save Simulation Settings
        </Button>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground">Default Provider</label>
          <Select
            value={config.defaultProviderId || ''}
            onChange={(v) => onChange({ ...config, defaultProviderId: v })}
            placeholder="(Default Fast Provider)"
            options={[
              { value: '', label: '(Default Fast Provider)' },
              ...providers.map((p) => ({ value: p.id, label: p.name })),
            ]}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground">Default Model</label>
          <Select
            value={config.defaultModelId || ''}
            onChange={(v) => onChange({ ...config, defaultModelId: v })}
            placeholder="(Default Fast Model)"
            options={[
              { value: '', label: '(Default Fast Model)' },
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
          <label className="text-xs font-semibold text-muted-foreground">Database Path</label>
          <Input
            type="text"
            placeholder="e.g. ~/.soloqueue/simulation.db"
            value={config.dbPath || ''}
            onChange={(e) => onChange({ ...config, dbPath: e.target.value })}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground">Tick Interval (ms)</label>
          <Input
            type="number"
            value={config.tickIntervalMs || 500}
            onChange={(e) => onChange({ ...config, tickIntervalMs: Number(e.target.value) })}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground">
            Simulated Hours ({config.simulatedHours || 48}h)
          </label>
          <input
            type="range"
            min={6}
            max={168}
            step={6}
            value={config.simulatedHours || 48}
            onChange={(e) => {
              const newHours = parseInt(e.target.value) || 48
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
            Default time span simulated within the world when creating a new simulation.
          </p>
        </div>
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground flex justify-between items-center">
            <span>Max Clock / Default timeout</span>
            <span className="text-primary font-mono font-bold">
              {config.defaultMaxWallClockMs ? Math.round(config.defaultMaxWallClockMs / 60000) : 18}{' '}
              min
              {config.defaultMaxWallClockMs && config.defaultMaxWallClockMs >= 3600000
                ? ` (${(config.defaultMaxWallClockMs / 3600000).toFixed(1)} hours)`
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
            Physical clock timeout limit during simulation runs (1 minute to 24 hours).
          </p>
        </div>
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground">Time Scale</label>
          <Select
            value={String(config.timeScale || 600)}
            onChange={(v) => {
              const newScale = parseInt(v) || 600
              const newTheoryMin = (currentHours * 60) / newScale
              const newMaxMin = Math.max(1, Math.min(1440, Math.round(multiplier * newTheoryMin)))
              onChange({
                ...config,
                timeScale: newScale,
                defaultMaxWallClockMs: newMaxMin * 60 * 1000,
              })
            }}
            options={[
              { value: '60', label: '1s = 1min' },
              { value: '300', label: '1s = 5min' },
              { value: '600', label: '1s = 10min' },
              { value: '1800', label: '1s = 30min' },
              { value: '3600', label: '1s = 1h' },
            ]}
          />
          <p className="text-[10px] text-muted-foreground leading-normal">
            Ratio of physical clock time to simulated world time.
          </p>
        </div>
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground">Default Language</label>
          <Select
            value={config.language || 'zh'}
            onChange={(v) => onChange({ ...config, language: v })}
            options={[
              { value: 'zh', label: '中文 (Chinese)' },
              { value: 'en', label: 'English' },
            ]}
          />
          <p className="text-[10px] text-muted-foreground leading-normal">
            Default language for generated agents, dialogue process, and reports.
          </p>
        </div>
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground">
            Reflection / Default reflection
          </label>
          <div className="flex items-center gap-2 mt-1">
            <Switch
              checked={config.enableReflection || false}
              onCheckedChange={(val) => onChange({ ...config, enableReflection: val })}
            />
            <span className="text-xs text-muted-foreground">
              {config.enableReflection ? 'Enabled' : 'Disabled'}
            </span>
          </div>
          <p className="text-[10px] text-muted-foreground leading-normal">
            Enable memory reflection by default when creating a new simulation.
          </p>
        </div>
      </div>
    </div>
  )
}
