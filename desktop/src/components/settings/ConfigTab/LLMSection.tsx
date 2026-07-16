import { useState, useEffect } from 'react'
import { Database, Plus, Settings, X, Eye, EyeOff, ChevronDown, Loader2 } from 'lucide-react'
import { useTranslation } from '@/lib/i18n'
import { listProviderRemoteModels } from '@/lib/api/config-api'

function parseHeadersJson(json: string): Record<string, string> {
  try {
    return JSON.parse(json)
  } catch (e) {
    // eslint-disable-next-line preserve-caught-error
    throw new Error('Headers must be valid JSON object: ' + (e as Error).message)
  }
}
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { Switch } from '@/components/ui/switch'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import type { LLMProvider, LLMModel, DefaultModelsConfig } from '@/types'

interface LLMSectionProps {
  providers: LLMProvider[]
  models: LLMModel[]
  defaultModels: DefaultModelsConfig
  onSaveDefaults: () => void
  onDefaultModelsChange: (config: DefaultModelsConfig) => void
  onCreateProvider: (provider: LLMProvider) => Promise<void>
  onUpdateProvider: (id: string, provider: LLMProvider) => Promise<void>
  onDeleteProvider: (id: string) => void
  onToggleProviderStatus: (provider: LLMProvider) => Promise<void>
  onSetProviderAsDefault: (provider: LLMProvider) => Promise<void>
  onCreateModel: (model: LLMModel) => Promise<void>
  onUpdateModel: (id: string, model: LLMModel) => Promise<void>
  onDeleteModel: (id: string) => void
  onToggleModelStatus: (model: LLMModel) => Promise<void>
}

