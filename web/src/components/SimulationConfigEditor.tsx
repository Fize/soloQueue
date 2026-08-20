import { Settings, Save, Loader2 } from 'lucide-react'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import type { SimulationPersona } from '@/types'
import { useTranslation } from '@/lib/i18n'

interface SimulationConfigEditorProps {
  open: boolean
  onOpenChange: (open: boolean) => void

  editTopic: string
  onEditTopicChange: (value: string) => void
  editMaxWallClockMin: number
  onEditMaxWallClockMinChange: (value: number) => void
  editSimHours: number
  onEditSimHoursChange: (value: number) => void
  editTimeScale: number
  onEditTimeScaleChange: (value: number) => void
  editEnableReflection: boolean
  onEditEnableReflectionChange: (value: boolean) => void
  editPersonas: SimulationPersona[]
  onEditPersonasChange: React.Dispatch<React.SetStateAction<SimulationPersona[]>>
  editLanguage: string
  onEditLanguageChange: (value: string) => void

  savingConfig: boolean
  onSave: () => void

  providers: { id: string; name: string }[]
  models: { id: string; name: string; providerId: string }[]
}

export function SimulationConfigEditor({
  open,
  onOpenChange,
  editTopic,
  onEditTopicChange,
  editMaxWallClockMin,
  onEditMaxWallClockMinChange,
  editSimHours,
  onEditSimHoursChange,
  editTimeScale,
  onEditTimeScaleChange,
  editEnableReflection,
  onEditEnableReflectionChange,
  editPersonas,
  onEditPersonasChange,
  editLanguage,
  onEditLanguageChange,
  savingConfig,
  onSave,
  providers,
  models,
}: SimulationConfigEditorProps) {
  const { t } = useTranslation()

  const handleUpdatePersonaOverride = (
    idx: number,
    field: 'model_id' | 'provider_id',
    value: string
  ) => {
    onEditPersonasChange((prev) => {
      const copy = [...prev]
      copy[idx] = {
        ...copy[idx],
        [field]: value || undefined,
      }
      return copy
    })
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto bg-card border border-border rounded-xl scroll-container">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-sm font-bold text-foreground">
            <Settings className="h-4.5 w-4.5 text-primary" />
            {t('simulation.editParams')}
          </DialogTitle>
        </DialogHeader>

        <div className="space-y-5 py-2">
          {/* Topic */}
          <div className="space-y-1.5">
            <Input
              label={t('simulation.simTopic')}
              value={editTopic}
              onChange={(e) => onEditTopicChange(e.target.value)}
              className="text-xs"
            />
          </div>

          {/* Wall Clock & Simulated Hours */}
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-1.5">
              <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono flex justify-between items-center">
                <span>{t('simulation.maxTime')}</span>
                <span className="text-primary font-bold">
                  {editMaxWallClockMin}m
                  {editMaxWallClockMin >= 60
                    ? ` (${(editMaxWallClockMin / 60).toFixed(1)}h)`
                    : ''}
                </span>
              </label>
              <div className="flex items-center gap-2">
                <input
                  type="range"
                  min={1}
                  max={180}
                  value={Math.min(editMaxWallClockMin, 180)}
                  onChange={(e) => onEditMaxWallClockMinChange(parseInt(e.target.value) || 5)}
                  className="flex-1 h-1.5 bg-muted rounded-lg appearance-none cursor-pointer accent-primary"
                />
                <Input
                  type="number"
                  min={1}
                  max={1440}
                  value={editMaxWallClockMin}
                  onChange={(e) => {
                    const val = Math.max(1, Math.min(1440, parseInt(e.target.value) || 1))
                    onEditMaxWallClockMinChange(val)
                  }}
                  className="w-16 text-center text-xs h-7 py-1 px-1.5 shrink-0"
                />
              </div>
            </div>
            <div className="space-y-1.5">
              <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono">
                {t('simulation.virtualTime')}: {t('simulation.hoursUnit', { count: editSimHours })}
              </label>
              <input
                type="range"
                min={6}
                max={168}
                step={6}
                value={editSimHours}
                onChange={(e) => {
                  const val = parseInt(e.target.value) || 168
                  const currentTheoryMin = (editSimHours * 60) / editTimeScale
                  const multiplier =
                    currentTheoryMin > 0 ? editMaxWallClockMin / currentTheoryMin : 3.75
                  const newTheoryMin = (val * 60) / editTimeScale
                  const newMaxMin = Math.max(
                    1,
                    Math.min(1440, Math.round(multiplier * newTheoryMin))
                  )
                  onEditSimHoursChange(val)
                  onEditMaxWallClockMinChange(newMaxMin)
                }}
                className="w-full h-1.5 bg-muted rounded-lg appearance-none cursor-pointer accent-primary"
              />
            </div>
          </div>

          {/* Time Scale & Reflection */}
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Select
                label={t('simulation.timeScale')}
                value={String(editTimeScale)}
                onChange={(v) => {
                  const newScale = parseInt(v) || 300
                  const currentTheoryMin = (editSimHours * 60) / editTimeScale
                  const multiplier =
                    currentTheoryMin > 0 ? editMaxWallClockMin / currentTheoryMin : 3.75
                  const newTheoryMin = (editSimHours * 60) / newScale
                  const newMaxMin = Math.max(
                    1,
                    Math.min(1440, Math.round(multiplier * newTheoryMin))
                  )
                  onEditTimeScaleChange(newScale)
                  onEditMaxWallClockMinChange(newMaxMin)
                }}
                options={[
                  { value: '60', label: t('simulation.timeScaleValue', { count: 1 }) },
                  { value: '300', label: t('simulation.timeScaleValue', { count: 5 }) },
                  { value: '600', label: t('simulation.timeScaleValue', { count: 10 }) },
                  { value: '1800', label: t('simulation.timeScaleValue', { count: 30 }) },
                  { value: '3600', label: t('simulation.timeScaleHour') },
                ]}
              />
            </div>
            <div className="space-y-1.5">
              <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono">
                {t('simulation.reflection')}
              </label>
              <div className="flex items-center gap-2 pt-1">
                <button
                  type="button"
                  onClick={() => onEditEnableReflectionChange(!editEnableReflection)}
                  className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors ${
                    editEnableReflection ? 'bg-primary' : 'bg-muted'
                  }`}
                >
                  <span
                    className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white transition-transform ${
                      editEnableReflection ? 'translate-x-[18px]' : 'translate-x-[3px]'
                    }`}
                  />
                </button>
                <span className="text-[10px] text-muted-foreground">
                  {editEnableReflection ? t('common.enabled') : t('common.disabled')}
                </span>
              </div>
            </div>
          </div>

          {/* Language */}
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Select
                label={t('simulation.simLang')}
                value={editLanguage}
                onChange={(v) => onEditLanguageChange(v)}
                options={[
                  { value: 'zh', label: t('simulation.chinese') },
                  { value: 'en', label: t('simulation.english') },
                ]}
              />
            </div>
          </div>

          {/* Agent Specific Models */}
          <div className="space-y-3 pt-2">
            <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono border-t border-border/40 pt-3">
              {t('simulation.agentModelConfig')}
            </label>
            <div className="space-y-2.5">
              {editPersonas.map((persona, idx) => (
                <div
                  key={persona.id || idx}
                  className="rounded-lg border border-border bg-background/55 p-3 space-y-2"
                >
                  <div className="flex items-center justify-between">
                    <span className="text-xs font-semibold text-foreground">{persona.name}</span>
                    <span className="text-[9px] text-muted-foreground font-mono">
                      {persona.role}
                    </span>
                  </div>
                  <div className="grid grid-cols-2 gap-2">
                    <div>
                      <Select
                        label={t('simulation.provider')}
                        value={persona.provider_id || ''}
                        onChange={(v) => {
                          handleUpdatePersonaOverride(idx, 'provider_id', v)
                          handleUpdatePersonaOverride(idx, 'model_id', '')
                        }}
                        placeholder={t('simulation.defaultProvider')}
                        options={[
                          { value: '', label: t('simulation.defaultProvider') },
                          ...providers.map((p) => ({ value: p.id, label: p.name })),
                        ]}
                      />
                    </div>
                    <div>
                      <Select
                        label={t('simulation.model')}
                        value={persona.model_id || ''}
                        onChange={(v) => handleUpdatePersonaOverride(idx, 'model_id', v)}
                        placeholder={t('simulation.defaultModel')}
                        options={[
                          { value: '', label: t('simulation.defaultModel') },
                          ...models
                            .filter(
                              (m) => !persona.provider_id || m.providerId === persona.provider_id
                            )
                            .map((m) => ({ value: m.id, label: m.name })),
                        ]}
                      />
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>

        <DialogFooter showCloseButton={false}>
          <button
            type="button"
            onClick={() => onOpenChange(false)}
            disabled={savingConfig}
            className="rounded-lg bg-muted hover:bg-muted/80 px-4 py-2 text-xs font-semibold text-foreground transition-colors cursor-pointer"
          >
            {t('common.cancel')}
          </button>
          <button
            type="button"
            onClick={onSave}
            disabled={savingConfig}
            className="flex items-center justify-center gap-1.5 rounded-lg bg-primary hover:bg-primary/95 disabled:bg-primary/50 px-4 py-2 text-xs font-semibold text-primary-foreground transition-all cursor-pointer shadow-md shadow-primary/5 disabled:cursor-not-allowed"
          >
            {savingConfig ? (
              <>
                <Loader2 className="h-3.5 w-3.5 animate-spin" /> {t('common.saving')}
              </>
            ) : (
              <>
                <Save className="h-3.5 w-3.5" /> {t('simulation.saveConfig')}
              </>
            )}
          </button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
