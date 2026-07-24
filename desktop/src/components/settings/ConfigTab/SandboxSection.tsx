import { Box } from 'lucide-react'
import { Switch } from '@/components/ui/switch'
import { Button } from '@/components/ui/button'
import type { SandboxConfig } from '@/types'
import { useTranslation } from '@/lib/i18n'

interface SandboxSectionProps {
  config: SandboxConfig
  onChange: (config: SandboxConfig) => void
  onSave: () => void
}

export function SandboxSection({ config, onChange, onSave }: SandboxSectionProps) {
  const { t } = useTranslation()

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

      <div className="flex items-center justify-between pt-2">
        <div className="flex flex-col gap-0.5">
          <span className="text-sm font-semibold text-foreground">{t('config.sandboxEnable')}</span>
          <span className="text-xs text-muted-foreground">
            {t('config.sandboxEnableDesc')}
          </span>
        </div>
        <Switch
          checked={config.enabled}
          onCheckedChange={(val) => onChange({ ...config, enabled: val })}
        />
      </div>
    </div>
  )
}