export function LLMSection({
  providers,
  models,
  defaultModels,
  onSaveDefaults,
  onDefaultModelsChange,
  onCreateProvider,
  onUpdateProvider,
  onDeleteProvider,
  onToggleProviderStatus,
  onSetProviderAsDefault,
  onCreateModel,
  onUpdateModel,
  onDeleteModel,
  onToggleModelStatus,
}: LLMSectionProps) {
  const { t } = useTranslation()
  // Provider form state
  const [isAddingProvider, setIsAddingProvider] = useState(false)
  const [editingProvider, setEditingProvider] = useState<LLMProvider | null>(null)
  const [providerForm, setProviderForm] = useState<Partial<LLMProvider>>({})
  const [providerHeadersJson, setProviderHeadersJson] = useState('{}')
  const [showApiKey, setShowApiKey] = useState<Record<string, boolean>>({})

  // Model form state
  const [isAddingModel, setIsAddingModel] = useState(false)
  const [editingModel, setEditingModel] = useState<LLMModel | null>(null)
  const [modelForm, setModelForm] = useState<Partial<LLMModel>>({
    generation: { temperature: 0.7, maxTokens: 4096 },
    thinking: { enabled: false, reasoningEffort: 'medium' },
  })
  const [remoteModels, setRemoteModels] = useState<string[]>([])
  const [isLoadingRemoteModels, setIsLoadingRemoteModels] = useState(false)
  const [remoteModelsError, setRemoteModelsError] = useState<string | null>(null)
  const [isComboboxOpen, setIsComboboxOpen] = useState(false)


  useEffect(() => {
    const providerId = modelForm.providerId
    if (!providerId) {
      setRemoteModels([])
      setRemoteModelsError(null)
      return
    }

    let active = true
    const fetchModels = async () => {
      setIsLoadingRemoteModels(true)
      setRemoteModelsError(null)
      try {
        const data = await listProviderRemoteModels(providerId)
        if (active) {
          setRemoteModels(data || [])
        }
      } catch (err) {
        if (active) {
          setRemoteModelsError((err as Error).message)
        }
      } finally {
        if (active) {
          setIsLoadingRemoteModels(false)
        }
      }
    }

    fetchModels()

    return () => {
      active = false
    }
  }, [modelForm.providerId])

  const startAddProvider = () => {
    setIsAddingProvider(true)
    setEditingProvider(null)
    setProviderForm({
      id: '',
      name: '',
      baseUrl: '',
      apiKey: '',
      apiKeyEnv: '',
      enabled: true,
      isDefault: false,
      timeoutMs: 30000,
      headers: {},
    })
    setProviderHeadersJson('{}')
  }

  const startEditProvider = (p: LLMProvider) => {
    setEditingProvider(p)
    setIsAddingProvider(false)
    setProviderForm({ ...p })
    setProviderHeadersJson(JSON.stringify(p.headers || {}, null, 2))
  }

  const cancelProviderForm = () => {
    setIsAddingProvider(false)
    setEditingProvider(null)
  }

  const saveProviderForm = async () => {
    const headers = parseHeadersJson(providerHeadersJson)

    const payload: LLMProvider = {
      id: providerForm.id || '',
      name: providerForm.name || '',
      baseUrl: providerForm.baseUrl || '',
      apiKey: providerForm.apiKey || '',
      apiKeyEnv: providerForm.apiKeyEnv || '',
      enabled: providerForm.enabled ?? true,
      isDefault: providerForm.isDefault ?? false,
      timeoutMs: Number(providerForm.timeoutMs || 30000),
      retry: { maxRetries: 3, initialDelayMs: 1000, maxDelayMs: 30000, backoffMultiplier: 2.0 },
      headers,
    }

    if (isAddingProvider) {
      await onCreateProvider(payload)
    } else if (editingProvider) {
      await onUpdateProvider(editingProvider.id, payload)
    }

    setIsAddingProvider(false)
    setEditingProvider(null)
  }

  const startAddModel = () => {
    setIsAddingModel(true)
    setEditingModel(null)
    setModelForm({
      id: '',
      providerId: providers[0]?.id || '',
      name: '',
      apiModel: '',
      contextWindow: 128000,
      enabled: true,
      generation: { temperature: 0.7, maxTokens: 4096 },
      thinking: { enabled: false, reasoningEffort: 'medium' },
      vision: false,
    })
  }

  const startEditModel = (m: LLMModel) => {
    setEditingModel(m)
    setIsAddingModel(false)
    setModelForm({ ...m })
  }

  const cancelModelForm = () => {
    setIsAddingModel(false)
    setEditingModel(null)
  }

  const saveModelForm = async () => {
    const payload: LLMModel = {
      id: modelForm.id || '',
      providerId: modelForm.providerId || '',
      name: modelForm.name || '',
      apiModel: modelForm.apiModel || '',
      contextWindow: Number(modelForm.contextWindow || 128000),
      enabled: modelForm.enabled ?? true,
      generation: {
        temperature: Number(modelForm.generation?.temperature ?? 0.7),
        maxTokens: Number(modelForm.generation?.maxTokens ?? 4096),
      },
      thinking: {
        enabled: modelForm.thinking?.enabled ?? false,
        reasoningEffort: modelForm.thinking?.reasoningEffort || 'medium',
        thinkingType: modelForm.thinking?.thinkingType || '',
      },
      vision: !!modelForm.vision,
    }

    if (isAddingModel) {
      await onCreateModel(payload)
    } else if (editingModel) {
      await onUpdateModel(editingModel.id, payload)
    }

    setIsAddingModel(false)
    setEditingModel(null)
  }

  return (
    <div className="space-y-8">
      {/* ─── Default Model Roles ─── */}
      <div className="rounded-xl border bg-card p-5 shadow-sm space-y-4">
        <div className="flex flex-col border-b pb-3">
          <div className="flex items-center gap-2">
            <Settings className="h-4 w-4 text-primary" />
            <h3 className="font-semibold text-foreground">{t('config.llmDefaultMappings')}</h3>
          </div>
          <p className="text-xs text-muted-foreground mt-1 leading-relaxed">
            {t('config.llmDefaultMappingsDesc')}
          </p>
        </div>
        <div className="space-y-4">
          {([
            { role: 'fast', lvl: '—', desc: 'config.llmRoleFastDesc' },
            { role: 'basic', lvl: 'L0', desc: 'config.llmRoleBasicDesc' },
            { role: 'universal', lvl: 'L1', desc: 'config.llmRoleUniversalDesc' },
            { role: 'superior', lvl: 'L2', desc: 'config.llmRoleSuperiorDesc' },
            { role: 'expert', lvl: 'L3', desc: 'config.llmRoleExpertDesc' },
            { role: 'apex', lvl: 'L4', desc: 'config.llmRoleApexDesc' },
          ] as const).map(({ role, lvl, desc }) => {
            const val = defaultModels[role] || ''
            return (
              <div key={role} className="flex flex-col gap-2 sm:flex-row sm:items-center p-3 rounded-lg border bg-muted/30">
                <label
                  htmlFor={`role-select-${role}`}
                  className="text-xs font-bold text-primary uppercase shrink-0 sm:w-28"
                >
                  {t('config.llmRoleTitle', { role, lvl })}
                </label>
                <div className="flex-1 min-w-0">
                  <p className="text-xs text-muted-foreground leading-relaxed">{t(desc)}</p>
                </div>
                <div className="shrink-0 sm:w-56 w-full">
                  <Select
                    id={`role-select-${role}`}
                    value={val}
                    onChange={(v) => onDefaultModelsChange({ ...defaultModels, [role]: v })}
                    placeholder={t('config.llmUnsetInherit')}
                    options={[
                      { value: '', label: t('config.llmUnsetInherit') },
                      ...models
                        .filter((m) => m.enabled)
                        .map((m) => ({
                          value: `${m.providerId}:${m.id}`,
                          label: `${m.providerId}:${m.id} (${m.name})`,
                        })),
                    ]}
                  />
                </div>
              </div>
            )
          })}
          {/* ─── Fallback (required) ─── */}
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center p-3 rounded-lg border-2 border-primary/30 bg-primary/5">
            <label
              htmlFor="role-select-fallback"
              className="text-xs font-bold text-primary uppercase shrink-0 sm:w-28"
            >
              {t('config.llmRoleTitle', { role: 'fallback', lvl: '—' })}
            </label>
            <div className="flex-1 min-w-0">
              <p className="text-xs text-muted-foreground leading-relaxed">{t('config.llmRoleFallbackDesc')}</p>
              <p className="text-xs text-destructive mt-1">{t('config.llmRoleFallbackRequired')}</p>
            </div>
            <div className="shrink-0 sm:w-56 w-full">
              <Select
                id="role-select-fallback"
                value={defaultModels.fallback || ''}
                onChange={(v) => {
                  if (!v) return
                  onDefaultModelsChange({ ...defaultModels, fallback: v })
                }}
                placeholder={t('config.llmSelectModel')}
                options={models
                  .filter((m) => m.enabled)
                  .map((m) => ({
                    value: `${m.providerId}:${m.id}`,
                    label: `${m.providerId}:${m.id} (${m.name})`,
                  }))}
              />
            </div>
          </div>
        </div>
        <div className="flex justify-end pt-2">
          <Button size="sm" onClick={onSaveDefaults}>
            {t('config.llmUpdateDefaults')}
          </Button>
        </div>
      </div>

      {/* ─── LLM Providers ─── */}
      <div className="space-y-4">
        <div className="flex flex-col gap-1">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Database className="h-4 w-4 text-primary" />
              <h3 className="font-semibold text-foreground">{t('config.llmProviders')}</h3>
            </div>
            <Button size="sm" variant="outline" className="h-8 gap-1" onClick={startAddProvider}>
              <Plus className="h-3.5 w-3.5" />
              {t('config.llmAddProvider')}
            </Button>
          </div>
          <p className="text-xs text-muted-foreground leading-relaxed">
            {t('config.llmProvidersDesc')}
          </p>
        </div>

        {/* Provider Form (modal dialog) */}
        <Dialog
          open={isAddingProvider || !!editingProvider}
          onOpenChange={(open) => {
            if (!open) cancelProviderForm()
          }}
        >
          <DialogContent className="max-w-xl max-h-[90vh] overflow-y-auto">
            <DialogHeader>
              <DialogTitle>
                {isAddingProvider ? t('config.llmAddProviderTitle') : t('config.llmEditProviderTitle', { name: editingProvider?.name || '' })}
              </DialogTitle>
              <DialogDescription>
                {isAddingProvider
                  ? t('config.llmAddProviderDesc')
                  : t('config.llmEditProviderDesc')}
              </DialogDescription>
            </DialogHeader>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div className="flex flex-col gap-1">
                <label className="text-xs font-semibold text-muted-foreground">
                  {t('config.llmProviderId')}
                </label>
                <Input
                  value={providerForm.id || ''}
                  disabled={!!editingProvider}
                  placeholder={t('config.llmProviderIdPlaceholder')}
                  onChange={(e) => setProviderForm({ ...providerForm, id: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1">
                <label className="text-xs font-semibold text-muted-foreground">{t('config.llmDisplayName')}</label>
                <Input
                  value={providerForm.name || ''}
                  placeholder={t('config.llmDisplayNamePlaceholder')}
                  onChange={(e) => setProviderForm({ ...providerForm, name: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1 sm:col-span-2">
                <label className="text-xs font-semibold text-muted-foreground">{t('config.llmApiBaseUrl')}</label>
                <Input
                  value={providerForm.baseUrl || ''}
                  placeholder={t('config.llmApiBaseUrlPlaceholder')}
                  onChange={(e) => setProviderForm({ ...providerForm, baseUrl: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1">
                <label className="text-xs font-semibold text-muted-foreground">
                  {t('config.llmApiKeyDirect')}
                </label>
                <Input
                  type="password"
                  value={providerForm.apiKey || ''}
                  placeholder={t('config.llmApiKeyDirectPlaceholder')}
                  onChange={(e) => setProviderForm({ ...providerForm, apiKey: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1">
                <label className="text-xs font-semibold text-muted-foreground">
                  {t('config.llmApiKeyEnv')}
                </label>
                <Input
                  value={providerForm.apiKeyEnv || ''}
                  placeholder={t('config.llmApiKeyEnvPlaceholder')}
                  onChange={(e) => setProviderForm({ ...providerForm, apiKeyEnv: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1">
                <label className="text-xs font-semibold text-muted-foreground">{t('config.llmTimeoutMs')}</label>
                <Input
                  type="number"
                  value={providerForm.timeoutMs || 30000}
                  onChange={(e) =>
                    setProviderForm({ ...providerForm, timeoutMs: Number(e.target.value) })
                  }
                />
              </div>
              <div className="flex items-center gap-4 pt-4">
                <div className="flex items-center gap-2">
                  <Switch
                    checked={providerForm.isDefault || false}
                    onCheckedChange={(val) => setProviderForm({ ...providerForm, isDefault: val })}
                  />
                  <span className="text-xs font-semibold text-foreground">
                    {t('config.llmSetAsDefaultProvider')}
                  </span>
                </div>
                <div className="flex items-center gap-2">
                  <Switch
                    checked={providerForm.enabled ?? true}
                    onCheckedChange={(val) => setProviderForm({ ...providerForm, enabled: val })}
                  />
                  <span className="text-xs font-semibold text-foreground">{t('common.enabled')}</span>
                </div>
              </div>

              {/* Headers JSON */}
              <div className="sm:col-span-2 border-t pt-3 mt-1">
                <h5 className="text-xs font-semibold text-foreground mb-2">{t('config.llmCustomHeaders')}</h5>
                <Textarea
                  value={providerHeadersJson}
                  onChange={(e) => setProviderHeadersJson(e.target.value)}
                  placeholder={t('config.llmCustomHeadersPlaceholder')}
                  className="font-mono text-xs min-h-[80px]"
                  spellCheck={false}
                />
              </div>
            </div>

            <DialogFooter>
              <Button size="sm" onClick={saveProviderForm}>
                {isAddingProvider ? t('config.llmCreateProvider') : t('config.llmUpdateProvider')}
              </Button>
              <Button variant="outline" size="sm" onClick={cancelProviderForm}>
                {t('common.cancel')}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* Provider List */}
        <div className="space-y-2">
          {providers.map((p) => (
            <div
              key={p.id}
              className="flex items-center justify-between p-3 rounded-lg border border-border/80 bg-card/40 hover:bg-muted/20 transition-colors"
            >
              <div className="flex items-center gap-3 min-w-0">
                <div className="flex flex-col min-w-0">
                  <div className="flex items-center gap-2 flex-wrap">
                    <span className="text-sm font-semibold text-foreground truncate">{p.name}</span>
                    <span className="text-[10px] font-mono text-muted-foreground">{p.id}</span>
                    {p.isDefault && (
                      <span className="text-[9px] font-bold uppercase text-primary bg-primary/10 px-1.5 py-0.5 rounded-full">
                        {t('config.llmDefault')}
                      </span>
                    )}
                  </div>
                  <p className="text-[10px] text-muted-foreground/70 truncate font-mono">
                    {p.baseUrl}
                  </p>
                </div>
              </div>
              <div className="flex items-center gap-1.5 shrink-0">
                <button
                  onClick={() => setShowApiKey((prev) => ({ ...prev, [p.id]: !prev[p.id] }))}
                  className="p-1.5 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
                  title={showApiKey[p.id] ? t('config.llmHideApiKey') : t('config.llmShowApiKey')}
                >
                  {showApiKey[p.id] ? (
                    <EyeOff className="h-3.5 w-3.5" />
                  ) : (
                    <Eye className="h-3.5 w-3.5" />
                  )}
                </button>
                {!p.isDefault && (
                  <button
                    onClick={() => onSetProviderAsDefault(p)}
                    className="p-1.5 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors text-[9px] font-bold"
                    title={t('config.llmSetAsDefault')}
                  >
                    ★
                  </button>
                )}
                <Switch checked={p.enabled} onCheckedChange={() => onToggleProviderStatus(p)} />
                <Button size="xs" variant="ghost" onClick={() => startEditProvider(p)}>
                  {t('common.edit')}
                </Button>
                <Button size="xs" variant="ghost" onClick={() => onDeleteProvider(p.id)}>
                  <X className="h-3 w-3" />
                </Button>
              </div>
            </div>
          ))}
          {providers.length === 0 && !isAddingProvider && (
            <div className="text-center p-8 border border-dashed rounded-xl text-sm text-muted-foreground">
              {t('config.llmNoProviders')}
            </div>
          )}
        </div>
      </div>

      {/* ─── LLM Models ─── */}
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Database className="h-4 w-4 text-primary" />
            <h3 className="font-semibold text-foreground">{t('config.llmModels')}</h3>
          </div>
            <Button size="sm" variant="outline" className="h-8 gap-1" onClick={startAddModel}>
              <Plus className="h-3.5 w-3.5" />
              {t('config.llmAddModel')}
            </Button>
        </div>

        {/* Model Form (modal dialog) */}
        <Dialog
          open={isAddingModel || !!editingModel}
          onOpenChange={(open) => {
            if (!open) cancelModelForm()
          }}
        >
          <DialogContent className="max-w-xl max-h-[90vh] overflow-y-auto">
            <DialogHeader>
              <DialogTitle>
                {isAddingModel ? t('config.llmAddModelTitle') : t('config.llmEditModelTitle', { name: editingModel?.name || '' })}
              </DialogTitle>
              <DialogDescription>
                {isAddingModel
                  ? t('config.llmAddModelDesc')
                  : t('config.llmEditModelDesc')}
              </DialogDescription>
            </DialogHeader>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div className="flex flex-col gap-1">
                <label className="text-xs font-semibold text-muted-foreground">{t('config.llmModelId')}</label>
                <Input
                  value={modelForm.id || ''}
                  disabled={!!editingModel}
                  placeholder={t('config.llmModelIdPlaceholder')}
                  onChange={(e) => setModelForm({ ...modelForm, id: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1">
                <label className="text-xs font-semibold text-muted-foreground">{t('config.llmDisplayName')}</label>
                <Input
                  value={modelForm.name || ''}
                  placeholder={t('config.llmDisplayNamePlaceholder')}
                  onChange={(e) => setModelForm({ ...modelForm, name: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1">
                <Select
                  label={t('config.llmProviderLabel')}
                  value={modelForm.providerId || ''}
                  onChange={(v) => setModelForm({ ...modelForm, providerId: v })}
                  placeholder={t('config.llmSelectProvider')}
                  options={[
                    { value: '', label: t('config.llmSelectProvider') },
                    ...providers.map((p) => ({ value: p.id, label: p.name })),
                  ]}
                />
              </div>
              <div className="flex flex-col gap-1 relative">
                <label className="text-xs font-semibold text-muted-foreground">
                  {t('config.llmApiModelName')}
                </label>
                <div className="relative">
                  <Input
                    value={modelForm.apiModel || ''}
                    placeholder={t('config.llmSelectRemoteModel')}
                    onChange={(e) => setModelForm({ ...modelForm, apiModel: e.target.value })}
                    onFocus={() => setIsComboboxOpen(true)}
                    onBlur={() => {
                      // Delay closing to let onMouseDown trigger first
                      setTimeout(() => setIsComboboxOpen(false), 200)
                    }}
                    className="pr-8"
                  />
                  <button
                    type="button"
                    onClick={() => setIsComboboxOpen((prev) => !prev)}
                    className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground focus:outline-none"
                  >
                    {isLoadingRemoteModels ? (
                      <Loader2 className="h-4 w-4 animate-spin text-muted-foreground/60" />
                    ) : (
                      <ChevronDown className="h-4 w-4" />
                    )}
                  </button>
                </div>

                {isComboboxOpen && (
                  <div className="absolute z-50 left-0 right-0 top-full mt-1 max-h-60 overflow-y-auto rounded-md border border-border bg-popover p-1 shadow-lg">
                    {isLoadingRemoteModels && (
                      <div className="flex items-center gap-2 px-2 py-1.5 text-xs text-muted-foreground">
                        <Loader2 className="h-3 w-3 animate-spin" />
                        {t('config.llmLoadingRemoteModels')}
                      </div>
                    )}
                    {remoteModelsError && (
                      <div className="px-2 py-1.5 text-xs text-destructive">
                        {t('config.llmFailedToLoadRemoteModels')}: {remoteModelsError}
                      </div>
                    )}
                    {!isLoadingRemoteModels && !remoteModelsError && (
                      <>
                        {(() => {
                          const query = (modelForm.apiModel || '').toLowerCase()
                          const filtered = remoteModels.filter((model) =>
                            model.toLowerCase().includes(query)
                          )

                          if (filtered.length === 0) {
                            return (
                              <div className="px-2 py-1.5 text-xs text-muted-foreground">
                                {query ? t('common.none') : t('config.llmSelectRemoteModel')}
                              </div>
                            )
                          }

                          return filtered.map((model) => (
                            <button
                              key={model}
                              type="button"
                              onMouseDown={() => {
                                setModelForm({ ...modelForm, apiModel: model })
                                setIsComboboxOpen(false)
                              }}
                              className="w-full text-left flex cursor-default select-none items-center rounded-sm px-2 py-1.5 text-xs outline-none hover:bg-muted focus:bg-muted text-foreground transition-colors"
                            >
                              {model}
                            </button>
                          ))
                        })()}
                      </>
                    )}
                  </div>
                )}
              </div>
              <div className="flex flex-col gap-1">
                <label className="text-xs font-semibold text-muted-foreground">
                  {t('config.llmContextWindow')}
                </label>
                <Input
                  type="number"
                  value={modelForm.contextWindow || 128000}
                  onChange={(e) =>
                    setModelForm({ ...modelForm, contextWindow: Number(e.target.value) })
                  }
                />
              </div>
              <div className="flex items-center gap-4 pt-4">
                <div className="flex items-center gap-2">
                  <Switch
                    checked={modelForm.enabled ?? true}
                    onCheckedChange={(val) => setModelForm({ ...modelForm, enabled: val })}
                  />
                  <span className="text-xs font-semibold text-foreground">{t('common.enabled')}</span>
                </div>
              </div>
              <div className="flex flex-col gap-1">
                <label className="text-xs font-semibold text-muted-foreground">
                  {t('config.llmTemperature', { val: modelForm.generation?.temperature ?? 0.7 })}
                </label>
                <input
                  type="range"
                  min={0}
                  max={2}
                  step={0.05}
                  value={modelForm.generation?.temperature ?? 0.7}
                  onChange={(e) =>
                    setModelForm({
                      ...modelForm,
                      generation: {
                        ...(modelForm.generation || { maxTokens: 4096 }),
                        temperature: Number(e.target.value),
                      },
                    })
                  }
                  className="w-full h-1.5 bg-muted rounded-lg appearance-none cursor-pointer accent-primary"
                />
              </div>
              <div className="flex flex-col gap-1">
                <label className="text-xs font-semibold text-muted-foreground">{t('config.llmMaxTokens')}</label>
                <Input
                  type="number"
                  value={modelForm.generation?.maxTokens ?? 4096}
                  onChange={(e) =>
                    setModelForm({
                      ...modelForm,
                      generation: {
                        ...(modelForm.generation || { temperature: 0.7 }),
                        maxTokens: Number(e.target.value),
                      },
                    })
                  }
                />
              </div>

              {/* Thinking config */}
              <div className="sm:col-span-2 border-t pt-3 mt-1">
                <h5 className="text-xs font-semibold text-foreground mb-2">{t('config.llmThinkingReasoning')}</h5>
                <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                  <div className="flex items-center gap-2">
                    <Switch
                      checked={modelForm.thinking?.enabled || false}
                      onCheckedChange={(val) =>
                        setModelForm({
                          ...modelForm,
                          thinking: {
                            ...(modelForm.thinking || { reasoningEffort: 'medium' }),
                            enabled: val,
                          },
                        })
                      }
                    />
                    <span className="text-xs font-semibold text-foreground">{t('config.llmEnableThinking')}</span>
                  </div>
                  <div className="flex flex-col gap-1">
                    <Select
                      label={t('config.llmReasoningEffort')}
                      value={modelForm.thinking?.reasoningEffort || 'medium'}
                      onChange={(v) =>
                        setModelForm({
                          ...modelForm,
                          thinking: {
                            ...(modelForm.thinking || { enabled: false }),
                            reasoningEffort: v,
                          },
                        })
                      }
                      options={[
                        { value: '', label: t('config.llmReasoningNone') },
                        { value: 'low', label: t('config.llmReasoningLow') },
                        { value: 'medium', label: t('config.llmReasoningMedium') },
                        { value: 'high', label: t('config.llmReasoningHigh') },
                        { value: 'xhigh', label: t('config.llmReasoningXHigh') },
                        { value: 'max', label: t('config.llmReasoningMax') },
                      ]}
                    />
                  </div>
                  <div className="flex flex-col gap-1">
                    <label className="text-[10px] font-semibold text-muted-foreground">
                      {t('config.llmThinkingType')}
                    </label>
                    <Input
                      value={modelForm.thinking?.thinkingType || ''}
                      onChange={(e) =>
                        setModelForm({
                          ...modelForm,
                          thinking: {
                            ...(modelForm.thinking || { enabled: false, reasoningEffort: 'medium' }),
                            thinkingType: e.target.value,
                          },
                        })
                      }
                      placeholder={t('config.llmThinkingTypePlaceholder')}
                    />
                  </div>
                </div>
              </div>

              {/* Vision config */}
              <div className="sm:col-span-2 border-t pt-3 mt-1">
                <h5 className="text-xs font-semibold text-foreground mb-2">{t('config.llmVisionMultimodal')}</h5>
                <div className="flex items-center gap-2">
                  <Switch
                    checked={modelForm.vision || false}
                    onCheckedChange={(val) =>
                      setModelForm({
                        ...modelForm,
                        vision: val,
                      })
                    }
                  />
                  <span className="text-xs font-semibold text-foreground">{t('config.llmEnableVision')}</span>
                </div>
                <p className="text-[10px] text-muted-foreground mt-1">
                  {t('config.llmVisionDesc')}
                </p>
              </div>
            </div>
            <DialogFooter>
              <Button size="sm" onClick={saveModelForm}>
                {isAddingModel ? t('config.llmCreateModel') : t('config.llmUpdateModel')}
              </Button>
              <Button variant="outline" size="sm" onClick={cancelModelForm}>
                {t('common.cancel')}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* Model List */}
        <div className="space-y-2">
          {models.map((m) => (
            <div
              key={m.id}
              className="flex items-center justify-between p-3 rounded-lg border border-border/80 bg-card/40 hover:bg-muted/20 transition-colors"
            >
              <div className="flex items-center gap-3 min-w-0">
                <div className="flex flex-col min-w-0">
                  <div className="flex items-center gap-2 flex-wrap">
                    <span className="text-sm font-semibold text-foreground truncate">{m.name}</span>
                    <span className="text-[10px] font-mono text-muted-foreground">{m.id}</span>
                    <span className="text-[10px] font-mono text-muted-foreground/50">
                      {m.providerId}
                    </span>
                    {m.thinking?.enabled && (
                      <span className="text-[9px] bg-purple-500/15 text-purple-600 dark:text-purple-400 border border-purple-500/20 px-1 py-0.5 rounded font-medium">
                        {t('config.llmThinkingBadge')}
                      </span>
                    )}
                    {m.vision && (
                      <span className="text-[9px] bg-sky-500/15 text-sky-600 dark:text-sky-400 border border-sky-500/20 px-1 py-0.5 rounded font-medium">
                        {t('config.llmVisionBadge')}
                      </span>
                    )}
                  </div>
                  <p className="text-[10px] text-muted-foreground/70 truncate font-mono">
                    {m.apiModel} · {t('config.llmContextLabel', { count: m.contextWindow?.toLocaleString() || 0 })}
                  </p>
                </div>
              </div>
              <div className="flex items-center gap-1.5 shrink-0">
                <Switch checked={m.enabled} onCheckedChange={() => onToggleModelStatus(m)} />
                <Button size="xs" variant="ghost" onClick={() => startEditModel(m)}>
                  {t('common.edit')}
                </Button>
                <Button size="xs" variant="ghost" onClick={() => onDeleteModel(m.id)}>
                  <X className="h-3 w-3" />
                </Button>
              </div>
            </div>
          ))}
          {models.length === 0 && !isAddingModel && (
            <div className="text-center p-8 border border-dashed rounded-xl text-sm text-muted-foreground">
              {t('config.llmNoModels')}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
