import { useState, useEffect } from 'react'
import { PrismLight as SyntaxHighlighter } from 'react-syntax-highlighter'
import { oneLight } from 'react-syntax-highlighter/dist/esm/styles/prism'
import toml from 'react-syntax-highlighter/dist/esm/languages/prism/toml'
import {
  getConfigToml,
  listProviders,
  createProvider,
  updateProvider,
  deleteProvider,
  listModels,
  createModel,
  updateModel,
  deleteModel,
  getDefaultModels,
  updateDefaultModels,
} from '@/lib/api'
import type { LLMProvider, LLMModel, DefaultModelsConfig } from '@/types'
import {
  FileText,
  Database,
  Plus,
  Trash2,
  Edit,
  Check,
  X,
  Settings,
  HelpCircle,
  AlertTriangle,
  Eye,
  EyeOff,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Badge } from '@/components/ui/badge'

SyntaxHighlighter.registerLanguage('toml', toml)

type TabType = 'db' | 'toml'

export function ConfigTab() {
  const [activeTab, setActiveTab] = useState<TabType>('db')
  const [tomlContent, setTomlContent] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [successMsg, setSuccessMsg] = useState<string | null>(null)

  // DB settings state
  const [providers, setProviders] = useState<LLMProvider[]>([])
  const [models, setModels] = useState<LLMModel[]>([])
  const [defaultModels, setDefaultModels] = useState<DefaultModelsConfig>({
    expert: '',
    superior: '',
    universal: '',
    fast: '',
    fallback: '',
  })

  // Form states
  const [editingProvider, setEditingProvider] = useState<LLMProvider | null>(null)
  const [isAddingProvider, setIsAddingProvider] = useState(false)
  const [providerForm, setProviderForm] = useState<Partial<LLMProvider>>({})
  const [providerHeadersJson, setProviderHeadersJson] = useState<string>('{}')
  const [showApiKey, setShowApiKey] = useState<Record<string, boolean>>({})

  const [editingModel, setEditingModel] = useState<LLMModel | null>(null)
  const [isAddingModel, setIsAddingModel] = useState(false)
  const [modelForm, setModelForm] = useState<Partial<LLMModel>>({
    generation: { temperature: 0.7, maxTokens: 4096 },
    thinking: { enabled: false, reasoningEffort: 'medium' },
  })

  useEffect(() => {
    loadAll()
  }, [])

  const loadAll = async () => {
    setLoading(true)
    setError(null)
    try {
      // Load TOML fallback
      const rawToml = await getConfigToml()
      setTomlContent(rawToml)

      // Load DB configurations
      const dbProviders = await listProviders()
      const dbModels = await listModels()
      const dbDefaults = await getDefaultModels()

      setProviders(dbProviders || [])
      setModels(dbModels || [])
      setDefaultModels(
        dbDefaults || {
          expert: '',
          superior: '',
          universal: '',
          fast: '',
          fallback: '',
        }
      )
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  const handleSaveDefaults = async () => {
    setError(null)
    setSuccessMsg(null)
    try {
      await updateDefaultModels(defaultModels)
      setSuccessMsg('Default models updated successfully!')
      setTimeout(() => setSuccessMsg(null), 3000)
      loadAll()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  // ─── Provider Actions ──────────────────────────────────────────────────────

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

  const saveProviderForm = async () => {
    setError(null)
    setSuccessMsg(null)
    try {
      // Validate headers JSON
      let headers = {}
      try {
        headers = JSON.parse(providerHeadersJson)
      } catch (e) {
        // eslint-disable-next-line preserve-caught-error
        throw new Error('Headers must be valid JSON object: ' + (e as Error).message)
      }

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
        headers: headers,
      }

      if (isAddingProvider) {
        await createProvider(payload)
        setSuccessMsg(`Provider ${payload.name} created successfully!`)
      } else if (editingProvider) {
        await updateProvider(editingProvider.id, payload)
        setSuccessMsg(`Provider ${payload.name} updated successfully!`)
      }

      setIsAddingProvider(false)
      setEditingProvider(null)
      loadAll()
      setTimeout(() => setSuccessMsg(null), 3000)
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const toggleProviderStatus = async (p: LLMProvider) => {
    setError(null)
    try {
      const updated = { ...p, enabled: !p.enabled }
      await updateProvider(p.id, updated)
      loadAll()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const setProviderAsDefault = async (p: LLMProvider) => {
    setError(null)
    try {
      const updated = { ...p, isDefault: true }
      await updateProvider(p.id, updated)
      loadAll()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const handleDeleteProvider = async (id: string) => {
    if (
      !confirm(
        'Are you sure you want to delete this provider? All associated models will be deleted.'
      )
    )
      return
    setError(null)
    try {
      await deleteProvider(id)
      loadAll()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  // ─── Model Actions ─────────────────────────────────────────────────────────

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

  const saveModelForm = async () => {
    setError(null)
    setSuccessMsg(null)
    try {
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
        await createModel(payload)
        setSuccessMsg(`Model ${payload.name} created successfully!`)
      } else if (editingModel) {
        await updateModel(editingModel.id, payload)
        setSuccessMsg(`Model ${payload.name} updated successfully!`)
      }

      setIsAddingModel(false)
      setEditingModel(null)
      loadAll()
      setTimeout(() => setSuccessMsg(null), 3000)
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const toggleModelStatus = async (m: LLMModel) => {
    setError(null)
    try {
      const updated = { ...m, enabled: !m.enabled }
      await updateModel(m.id, updated)
      loadAll()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const handleDeleteModel = async (id: string) => {
    if (!confirm('Are you sure you want to delete this model?')) return
    setError(null)
    try {
      await deleteModel(id)
      loadAll()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  // ─── Renderers ─────────────────────────────────────────────────────────────

  if (loading) {
    return (
      <div className="text-sm font-mono text-muted-foreground p-6">
        Loading LLM configurations...
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Tab Switcher */}
      <div className="flex border-b border-border">
        <button
          type="button"
          onClick={() => setActiveTab('db')}
          className={`flex items-center gap-2 px-5 py-3 text-sm font-semibold border-b-2 transition-all whitespace-nowrap ${
            activeTab === 'db'
              ? 'border-primary text-primary'
              : 'border-transparent text-muted-foreground hover:text-foreground'
          }`}
        >
          <Database className="h-4 w-4" />
          Database Settings
        </button>
        <button
          type="button"
          onClick={() => setActiveTab('toml')}
          className={`flex items-center gap-2 px-5 py-3 text-sm font-semibold border-b-2 transition-all whitespace-nowrap ${
            activeTab === 'toml'
              ? 'border-primary text-primary'
              : 'border-transparent text-muted-foreground hover:text-foreground'
          }`}
        >
          <FileText className="h-4 w-4" />
          settings.toml File (ReadOnly)
        </button>
      </div>

      {/* Success / Error Alerts */}
      {error && (
        <div className="flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive">
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
          <div>{error}</div>
        </div>
      )}
      {successMsg && (
        <div className="flex items-start gap-2 rounded-md border border-emerald-500/30 bg-emerald-500/5 p-4 text-sm text-emerald-600">
          <Check className="mt-0.5 h-4 w-4 shrink-0" />
          <div>{successMsg}</div>
        </div>
      )}

      {activeTab === 'toml' ? (
        <div className="space-y-4">
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <FileText className="h-3.5 w-3.5" />
            <span>~/.soloqueue/settings.toml</span>
          </div>
          {tomlContent ? (
            <div className="overflow-hidden rounded-lg border bg-card">
              <SyntaxHighlighter
                language="toml"
                style={oneLight}
                customStyle={{
                  margin: 0,
                  borderRadius: 0,
                  fontSize: '13px',
                  lineHeight: '1.6',
                  background: 'transparent',
                }}
                showLineNumbers
              >
                {tomlContent}
              </SyntaxHighlighter>
            </div>
          ) : (
            <div className="text-sm text-muted-foreground">No content loaded.</div>
          )}
        </div>
      ) : (
        <div className="space-y-8 pb-10">
          {/* ─── Default Model Roles ─── */}
          <div className="rounded-xl border bg-card p-5 shadow-sm space-y-4">
            <div className="flex items-center gap-2 border-b pb-3">
              <Settings className="h-4 w-4 text-primary" />
              <h3 className="font-semibold text-foreground">Default Model Mappings</h3>
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              {['expert', 'superior', 'universal', 'fast', 'fallback'].map((role) => {
                const val = defaultModels[role as keyof DefaultModelsConfig] || ''
                return (
                  <div key={role} className="flex flex-col gap-1.5">
                    <label className="text-xs font-semibold capitalize text-muted-foreground">
                      {role} Model
                    </label>
                    <select
                      value={val}
                      onChange={(e) =>
                        setDefaultModels({ ...defaultModels, [role]: e.target.value })
                      }
                      className="flex h-9 w-full rounded-md border bg-card px-3 py-1.5 text-sm text-foreground transition-colors outline-none focus-visible:ring-1 focus-visible:ring-primary"
                    >
                      <option value="">-- Unset / Inherit --</option>
                      {models
                        .filter((m) => m.enabled)
                        .map((m) => (
                          <option key={m.id} value={`${m.providerId}:${m.id}`}>
                            {m.providerId}:{m.id} ({m.name})
                          </option>
                        ))}
                    </select>
                  </div>
                )
              })}
            </div>
            <div className="flex justify-end pt-2">
              <Button size="sm" onClick={handleSaveDefaults}>
                Update Defaults
              </Button>
            </div>
          </div>

          {/* ─── LLM Providers ─── */}
          <div className="space-y-4">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <Database className="h-4 w-4 text-primary" />
                <h3 className="font-semibold text-foreground">LLM Providers</h3>
              </div>
              {!isAddingProvider && !editingProvider && (
                <Button
                  size="sm"
                  variant="outline"
                  className="h-8 gap-1"
                  onClick={startAddProvider}
                >
                  <Plus className="h-3.5 w-3.5" />
                  Add Provider
                </Button>
              )}
            </div>

            {/* Provider Form (inline) */}
            {(isAddingProvider || editingProvider) && (
              <div className="rounded-xl border border-primary/20 bg-primary/5 p-5 space-y-4 shadow-sm animate-in fade-in duration-200">
                <div className="flex items-center justify-between border-b pb-2">
                  <h4 className="font-semibold text-foreground">
                    {isAddingProvider
                      ? 'Add LLM Provider'
                      : `Edit Provider: ${editingProvider?.name}`}
                  </h4>
                  <button
                    onClick={() => {
                      setIsAddingProvider(false)
                      setEditingProvider(null)
                    }}
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
                    <label className="text-xs font-semibold text-muted-foreground">
                      Display Name
                    </label>
                    <Input
                      value={providerForm.name || ''}
                      placeholder="e.g. DeepSeek Official"
                      onChange={(e) => setProviderForm({ ...providerForm, name: e.target.value })}
                    />
                  </div>
                  <div className="flex flex-col gap-1 sm:col-span-2">
                    <label className="text-xs font-semibold text-muted-foreground">
                      API Base URL
                    </label>
                    <Input
                      value={providerForm.baseUrl || ''}
                      placeholder="https://api.deepseek.com/v1"
                      onChange={(e) =>
                        setProviderForm({ ...providerForm, baseUrl: e.target.value })
                      }
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
                      onChange={(e) =>
                        setProviderForm({ ...providerForm, apiKeyEnv: e.target.value })
                      }
                    />
                  </div>
                  <div className="flex flex-col gap-1">
                    <label className="text-xs font-semibold text-muted-foreground">
                      Timeout (ms)
                    </label>
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
                        onCheckedChange={(val) =>
                          setProviderForm({ ...providerForm, isDefault: val })
                        }
                      />
                      <span className="text-xs font-semibold text-foreground">
                        Set as Default Provider
                      </span>
                    </div>
                    <div className="flex items-center gap-2">
                      <Switch
                        checked={providerForm.enabled ?? true}
                        onCheckedChange={(val) =>
                          setProviderForm({ ...providerForm, enabled: val })
                        }
                      />
                      <span className="text-xs font-semibold text-foreground">Enabled</span>
                    </div>
                  </div>

                  {/* Retry parameters block */}
                  <div className="sm:col-span-2 border-t pt-3 mt-1">
                    <h5 className="text-xs font-semibold text-foreground mb-2">
                      Retry Configurations
                    </h5>
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
                          Multiplier
                        </label>
                        <Input
                          type="number"
                          step="0.1"
                          value={providerForm.retry?.backoffMultiplier ?? 2.0}
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

                  {/* Custom headers block */}
                  <div className="sm:col-span-2 flex flex-col gap-1 border-t pt-3 mt-1">
                    <label className="text-xs font-semibold text-muted-foreground flex items-center gap-1">
                      Custom HTTP Headers (JSON Object)
                      <span title="Enter valid JSON: e.g. { 'X-My-Header': 'value' }">
                        <HelpCircle className="h-3 w-3 text-muted-foreground" />
                      </span>
                    </label>
                    <textarea
                      value={providerHeadersJson}
                      onChange={(e) => setProviderHeadersJson(e.target.value)}
                      rows={3}
                      className="flex w-full rounded-md border bg-card px-3 py-1.5 text-sm font-mono text-foreground outline-none focus-visible:ring-1 focus-visible:ring-primary"
                      placeholder="{}"
                    />
                  </div>
                </div>
                <div className="flex justify-end gap-2 border-t pt-3">
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => {
                      setIsAddingProvider(false)
                      setEditingProvider(null)
                    }}
                  >
                    Cancel
                  </Button>
                  <Button size="sm" onClick={saveProviderForm}>
                    Save Provider
                  </Button>
                </div>
              </div>
            )}

            {/* Providers List */}
            <div className="grid grid-cols-1 gap-3">
              {providers.map((p) => {
                return (
                  <div
                    key={p.id}
                    className={`flex flex-col sm:flex-row sm:items-center justify-between p-4 rounded-xl border bg-card hover:shadow-sm transition-all duration-200 ${
                      p.enabled ? 'border-border' : 'border-border/50 opacity-60'
                    }`}
                  >
                    <div className="space-y-1.5 flex-1 min-w-0">
                      <div className="flex items-center gap-2 flex-wrap">
                        <span className="font-semibold text-foreground">{p.name}</span>
                        <span className="text-xs font-mono text-muted-foreground">({p.id})</span>
                        {p.isDefault && <Badge variant="secondary">Default</Badge>}
                        {p.enabled ? (
                          <span className="flex h-2 w-2 rounded-full bg-emerald-500" />
                        ) : (
                          <span className="flex h-2 w-2 rounded-full bg-muted" />
                        )}
                      </div>
                      <div className="text-xs text-muted-foreground font-mono truncate">
                        {p.baseUrl}
                      </div>
                      <div className="flex items-center gap-4 text-[10px] text-muted-foreground font-mono">
                        <div>Timeout: {p.timeoutMs}ms</div>
                        {p.apiKeyEnv && <div>Env: {p.apiKeyEnv}</div>}
                        {p.apiKey && (
                          <div className="flex items-center gap-1">
                            <span>Key:</span>
                            <span className="font-sans">
                              {showApiKey[p.id] ? p.apiKey : '••••••••'}
                            </span>
                            <button
                              onClick={() =>
                                setShowApiKey({ ...showApiKey, [p.id]: !showApiKey[p.id] })
                              }
                              className="text-muted-foreground hover:text-foreground"
                            >
                              {showApiKey[p.id] ? (
                                <EyeOff className="h-3 w-3" />
                              ) : (
                                <Eye className="h-3 w-3" />
                              )}
                            </button>
                          </div>
                        )}
                      </div>
                    </div>

                    <div className="flex items-center gap-2 mt-3 sm:mt-0 justify-end">
                      <Switch
                        checked={p.enabled}
                        onCheckedChange={() => toggleProviderStatus(p)}
                        title="Toggle Status"
                      />
                      {!p.isDefault && p.enabled && (
                        <Button size="xs" variant="outline" onClick={() => setProviderAsDefault(p)}>
                          Set Default
                        </Button>
                      )}
                      <Button
                        size="icon-xs"
                        variant="ghost"
                        onClick={() => startEditProvider(p)}
                        title="Edit"
                      >
                        <Edit className="h-3.5 w-3.5" />
                      </Button>
                      <Button
                        size="icon-xs"
                        variant="ghost"
                        className="hover:text-destructive hover:bg-destructive/5"
                        onClick={() => handleDeleteProvider(p.id)}
                        title="Delete"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  </div>
                )
              })}
              {providers.length === 0 && (
                <div className="text-center p-6 border border-dashed rounded-xl text-muted-foreground text-sm">
                  No providers configured in database.
                </div>
              )}
            </div>
          </div>

          {/* ─── LLM Models ─── */}
          <div className="space-y-4">
            <div className="flex items-center justify-between border-t pt-6">
              <div className="flex items-center gap-2">
                <Settings className="h-4 w-4 text-primary" />
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
                    onClick={() => {
                      setIsAddingModel(false)
                      setEditingModel(null)
                    }}
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
                      value={modelForm.id || ''}
                      disabled={!!editingModel}
                      placeholder="e.g. deepseek-v4-pro"
                      onChange={(e) => setModelForm({ ...modelForm, id: e.target.value })}
                    />
                  </div>
                  <div className="flex flex-col gap-1">
                    <label className="text-xs font-semibold text-muted-foreground">
                      LLM Provider
                    </label>
                    <select
                      value={modelForm.providerId || ''}
                      onChange={(e) => setModelForm({ ...modelForm, providerId: e.target.value })}
                      className="flex h-9 w-full rounded-md border bg-card px-3 py-1.5 text-sm text-foreground outline-none focus-visible:ring-1 focus-visible:ring-primary"
                    >
                      {providers.map((p) => (
                        <option key={p.id} value={p.id}>
                          {p.name} ({p.id})
                        </option>
                      ))}
                    </select>
                  </div>
                  <div className="flex flex-col gap-1">
                    <label className="text-xs font-semibold text-muted-foreground">
                      Display Name
                    </label>
                    <Input
                      value={modelForm.name || ''}
                      placeholder="e.g. DeepSeek-V4 Chat"
                      onChange={(e) => setModelForm({ ...modelForm, name: e.target.value })}
                    />
                  </div>
                  <div className="flex flex-col gap-1">
                    <label className="text-xs font-semibold text-muted-foreground">
                      API Model Name (actual payload)
                    </label>
                    <Input
                      value={modelForm.apiModel || ''}
                      placeholder="e.g. deepseek-chat"
                      onChange={(e) => setModelForm({ ...modelForm, apiModel: e.target.value })}
                    />
                  </div>
                  <div className="flex flex-col gap-1">
                    <label className="text-xs font-semibold text-muted-foreground">
                      Context Window (tokens)
                    </label>
                    <Input
                      type="number"
                      value={modelForm.contextWindow || 128000}
                      onChange={(e) =>
                        setModelForm({ ...modelForm, contextWindow: Number(e.target.value) })
                      }
                    />
                  </div>
                  <div className="flex flex-col gap-1">
                    <label className="text-xs font-semibold text-muted-foreground">
                      Temperature
                    </label>
                    <Input
                      type="number"
                      step="0.1"
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
                    />
                  </div>
                  <div className="flex flex-col gap-1">
                    <label className="text-xs font-semibold text-muted-foreground">
                      Max Completion Tokens
                    </label>
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
                  <div className="flex items-center gap-2 pt-5">
                    <Switch
                      checked={modelForm.enabled ?? true}
                      onCheckedChange={(val) => setModelForm({ ...modelForm, enabled: val })}
                    />
                    <span className="text-xs font-semibold text-foreground">Enabled</span>
                  </div>

                  {/* Thinking settings */}
                  <div className="sm:col-span-2 border-t pt-3 mt-1">
                    <div className="flex items-center gap-6">
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
                        <span className="text-xs font-semibold text-foreground">
                          Enable Reasoning/Thinking
                        </span>
                      </div>
                      {modelForm.thinking?.enabled && (
                        <div className="flex items-center gap-2 flex-1">
                          <label className="text-xs font-semibold text-muted-foreground whitespace-nowrap">
                            Reasoning Effort
                          </label>
                          <select
                            value={modelForm.thinking?.reasoningEffort || 'medium'}
                            onChange={(e) =>
                              setModelForm({
                                ...modelForm,
                                thinking: {
                                  ...(modelForm.thinking || { enabled: true }),
                                  reasoningEffort: e.target.value,
                                },
                              })
                            }
                            className="flex h-8 rounded-md border bg-card px-2.5 py-1 text-xs text-foreground outline-none"
                          >
                            <option value="low">Low</option>
                            <option value="medium">Medium</option>
                            <option value="high">High</option>
                          </select>
                        </div>
                      )}
                    </div>
                  </div>
                </div>
                <div className="flex justify-end gap-2 border-t pt-3">
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => {
                      setIsAddingModel(false)
                      setEditingModel(null)
                    }}
                  >
                    Cancel
                  </Button>
                  <Button size="sm" onClick={saveModelForm}>
                    Save Model
                  </Button>
                </div>
              </div>
            )}

            {/* Models List */}
            <div className="grid grid-cols-1 gap-3">
              {models.map((m) => (
                <div
                  key={m.id}
                  className={`flex flex-col sm:flex-row sm:items-center justify-between p-4 rounded-xl border bg-card hover:shadow-sm transition-all duration-200 ${
                    m.enabled ? 'border-border' : 'border-border/50 opacity-60'
                  }`}
                >
                  <div className="space-y-1.5 flex-1 min-w-0">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="font-semibold text-foreground">{m.name}</span>
                      <span className="text-xs font-mono text-muted-foreground">({m.id})</span>
                      <Badge variant="outline">{m.providerId}</Badge>
                      {m.thinking.enabled && (
                        <Badge variant="secondary">Thinking ({m.thinking.reasoningEffort})</Badge>
                      )}
                      {m.enabled ? (
                        <span className="flex h-2 w-2 rounded-full bg-emerald-500" />
                      ) : (
                        <span className="flex h-2 w-2 rounded-full bg-muted" />
                      )}
                    </div>
                    <div className="text-xs text-muted-foreground font-mono">
                      API Name: <span className="text-foreground">{m.apiModel || m.id}</span>
                    </div>
                    <div className="flex items-center gap-4 text-[10px] text-muted-foreground font-mono">
                      <div>Context: {m.contextWindow.toLocaleString()} tokens</div>
                      <div>Temp: {m.generation.temperature}</div>
                      <div>Max Out: {m.generation.maxTokens}</div>
                    </div>
                  </div>

                  <div className="flex items-center gap-2 mt-3 sm:mt-0 justify-end">
                    <Switch
                      checked={m.enabled}
                      onCheckedChange={() => toggleModelStatus(m)}
                      title="Toggle Status"
                    />
                    <Button
                      size="icon-xs"
                      variant="ghost"
                      onClick={() => startEditModel(m)}
                      title="Edit"
                    >
                      <Edit className="h-3.5 w-3.5" />
                    </Button>
                    <Button
                      size="icon-xs"
                      variant="ghost"
                      className="hover:text-destructive hover:bg-destructive/5"
                      onClick={() => handleDeleteModel(m.id)}
                      title="Delete"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </div>
              ))}
              {models.length === 0 && (
                <div className="text-center p-6 border border-dashed rounded-xl text-muted-foreground text-sm">
                  No models configured in database.
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
