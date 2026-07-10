import { Shield } from 'lucide-react'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Button } from '@/components/ui/button'
import { useTranslation } from '@/lib/i18n'
import type { ToolsConfig } from '@/types'

interface ToolsSectionProps {
  config: ToolsConfig
  onChange: (config: ToolsConfig) => void
  onSave: () => void
}

export function ToolsSection({ config, onChange, onSave }: ToolsSectionProps) {
  const { t } = useTranslation()
  return (
    <div className="rounded-xl border bg-card p-6 shadow-sm space-y-8">
      <div className="flex items-center justify-between border-b pb-3">
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-2">
            <Shield className="h-4 w-4 text-primary" />
            <h3 className="font-semibold text-foreground">{t('config.toolsTitle')}</h3>
          </div>
          <p className="text-xs text-muted-foreground mt-0.5 leading-relaxed">
            {t('config.toolsTitleDesc')}
          </p>
        </div>
        <Button size="sm" onClick={onSave}>
          {t('config.toolsSave')}
        </Button>
      </div>

      {/* Read Limits */}
      <div>
        <h4 className="text-sm font-semibold text-foreground border-b pb-1 mb-3">
          {t('config.toolsFileRead')}
        </h4>
        <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-4">
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">
              {t('config.toolsMaxFileSize')}
            </label>
            <Input
              type="number"
              value={config.maxFileSize}
              onChange={(e) => onChange({ ...config, maxFileSize: Number(e.target.value) })}
            />
          </div>
        </div>
      </div>

      {/* Grep/Search Limits */}
      <div>
        <h4 className="text-sm font-semibold text-foreground border-b pb-1 mb-3">
          {t('config.toolsGrepSearch')}
        </h4>
        <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-4">
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">{t('config.toolsMaxMatches')}</label>
            <Input
              type="number"
              value={config.maxMatches}
              onChange={(e) => onChange({ ...config, maxMatches: Number(e.target.value) })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">{t('config.toolsMaxLineLength')}</label>
            <Input
              type="number"
              value={config.maxLineLen}
              onChange={(e) => onChange({ ...config, maxLineLen: Number(e.target.value) })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">
              {t('config.toolsMaxGlobItems')}
            </label>
            <Input
              type="number"
              value={config.maxGlobItems}
              onChange={(e) => onChange({ ...config, maxGlobItems: Number(e.target.value) })}
            />
          </div>
        </div>
      </div>

      {/* File Write Limits */}
      <div>
        <h4 className="text-sm font-semibold text-foreground border-b pb-1 mb-3">
          {t('config.toolsFileWrite')}
        </h4>
        <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-4 gap-4">
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">
              {t('config.toolsMaxWriteSize')}
            </label>
            <Input
              type="number"
              value={config.maxWriteSize}
              onChange={(e) => onChange({ ...config, maxWriteSize: Number(e.target.value) })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">
              {t('config.toolsMaxMultiWriteBytes')}
            </label>
            <Input
              type="number"
              value={config.maxMultiWriteBytes}
              onChange={(e) => onChange({ ...config, maxMultiWriteBytes: Number(e.target.value) })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">
              {t('config.toolsMaxMultiWriteFiles')}
            </label>
            <Input
              type="number"
              value={config.maxMultiWriteFiles}
              onChange={(e) => onChange({ ...config, maxMultiWriteFiles: Number(e.target.value) })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">{t('config.toolsMaxReplaceEdits')}</label>
            <Input
              type="number"
              value={config.maxReplaceEdits}
              onChange={(e) => onChange({ ...config, maxReplaceEdits: Number(e.target.value) })}
            />
          </div>
        </div>
      </div>

      {/* Web Search & Fetch */}
      <div>
        <h4 className="text-sm font-semibold text-foreground border-b pb-1 mb-3">
          {t('config.toolsWebSearch')}
        </h4>
        <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-4">
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">
              {t('config.toolsHttpMaxBody')}
            </label>
            <Input
              type="number"
              value={config.httpMaxBody}
              onChange={(e) => onChange({ ...config, httpMaxBody: Number(e.target.value) })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">{t('config.toolsHttpTimeout')}</label>
            <Input
              type="number"
              value={config.httpTimeoutMs}
              onChange={(e) => onChange({ ...config, httpTimeoutMs: Number(e.target.value) })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">
              {t('config.toolsWebSearchTimeout')}
            </label>
            <Input
              type="number"
              value={config.webSearchTimeoutMs}
              onChange={(e) => onChange({ ...config, webSearchTimeoutMs: Number(e.target.value) })}
            />
          </div>
          <div className="flex items-center gap-2 pt-4">
            <Switch
              checked={config.httpBlockPrivate}
              onCheckedChange={(val) => onChange({ ...config, httpBlockPrivate: val })}
            />
            <span className="text-xs font-semibold text-foreground">{t('config.toolsBlockPrivateNetworks')}</span>
          </div>
        </div>
        <div className="mt-4 flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground">
            {t('config.toolsAllowedHosts')}
          </label>
          <Input
            type="text"
            placeholder={t('config.toolsAllowedHostsPlaceholder')}
            value={config.httpAllowedHosts?.join(', ') || ''}
            onChange={(e) =>
              onChange({
                ...config,
                httpAllowedHosts: e.target.value
                  .split(',')
                  .map((s) => s.trim())
                  .filter(Boolean),
              })
            }
          />
        </div>
      </div>

      {/* Shell Execute / Bash */}
      <div>
        <h4 className="text-sm font-semibold text-foreground border-b pb-1 mb-3">
          {t('config.toolsShellBash')}
        </h4>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">
              {t('config.toolsShellMaxOutput')}
            </label>
            <Input
              type="number"
              value={config.shellMaxOutput}
              onChange={(e) => onChange({ ...config, shellMaxOutput: Number(e.target.value) })}
            />
          </div>
        </div>
        <div className="grid grid-cols-1 gap-4 mt-4">
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">
              {t('config.toolsBlockCommandRegex')}
            </label>
            <Input
              type="text"
              placeholder={t('config.toolsBlockCommandPlaceholder')}
              value={config.shellBlockRegexes?.join(', ') || ''}
              onChange={(e) =>
                onChange({
                  ...config,
                  shellBlockRegexes: e.target.value
                    .split(',')
                    .map((s) => s.trim())
                    .filter(Boolean),
                })
              }
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">
              {t('config.toolsConfirmCommandRegex')}
            </label>
            <Input
              type="text"
              placeholder={t('config.toolsConfirmCommandPlaceholder')}
              value={config.shellConfirmRegexes?.join(', ') || ''}
              onChange={(e) =>
                onChange({
                  ...config,
                  shellConfirmRegexes: e.target.value
                    .split(',')
                    .map((s) => s.trim())
                    .filter(Boolean),
                })
              }
            />
          </div>
        </div>
      </div>
    </div>
  )
}
