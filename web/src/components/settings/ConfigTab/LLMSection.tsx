import { useState } from 'react'
import { Database, Plus, Settings, X, Eye, EyeOff } from 'lucide-react'

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
      retry: { maxRetries: 3, initialDelayMs: 1000, maxDelayMs: 5000, backoffMultiplier: 2.0 },
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
      retry: providerForm.retry || {
        maxRetries: 3,
        initialDelayMs: 1000,
        maxDelayMs: 5000,
        backoffMultiplier: 2.0,
      },
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
      },
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
            <h3 className="font-semibold text-foreground">Default Model Mappings</h3>
          </div>
          <p className="text-xs text-muted-foreground mt-1 leading-relaxed">
            Configure default models used by agents for specific roles like "expert", "superior",
            "universal", "fast", or "fallback".
          </p>
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          {(['expert', 'superior', 'universal', 'fast', 'fallback'] as const).map((role) => {
            const val = defaultModels[role] || ''
            return (
              <div key={role} className="flex flex-col gap-1.5">
                <label className="text-xs font-semibold capitalize text-muted-foreground">
                  {role} Model
                </label>
                <Select
                  value={val}
                  onChange={(v) => onDefaultModelsChange({ ...defaultModels, [role]: v })}
                  placeholder="-- Unset / Inherit --"
                  options={[
                    { value: '', label: '-- Unset / Inherit --' },
                    ...models
                      .filter((m) => m.enabled)
                      .map((m) => ({
                        value: `${m.providerId}:${m.id}`,
                        label: `${m.providerId}:${m.id} (${m.name})`,
                      })),
                  ]}
                />
              </div>
            )
          })}
        </div>
        <div className="flex justify-end pt-2">
          <Button size="sm" onClick={onSaveDefaults}>
            Update Defaults
          </Button>
        </div>
      </div>

      {/* ─── LLM Providers ─── */}
      <div className="space-y-4">
        <div className="flex flex-col gap-1">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Database className="h-4 w-4 text-primary" />
              <h3 className="font-semibold text-foreground">LLM Providers</h3>
            </div>
            {!isAddingProvider && !editingProvider && (
              <Button size="sm" variant="outline" className="h-8 gap-1" onClick={startAddProvider}>
                <Plus className="h-3.5 w-3.5" />
                Add Provider
              </Button>
            )}
          </div>
          <p className="text-xs text-muted-foreground leading-relaxed">
            LLM Providers: Manage API connection settings for Large Language Model providers.
          </p>
        </div>

        {/* Provider Form (inline) */}
        {(isAddingProvider || editingProvider) && (
          <div className="rounded-xl border border-primary/20 bg-primary/5 p-5 space-y-4 shadow-sm animate-in fade-in duration-200">
            <div className="flex items-center justify-between border-b pb-2">
              <h4 className="font-semibold text-foreground">
                {isAddingProvider ? 'Add LLM Provider' : `Edit Provider: ${editingProvider?.name}`}
              </h4>
              <button
                onClick={cancelProviderForm}
                className="text-muted-foreground hover:text-foreground"
              >
                <X className="h-4 w-4" />
              </button>
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div className="flex flex-col gap-1">
                <label className="text-xs font-semibold text-muted-foreground">
                  ID (unique slug)
                </label>
                <Input
                  value={providerForm.id || ''}
                  disabled={!!editingProvider}
                  placeholder="e.g. deepseek"
                  onChange={(e) => setProviderForm({ ...providerForm, id: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1">
                <label className="text-xs font-semibold text-muted-foreground">Display Name</label>
                <Input
                  value={providerForm.name || ''}
                  placeholder="e.g. DeepSeek Official"
                  onChange={(e) => setProviderForm({ ...providerForm, name: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1 sm:col-span-2">
                <label className="text-xs font-semibold text-muted-foreground">API Base URL</label>
                <Input
                  value={providerForm.baseUrl || ''}
                  placeholder="https://api.deepseek.com/v1"
                  onChange={(e) => setProviderForm({ ...providerForm, baseUrl: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1">
                <label className="text-xs font-semibold text-muted-foreground">
                  API Key (Direct)
                </label>
                <Input
                  type="password"
                  value={providerForm.apiKey || ''}
                  placeholder="sk-..."
                  onChange={(e) => setProviderForm({ ...providerForm, apiKey: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1">
                <label className="text-xs font-semibold text-muted-foreground">
                  API Key Env Variable
                </label>
                <Input
                  value={providerForm.apiKeyEnv || ''}
                  placeholder="DEEPSEEK_API_KEY"
                  onChange={(e) => setProviderForm({ ...providerForm, apiKeyEnv: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1">
                <label className="text-xs font-semibold text-muted-foreground">Timeout (ms)</label>
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
                    Set as Default Provider
                  </span>
                </div>
                <div className="flex items-center gap-2">
                  <Switch
                    checked={providerForm.enabled ?? true}
                    onCheckedChange={(val) => setProviderForm({ ...providerForm, enabled: val })}
                  />
                  <span className="text-xs font-semibold text-foreground">Enabled</span>
                </div>
              </div>

              {/* Retry parameters block */}
              <div className="sm:col-span-2 border-t pt-3 mt-1">
                <h5 className="text-xs font-semibold text-foreground mb-2">Retry Configurations</h5>
                <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
                  <div className="flex flex-col gap-1">
                    <label className="text-[10px] font-semibold text-muted-foreground">
                      Max Retries
                    </label>
                    <Input
                      type="number"
                      value={providerForm.retry?.maxRetries ?? 3}
                      onChange={(e) =>
                        setProviderForm({
                          ...providerForm,
                          retry: {
                            ...(providerForm.retry || {
                              initialDelayMs: 1000,
                              maxDelayMs: 5000,
                              backoffMultiplier: 2,
                            }),
                            maxRetries: Number(e.target.value),
                          },
                        })
                      }
                    />
                  </div>
                  <div className="flex flex-col gap-1">
                    <label className="text-[10px] font-semibold text-muted-foreground">
                      Init Delay (ms)
                    </label>
                    <Input
                      type="number"
                      value={providerForm.retry?.initialDelayMs ?? 1000}
                      onChange={(e) =>
                        setProviderForm({
                          ...providerForm,
                          retry: {
                            ...(providerForm.retry || {
                              maxRetries: 3,
                              maxDelayMs: 5000,
                              backoffMultiplier: 2,
                            }),
                            initialDelayMs: Number(e.target.value),
                          },
                        })
                      }
                    />
                  </div>
                  <div className="flex flex-col gap-1">
                    <label className="text-[10px] font-semibold text-muted-foreground">
                      Max Delay (ms)
                    </label>
                    <Input
                      type="number"
                      value={providerForm.retry?.maxDelayMs ?? 5000}
                      onChange={(e) =>
                        setProviderForm({
                          ...providerForm,
                          retry: {
                            ...(providerForm.retry || {
                              maxRetries: 3,
                              initialDelayMs: 1000,
                              backoffMultiplier: 2,
                            }),
                            maxDelayMs: Number(e.target.value),
                          },
                        })
                      }
                    />
                  </div>
                  <div className="flex flex-col gap-1">
                    <label className="text-[10px] font-semibold text-muted-foreground">
                      Backoff Multiplier
                    </label>
                    <Input
                      type="number"
                      step="0.1"
                      value={providerForm.retry?.backoffMultiplier ?? 2}
                      onChange={(e) =>
                        setProviderForm({
                          ...providerForm,
                          retry: {
                            ...(providerForm.retry || {
                              maxRetries: 3,
                              initialDelayMs: 1000,
                              maxDelayMs: 5000,
                            }),
                            backoffMultiplier: Number(e.target.value),
                          },
                        })
                      }
                    />
                  </div>
                </div>
              </div>

              {/* Headers JSON */}
              <div className="sm:col-span-2 border-t pt-3 mt-1">
                <h5 className="text-xs font-semibold text-foreground mb-2">
                  Custom Headers (JSON)
                </h5>
                <Textarea
                  value={providerHeadersJson}
                  onChange={(e) => setProviderHeadersJson(e.target.value)}
                  placeholder='{"X-Custom-Header": "value"}'
                  className="font-mono text-xs min-h-[80px]"
                  spellCheck={false}
                />
              </div>
            </div>

            <div className="flex justify-end gap-2 pt-2 border-t">
              <Button variant="outline" size="sm" onClick={cancelProviderForm}>
                Cancel
              </Button>
              <Button size="sm" onClick={saveProviderForm}>
                {isAddingProvider ? 'Create Provider' : 'Update Provider'}
              </Button>
            </div>
          </div>
        )}

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
                        Default
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
                  title={showApiKey[p.id] ? 'Hide API Key' : 'Show API Key'}
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
                    title="Set as Default"
                  >
                    ★
                  </button>
                )}
                <Switch checked={p.enabled} onCheckedChange={() => onToggleProviderStatus(p)} />
                <Button size="xs" variant="ghost" onClick={() => startEditProvider(p)}>
                  Edit
                </Button>
                <Button size="xs" variant="ghost" onClick={() => onDeleteProvider(p.id)}>
                  <X className="h-3 w-3" />
                </Button>
              </div>
            </div>
          ))}
          {providers.length === 0 && !isAddingProvider && (
            <div className="text-center p-8 border border-dashed rounded-xl text-sm text-muted-foreground">
              No LLM providers configured. Click "Add Provider" to get started.
            </div>
          )}
        </div>
      </div>

      {/* ─── LLM Models ─── */}
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Database className="h-4 w-4 text-primary" />
            <h3 className="font-semibold text-foreground">LLM Models</h3>
          </div>
          {!isAddingModel && !editingModel && (
            <Button size="sm" variant="outline" className="h-8 gap-1" onClick={startAddModel}>
              <Plus className="h-3.5 w-3.5" />
              Add Model
            </Button>
          )}
        </div>

        {/* Model Form (inline) */}
        {(isAddingModel || editingModel) && (
          <div className="rounded-xl border border-primary/20 bg-primary/5 p-5 space-y-4 shadow-sm animate-in fade-in duration-200">
            <div className="flex items-center justify-between border-b pb-2">
              <h4 className="font-semibold text-foreground">
                {isAddingModel ? 'Add LLM Model' : `Edit Model: ${editingModel?.name}`}
              </h4>
              <button
                onClick={cancelModelForm}
                className="text-muted-foreground hover:text-foreground"
              >
                <X className="h-4 w-4" />
              </button>
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div className="flex flex-col gap-1">
                <label className="text-xs font-semibold text-muted-foreground">Model ID</label>
                <Input
                  value={modelForm.id || ''}
                  disabled={!!editingModel}
                  placeholder="e.g. deepseek-chat"
                  onChange={(e) => setModelForm({ ...modelForm, id: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1">
                <label className="text-xs font-semibold text-muted-foreground">Display Name</label>
                <Input
                  value={modelForm.name || ''}
                  placeholder="e.g. DeepSeek Chat"
                  onChange={(e) => setModelForm({ ...modelForm, name: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1">
                <Select
                  label="Provider"
                  value={modelForm.providerId || ''}
                  onChange={(v) => setModelForm({ ...modelForm, providerId: v })}
                  placeholder="Select a provider"
                  options={[
                    { value: '', label: 'Select a provider' },
                    ...providers.map((p) => ({ value: p.id, label: p.name })),
                  ]}
                />
              </div>
              <div className="flex flex-col gap-1">
                <label className="text-xs font-semibold text-muted-foreground">
                  API Model Name
                </label>
                <Input
                  value={modelForm.apiModel || ''}
                  placeholder="e.g. deepseek-chat"
                  onChange={(e) => setModelForm({ ...modelForm, apiModel: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1">
                <label className="text-xs font-semibold text-muted-foreground">
                  Context Window
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
                  <span className="text-xs font-semibold text-foreground">Enabled</span>
                </div>
              </div>
              <div className="flex flex-col gap-1">
                <label className="text-xs font-semibold text-muted-foreground">
                  Temperature ({modelForm.generation?.temperature ?? 0.7})
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
                <label className="text-xs font-semibold text-muted-foreground">Max Tokens</label>
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
                <h5 className="text-xs font-semibold text-foreground mb-2">Thinking / Reasoning</h5>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
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
                    <span className="text-xs font-semibold text-foreground">Enable Thinking</span>
                  </div>
                  <div className="flex flex-col gap-1">
                    <Select
                      label="Reasoning Effort"
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
                        { value: 'low', label: 'Low' },
                        { value: 'medium', label: 'Medium' },
                        { value: 'high', label: 'High' },
                      ]}
                    />
                  </div>
                </div>
              </div>
            </div>
            <div className="flex justify-end gap-2 pt-2 border-t">
              <Button variant="outline" size="sm" onClick={cancelModelForm}>
                Cancel
              </Button>
              <Button size="sm" onClick={saveModelForm}>
                {isAddingModel ? 'Create Model' : 'Update Model'}
              </Button>
            </div>
          </div>
        )}

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
                  </div>
                  <p className="text-[10px] text-muted-foreground/70 truncate font-mono">
                    {m.apiModel} · context: {m.contextWindow?.toLocaleString()}
                  </p>
                </div>
              </div>
              <div className="flex items-center gap-1.5 shrink-0">
                <Switch checked={m.enabled} onCheckedChange={() => onToggleModelStatus(m)} />
                <Button size="xs" variant="ghost" onClick={() => startEditModel(m)}>
                  Edit
                </Button>
                <Button size="xs" variant="ghost" onClick={() => onDeleteModel(m.id)}>
                  <X className="h-3 w-3" />
                </Button>
              </div>
            </div>
          ))}
          {models.length === 0 && !isAddingModel && (
            <div className="text-center p-8 border border-dashed rounded-xl text-sm text-muted-foreground">
              No LLM models configured. Click "Add Model" to get started.
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
