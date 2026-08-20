import { Brain, Plus, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { useTranslation } from '@/lib/i18n'
import type { EmbeddingConfig } from '@/types'

interface EmbeddingSectionProps {
  config: EmbeddingConfig
  onChange: (config: EmbeddingConfig) => void
  onSave: () => void
  onAddProvider: () => void
  onRemoveProvider: (index: number) => void
  onUpdateProvider: (index: number, field: string, val: any) => void
  onAddModel: () => void
  onRemoveModel: (index: number) => void
  onUpdateModel: (index: number, field: string, val: any) => void
}

export function EmbeddingSection({
  config,
  onChange,
  onSave,
  onAddProvider,
  onRemoveProvider,
  onUpdateProvider,
  onAddModel,
  onRemoveModel,
  onUpdateModel,
}: EmbeddingSectionProps) {
  const { t } = useTranslation()
  return (
    <div className="rounded-xl border bg-card p-6 shadow-sm space-y-8">
      <div className="flex items-center justify-between border-b pb-3">
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-2">
            <Brain className="h-4 w-4 text-primary" />
            <h3 className="font-semibold text-foreground">{t('config.embedTitle')}</h3>
          </div>
          <p className="text-xs text-muted-foreground mt-0.5 leading-relaxed">
            {t('config.embedTitleDesc')}
          </p>
        </div>
        <Button size="sm" onClick={onSave}>
          {t('config.embedSave')}
        </Button>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <div className="flex items-center gap-2">
          <Switch
            checked={config.enabled}
            onCheckedChange={(val) => onChange({ ...config, enabled: val })}
          />
          <span className="text-xs font-semibold text-foreground">
            {t('config.embedEnablePermanent')}
          </span>
        </div>
        <div className="flex flex-col gap-1.5">
          <Select
            label={t('config.embedProvider')}
            value={config.provider || ''}
            onChange={(v) => onChange({ ...config, provider: v })}
            placeholder={t('config.embedProviderNone')}
            options={[
              { value: '', label: t('config.embedProviderNone') },
              { value: 'none', label: t('config.embedProviderNoneDesc') },
              { value: 'openai', label: t('config.embedProviderOpenai') },
            ]}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground">
            {t('config.embedApiModel')}
          </label>
          <Input
            type="text"
            placeholder={t('config.embedApiModelPlaceholder')}
            value={config.modelName || ''}
            onChange={(e) => onChange({ ...config, modelName: e.target.value })}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground">
            {t('config.embedMinSimilarity')}
          </label>
          <Input
            type="number"
            step="0.01"
            min="0"
            max="1"
            value={config.minSimilarity ?? 0.65}
            onChange={(e) => onChange({ ...config, minSimilarity: Number(e.target.value) })}
          />
        </div>
      </div>

      {/* Embedding Providers Section */}
      {config.provider === 'openai' && (
        <>
          <div className="space-y-4 pt-4 border-t">
            <div className="flex items-center justify-between">
              <h4 className="text-sm font-semibold text-foreground">{t('config.embedProvidersSection')}</h4>
              <Button size="xs" variant="outline" onClick={onAddProvider}>
                <Plus className="h-3 w-3 mr-1" /> {t('config.embedAddProvider')}
              </Button>
            </div>

            <div className="space-y-3">
              {(config.providers || []).map((prov, idx) => (
                <div
                  key={prov.id || idx}
                  className="p-4 border rounded-lg relative space-y-4 bg-muted/20"
                >
                  <button
                    type="button"
                    onClick={() => onRemoveProvider(idx)}
                    className="absolute top-4 right-4 text-muted-foreground hover:text-destructive transition-colors"
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                  <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-4">
                    <div className="flex flex-col gap-1">
                      <label className="text-xs font-semibold text-muted-foreground font-mono">
                        {t('config.embedProviderId')}
                      </label>
                      <Input
                        type="text"
                        placeholder={t('config.embedProviderIdPlaceholder')}
                        value={prov.id || ''}
                        onChange={(e) => onUpdateProvider(idx, 'id', e.target.value)}
                      />
                    </div>
                    <div className="flex flex-col gap-1">
                      <label className="text-xs font-semibold text-muted-foreground">
                        {t('config.embedProviderName')}
                      </label>
                      <Input
                        type="text"
                        placeholder={t('config.embedProviderNamePlaceholder')}
                        value={prov.name || ''}
                        onChange={(e) => onUpdateProvider(idx, 'name', e.target.value)}
                      />
                    </div>
                    <div className="flex flex-col gap-1">
                      <label className="text-xs font-semibold text-muted-foreground">
                        {t('config.embedBaseUrl')}
                      </label>
                      <Input
                        type="text"
                        placeholder={t('config.embedBaseUrlPlaceholder')}
                        value={prov.baseUrl || ''}
                        onChange={(e) => onUpdateProvider(idx, 'baseUrl', e.target.value)}
                      />
                    </div>
                    <div className="flex flex-col gap-1">
                      <label className="text-xs font-semibold text-muted-foreground">
                        {t('config.llmApiKeyDirect')}
                      </label>
                      <Input
                        type="password"
                        placeholder={t('config.embedApiKeyDirectPlaceholder')}
                        value={prov.apiKey || ''}
                        onChange={(e) => onUpdateProvider(idx, 'apiKey', e.target.value)}
                      />
                    </div>
                    <div className="flex flex-col gap-1">
                      <label className="text-xs font-semibold text-muted-foreground font-mono">
                        {t('config.embedApiKeyEnv')}
                      </label>
                      <Input
                        type="text"
                        placeholder={t('config.embedApiKeyEnvPlaceholder')}
                        value={prov.apiKeyEnv || ''}
                        onChange={(e) => onUpdateProvider(idx, 'apiKeyEnv', e.target.value)}
                      />
                    </div>
                    <div className="flex items-center gap-2 pt-6">
                      <Switch
                        checked={prov.enabled}
                        onCheckedChange={(val) => onUpdateProvider(idx, 'enabled', val)}
                      />
                      <span className="text-xs font-semibold text-foreground">{t('common.enabled')}</span>
                    </div>
                  </div>
                </div>
              ))}
              {(config.providers || []).length === 0 && (
                <div className="text-center p-6 border border-dashed rounded-xl text-muted-foreground text-xs">
                  {t('config.embedNoProviders')}
                </div>
              )}
            </div>
          </div>

          {/* Embedding Models Section */}
          <div className="space-y-4 pt-4 border-t">
            <div className="flex items-center justify-between">
              <h4 className="text-sm font-semibold text-foreground">{t('config.embedModelsSection')}</h4>
              <Button size="xs" variant="outline" onClick={onAddModel}>
                <Plus className="h-3 w-3 mr-1" /> {t('config.embedAddModel')}
              </Button>
            </div>

            <div className="space-y-3">
              {(config.models || []).map((mdl, idx) => (
                <div
                  key={mdl.id || idx}
                  className="p-4 border rounded-lg relative space-y-4 bg-muted/20"
                >
                  <button
                    type="button"
                    onClick={() => onRemoveModel(idx)}
                    className="absolute top-4 right-4 text-muted-foreground hover:text-destructive transition-colors"
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                  <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-4">
                    <div className="flex flex-col gap-1">
                      <label className="text-xs font-semibold text-muted-foreground font-mono">
                        {t('config.embedModelId')}
                      </label>
                      <Input
                        type="text"
                        placeholder={t('config.embedModelIdPlaceholder')}
                        value={mdl.id || ''}
                        onChange={(e) => onUpdateModel(idx, 'id', e.target.value)}
                      />
                    </div>
                    <div className="flex flex-col gap-1 font-mono">
                      <label className="text-xs font-semibold text-muted-foreground">
                        {t('config.embedModelProviderId')}
                      </label>
                      <Select
                        value={mdl.providerId || ''}
                        onChange={(v) => onUpdateModel(idx, 'providerId', v)}
                        placeholder={t('config.embedSelectProvider')}
                        options={[
                          { value: '', label: t('config.embedSelectProvider') },
                          ...(config.providers || []).map((p) => ({
                            value: p.id,
                            label: `${p.name} (${p.id})`,
                          })),
                        ]}
                      />
                    </div>
                    <div className="flex flex-col gap-1">
                      <label className="text-xs font-semibold text-muted-foreground">
                        {t('config.embedModelName')}
                      </label>
                      <Input
                        type="text"
                        placeholder={t('config.embedModelNamePlaceholder')}
                        value={mdl.name || ''}
                        onChange={(e) => onUpdateModel(idx, 'name', e.target.value)}
                      />
                    </div>
                    <div className="flex flex-col gap-1">
                      <label className="text-xs font-semibold text-muted-foreground">
                        {t('config.embedDimensionSize')}
                      </label>
                      <Input
                        type="number"
                        value={mdl.dimension || 1024}
                        onChange={(e) => onUpdateModel(idx, 'dimension', Number(e.target.value))}
                      />
                    </div>
                    <div className="flex flex-col gap-1">
                      <label className="text-xs font-semibold text-muted-foreground">
                        {t('config.embedBatchSize')}
                      </label>
                      <Input
                        type="number"
                        value={mdl.batchSize || 32}
                        onChange={(e) => onUpdateModel(idx, 'batchSize', Number(e.target.value))}
                      />
                    </div>
                    <div className="flex items-center gap-6 pt-6 flex-wrap">
                      <div className="flex items-center gap-2">
                        <Switch
                          checked={mdl.normalize}
                          onCheckedChange={(val) => onUpdateModel(idx, 'normalize', val)}
                        />
                        <span className="text-xs font-semibold text-foreground">{t('config.embedNormalize')}</span>
                      </div>
                      <div className="flex items-center gap-2">
                        <Switch
                          checked={mdl.isDefault}
                          onCheckedChange={(val) => onUpdateModel(idx, 'isDefault', val)}
                        />
                        <span className="text-xs font-semibold text-foreground">
                          {t('config.embedIsDefaultModel')}
                        </span>
                      </div>
                      <div className="flex items-center gap-2">
                        <Switch
                          checked={mdl.enabled}
                          onCheckedChange={(val) => onUpdateModel(idx, 'enabled', val)}
                        />
                        <span className="text-xs font-semibold text-foreground">{t('common.enabled')}</span>
                      </div>
                    </div>
                  </div>
                </div>
              ))}
              {(config.models || []).length === 0 && (
                <div className="text-center p-6 border border-dashed rounded-xl text-muted-foreground text-xs">
                  {t('config.embedNoModels')}
                </div>
              )}
            </div>
          </div>
        </>
      )}
    </div>
  )
}
