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
  getToolsConfig,
  updateToolsConfig,
  getQQBotConfig,
  updateQQBotConfig,
  getLSPMCPConfig,
  updateLSPMCPConfig,
  getEmbeddingConfig,
  updateEmbeddingConfig,
  getSessionConfig,
  updateSessionConfig,
} from '@/lib/api'
import type {
  LLMProvider,
  LLMModel,
  DefaultModelsConfig,
  ToolsConfig,
  QQBotConfig,
  LSPMCPConfig,
  EmbeddingConfig,
  SessionConfig,
  LSPMCPEntry,
} from '@/types'
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

  // DB sub-tab selection
  const [subTab, setSubTab] = useState<'llm' | 'tools' | 'qqbot' | 'lspmcp' | 'embedding' | 'session'>('llm')

  // DB sub-tabs config state
  const [toolsConfig, setToolsConfig] = useState<ToolsConfig | null>(null)
  const [qqbotConfig, setQqbotConfig] = useState<QQBotConfig | null>(null)
  const [lspmcpConfig, setLspmcpConfig] = useState<LSPMCPConfig | null>(null)
  const [embeddingConfig, setEmbeddingConfig] = useState<EmbeddingConfig | null>(null)
  const [sessionConfig, setSessionConfig] = useState<SessionConfig | null>(null)

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

      const dbTools = await getToolsConfig()
      const dbQqbot = await getQQBotConfig()
      const dbLspmcp = await getLSPMCPConfig()
      const dbEmbedding = await getEmbeddingConfig()
      const dbSession = await getSessionConfig()

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

      setToolsConfig(dbTools)
      setQqbotConfig(dbQqbot)
      setLspmcpConfig(dbLspmcp)
      setEmbeddingConfig(dbEmbedding)
      setSessionConfig(dbSession)
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

  const handleSaveTools = async () => {
    if (!toolsConfig) return
    setError(null)
    setSuccessMsg(null)
    try {
      await updateToolsConfig(toolsConfig)
      setSuccessMsg('Tools configuration updated successfully!')
      setTimeout(() => setSuccessMsg(null), 3000)
      loadAll()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const handleSaveQQBot = async () => {
    if (!qqbotConfig) return
    setError(null)
    setSuccessMsg(null)
    try {
      await updateQQBotConfig(qqbotConfig)
      setSuccessMsg('QQ Bot configuration updated successfully!')
      setTimeout(() => setSuccessMsg(null), 3000)
      loadAll()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const handleSaveLSPMCP = async () => {
    if (!lspmcpConfig) return
    setError(null)
    setSuccessMsg(null)
    try {
      await updateLSPMCPConfig(lspmcpConfig)
      setSuccessMsg('LSP MCP configuration updated successfully!')
      setTimeout(() => setSuccessMsg(null), 3000)
      loadAll()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const handleSaveEmbedding = async () => {
    if (!embeddingConfig) return
    setError(null)
    setSuccessMsg(null)
    try {
      await updateEmbeddingConfig(embeddingConfig)
      setSuccessMsg('Embedding configuration updated successfully!')
      setTimeout(() => setSuccessMsg(null), 3000)
      loadAll()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const handleSaveSession = async () => {
    if (!sessionConfig) return
    setError(null)
    setSuccessMsg(null)
    try {
      await updateSessionConfig(sessionConfig)
      setSuccessMsg('Session configuration updated successfully!')
      setTimeout(() => setSuccessMsg(null), 3000)
      loadAll()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  const handleAddLSPServer = () => {
    if (!lspmcpConfig) return
    const newServer: LSPMCPEntry = {
      id: 'new-server-' + Math.random().toString(36).substring(2, 7),
      command: '',
      args: [],
      languages: [],
      extensions: [],
      disabled: false,
    }
    setLspmcpConfig({
      ...lspmcpConfig,
      servers: [...(lspmcpConfig.servers || []), newServer]
    })
  }

  const handleRemoveLSPServer = (index: number) => {
    if (!lspmcpConfig) return
    const updated = [...lspmcpConfig.servers]
    updated.splice(index, 1)
    setLspmcpConfig({
      ...lspmcpConfig,
      servers: updated
    })
  }

  const handleUpdateLSPServer = (index: number, field: keyof LSPMCPEntry, val: any) => {
    if (!lspmcpConfig) return
    const updated = [...lspmcpConfig.servers]
    updated[index] = {
      ...updated[index],
      [field]: val
    }
    setLspmcpConfig({
      ...lspmcpConfig,
      servers: updated
    })
  }

  const handleAddEmbeddingProvider = () => {
    if (!embeddingConfig) return
    const newProvider = {
      id: 'new-provider-' + Math.random().toString(36).substring(2, 7),
      name: '',
      baseUrl: '',
      apiKey: '',
      apiKeyEnv: '',
      enabled: false,
    }
    setEmbeddingConfig({
      ...embeddingConfig,
      providers: [...(embeddingConfig.providers || []), newProvider]
    })
  }

  const handleRemoveEmbeddingProvider = (index: number) => {
    if (!embeddingConfig) return
    const updated = [...(embeddingConfig.providers || [])]
    updated.splice(index, 1)
    setEmbeddingConfig({
      ...embeddingConfig,
      providers: updated
    })
  }

  const handleUpdateEmbeddingProvider = (index: number, field: string, val: any) => {
    if (!embeddingConfig) return
    const updated = [...(embeddingConfig.providers || [])]
    updated[index] = {
      ...updated[index],
      [field]: val
    }
    setEmbeddingConfig({
      ...embeddingConfig,
      providers: updated
    })
  }

  const handleAddEmbeddingModel = () => {
    if (!embeddingConfig) return
    const newModel = {
      id: 'new-model-' + Math.random().toString(36).substring(2, 7),
      providerId: embeddingConfig.providers?.[0]?.id || '',
      name: '',
      dimension: 1024,
      batchSize: 32,
      normalize: true,
      enabled: false,
      isDefault: false,
    }
    setEmbeddingConfig({
      ...embeddingConfig,
      models: [...(embeddingConfig.models || []), newModel]
    })
  }

  const handleRemoveEmbeddingModel = (index: number) => {
    if (!embeddingConfig) return
    const updated = [...(embeddingConfig.models || [])]
    updated.splice(index, 1)
    setEmbeddingConfig({
      ...embeddingConfig,
      models: updated
    })
  }

  const handleUpdateEmbeddingModel = (index: number, field: string, val: any) => {
    if (!embeddingConfig) return
    const updated = [...(embeddingConfig.models || [])]
    updated[index] = {
      ...updated[index],
      [field]: val
    }
    if (field === 'isDefault' && val === true) {
      updated.forEach((m, idx) => {
        if (idx !== index) {
          m.isDefault = false
        }
      })
    }
    setEmbeddingConfig({
      ...embeddingConfig,
      models: updated
    })
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
        <div className="space-y-6 pb-10">
          {/* Sub-tab Switcher */}
          <div className="flex flex-wrap gap-2 p-1 bg-muted rounded-lg w-max mb-6">
            <button
              type="button"
              onClick={() => setSubTab('llm')}
              className={`px-3 py-1.5 text-xs font-semibold rounded-md transition-all ${
                subTab === 'llm'
                  ? 'bg-background text-foreground shadow-sm'
                  : 'text-muted-foreground hover:text-foreground'
              }`}
            >
              Models & Providers
            </button>
            <button
              type="button"
              onClick={() => setSubTab('tools')}
              className={`px-3 py-1.5 text-xs font-semibold rounded-md transition-all ${
                subTab === 'tools'
                  ? 'bg-background text-foreground shadow-sm'
                  : 'text-muted-foreground hover:text-foreground'
              }`}
            >
              Tools Settings
            </button>
            <button
              type="button"
              onClick={() => setSubTab('qqbot')}
              className={`px-3 py-1.5 text-xs font-semibold rounded-md transition-all ${
                subTab === 'qqbot'
                  ? 'bg-background text-foreground shadow-sm'
                  : 'text-muted-foreground hover:text-foreground'
              }`}
            >
              QQ Bot
            </button>
            <button
              type="button"
              onClick={() => setSubTab('lspmcp')}
              className={`px-3 py-1.5 text-xs font-semibold rounded-md transition-all ${
                subTab === 'lspmcp'
                  ? 'bg-background text-foreground shadow-sm'
                  : 'text-muted-foreground hover:text-foreground'
              }`}
            >
              LSP MCP
            </button>
            <button
              type="button"
              onClick={() => setSubTab('embedding')}
              className={`px-3 py-1.5 text-xs font-semibold rounded-md transition-all ${
                subTab === 'embedding'
                  ? 'bg-background text-foreground shadow-sm'
                  : 'text-muted-foreground hover:text-foreground'
              }`}
            >
              Embedding
            </button>
            <button
              type="button"
              onClick={() => setSubTab('session')}
              className={`px-3 py-1.5 text-xs font-semibold rounded-md transition-all ${
                subTab === 'session'
                  ? 'bg-background text-foreground shadow-sm'
                  : 'text-muted-foreground hover:text-foreground'
              }`}
            >
              Session Settings
            </button>
          </div>

          {subTab === 'llm' && (
            <div className="space-y-8">
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
                  {providers.map((p) => (
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
                  ))}
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
                          {m.thinking?.enabled && (
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

          {subTab === 'tools' && toolsConfig && (
            <div className="rounded-xl border bg-card p-6 shadow-sm space-y-6">
              <div className="flex items-center justify-between border-b pb-3">
                <div className="flex items-center gap-2">
                  <Settings className="h-4 w-4 text-primary" />
                  <h3 className="font-semibold text-foreground">Tools Limit Configurations</h3>
                </div>
                <Button size="sm" onClick={handleSaveTools}>
                  Save Tools Settings
                </Button>
              </div>

              <div className="space-y-6">
                <div>
                  <h4 className="text-sm font-semibold text-foreground border-b pb-1 mb-3">File System Read Limits</h4>
                  <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-4 gap-4">
                    <div className="flex flex-col gap-1.5">
                      <label className="text-xs font-semibold text-muted-foreground">Max File Size (Bytes)</label>
                      <Input
                        type="number"
                        value={toolsConfig.maxFileSize}
                        onChange={(e) => setToolsConfig({ ...toolsConfig, maxFileSize: Number(e.target.value) })}
                      />
                    </div>
                    <div className="flex flex-col gap-1.5">
                      <label className="text-xs font-semibold text-muted-foreground">Max Search Matches</label>
                      <Input
                        type="number"
                        value={toolsConfig.maxMatches}
                        onChange={(e) => setToolsConfig({ ...toolsConfig, maxMatches: Number(e.target.value) })}
                      />
                    </div>
                    <div className="flex flex-col gap-1.5">
                      <label className="text-xs font-semibold text-muted-foreground">Max Line Length</label>
                      <Input
                        type="number"
                        value={toolsConfig.maxLineLen}
                        onChange={(e) => setToolsConfig({ ...toolsConfig, maxLineLen: Number(e.target.value) })}
                      />
                    </div>
                    <div className="flex flex-col gap-1.5">
                      <label className="text-xs font-semibold text-muted-foreground">Max Glob List Items</label>
                      <Input
                        type="number"
                        value={toolsConfig.maxGlobItems}
                        onChange={(e) => setToolsConfig({ ...toolsConfig, maxGlobItems: Number(e.target.value) })}
                      />
                    </div>
                  </div>
                </div>

                <div>
                  <h4 className="text-sm font-semibold text-foreground border-b pb-1 mb-3">File Write Limits</h4>
                  <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-4 gap-4">
                    <div className="flex flex-col gap-1.5">
                      <label className="text-xs font-semibold text-muted-foreground">Max Write Size (Bytes)</label>
                      <Input
                        type="number"
                        value={toolsConfig.maxWriteSize}
                        onChange={(e) => setToolsConfig({ ...toolsConfig, maxWriteSize: Number(e.target.value) })}
                      />
                    </div>
                    <div className="flex flex-col gap-1.5">
                      <label className="text-xs font-semibold text-muted-foreground">Max Multi-Write (Bytes)</label>
                      <Input
                        type="number"
                        value={toolsConfig.maxMultiWriteBytes}
                        onChange={(e) => setToolsConfig({ ...toolsConfig, maxMultiWriteBytes: Number(e.target.value) })}
                      />
                    </div>
                    <div className="flex flex-col gap-1.5">
                      <label className="text-xs font-semibold text-muted-foreground">Max Multi-Write Files</label>
                      <Input
                        type="number"
                        value={toolsConfig.maxMultiWriteFiles}
                        onChange={(e) => setToolsConfig({ ...toolsConfig, maxMultiWriteFiles: Number(e.target.value) })}
                      />
                    </div>
                    <div className="flex flex-col gap-1.5">
                      <label className="text-xs font-semibold text-muted-foreground">Max Replace Edits</label>
                      <Input
                        type="number"
                        value={toolsConfig.maxReplaceEdits}
                        onChange={(e) => setToolsConfig({ ...toolsConfig, maxReplaceEdits: Number(e.target.value) })}
                      />
                    </div>
                  </div>
                </div>

                <div>
                  <h4 className="text-sm font-semibold text-foreground border-b pb-1 mb-3">Web Search & Fetch</h4>
                  <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-4">
                    <div className="flex flex-col gap-1.5">
                      <label className="text-xs font-semibold text-muted-foreground">HTTP Max Body (Bytes)</label>
                      <Input
                        type="number"
                        value={toolsConfig.httpMaxBody}
                        onChange={(e) => setToolsConfig({ ...toolsConfig, httpMaxBody: Number(e.target.value) })}
                      />
                    </div>
                    <div className="flex flex-col gap-1.5">
                      <label className="text-xs font-semibold text-muted-foreground">HTTP Timeout (ms)</label>
                      <Input
                        type="number"
                        value={toolsConfig.httpTimeoutMs}
                        onChange={(e) => setToolsConfig({ ...toolsConfig, httpTimeoutMs: Number(e.target.value) })}
                      />
                    </div>
                    <div className="flex flex-col gap-1.5">
                      <label className="text-xs font-semibold text-muted-foreground">Web Search Timeout (ms)</label>
                      <Input
                        type="number"
                        value={toolsConfig.webSearchTimeoutMs}
                        onChange={(e) => setToolsConfig({ ...toolsConfig, webSearchTimeoutMs: Number(e.target.value) })}
                      />
                    </div>
                    <div className="flex items-center gap-2 pt-4">
                      <Switch
                        checked={toolsConfig.httpBlockPrivate}
                        onCheckedChange={(val) => setToolsConfig({ ...toolsConfig, httpBlockPrivate: val })}
                      />
                      <span className="text-xs font-semibold text-foreground">Block Private Networks</span>
                    </div>
                  </div>
                  <div className="mt-4 flex flex-col gap-1.5">
                    <label className="text-xs font-semibold text-muted-foreground">Allowed HTTP Hosts (comma separated)</label>
                    <Input
                      type="text"
                      placeholder="e.g. api.github.com, google.com"
                      value={toolsConfig.httpAllowedHosts?.join(', ') || ''}
                      onChange={(e) => setToolsConfig({
                        ...toolsConfig,
                        httpAllowedHosts: e.target.value.split(',').map(s => s.trim()).filter(Boolean)
                      })}
                    />
                  </div>
                </div>

                <div>
                  <h4 className="text-sm font-semibold text-foreground border-b pb-1 mb-3">Shell Execute / Bash</h4>
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                    <div className="flex flex-col gap-1.5">
                      <label className="text-xs font-semibold text-muted-foreground">Shell Max Output Captured (Bytes)</label>
                      <Input
                        type="number"
                        value={toolsConfig.shellMaxOutput}
                        onChange={(e) => setToolsConfig({ ...toolsConfig, shellMaxOutput: Number(e.target.value) })}
                      />
                    </div>
                  </div>
                  <div className="grid grid-cols-1 gap-4 mt-4">
                    <div className="flex flex-col gap-1.5">
                      <label className="text-xs font-semibold text-muted-foreground">Block Command Regexes (comma separated)</label>
                      <Input
                        type="text"
                        placeholder="e.g. ^\s*sudo\b, ^\s*poweroff\b"
                        value={toolsConfig.shellBlockRegexes?.join(', ') || ''}
                        onChange={(e) => setToolsConfig({
                          ...toolsConfig,
                          shellBlockRegexes: e.target.value.split(',').map(s => s.trim()).filter(Boolean)
                        })}
                      />
                    </div>
                    <div className="flex flex-col gap-1.5">
                      <label className="text-xs font-semibold text-muted-foreground">Confirm Command Regexes (comma separated)</label>
                      <Input
                        type="text"
                        placeholder="e.g. ^\s*rm\b, ^\s*dd\b"
                        value={toolsConfig.shellConfirmRegexes?.join(', ') || ''}
                        onChange={(e) => setToolsConfig({
                          ...toolsConfig,
                          shellConfirmRegexes: e.target.value.split(',').map(s => s.trim()).filter(Boolean)
                        })}
                      />
                    </div>
                  </div>
                </div>
              </div>
            </div>
          )}

          {subTab === 'qqbot' && qqbotConfig && (
            <div className="rounded-xl border bg-card p-6 shadow-sm space-y-6">
              <div className="flex items-center justify-between border-b pb-3">
                <div className="flex items-center gap-2">
                  <Database className="h-4 w-4 text-primary" />
                  <h3 className="font-semibold text-foreground">QQ Bot WebSocket Config</h3>
                </div>
                <Button size="sm" onClick={handleSaveQQBot}>
                  Save QQ Bot Settings
                </Button>
              </div>

              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div className="flex flex-col gap-1.5">
                  <label className="text-xs font-semibold text-muted-foreground">App ID</label>
                  <Input
                    type="text"
                    placeholder="Enter AppID"
                    value={qqbotConfig.appId || ''}
                    onChange={(e) => setQqbotConfig({ ...qqbotConfig, appId: e.target.value })}
                  />
                </div>
                <div className="flex flex-col gap-1.5">
                  <label className="text-xs font-semibold text-muted-foreground">App Secret</label>
                  <Input
                    type="password"
                    placeholder="Enter AppSecret"
                    value={qqbotConfig.appSecret || ''}
                    onChange={(e) => setQqbotConfig({ ...qqbotConfig, appSecret: e.target.value })}
                  />
                </div>
                <div className="flex flex-col gap-1.5">
                  <label className="text-xs font-semibold text-muted-foreground">Intents Mask</label>
                  <Input
                    type="number"
                    value={qqbotConfig.intents || 0}
                    onChange={(e) => setQqbotConfig({ ...qqbotConfig, intents: Number(e.target.value) })}
                  />
                </div>
                <div className="flex items-center gap-6 pt-4 flex-wrap">
                  <div className="flex items-center gap-2">
                    <Switch
                      checked={qqbotConfig.enabled}
                      onCheckedChange={(val) => setQqbotConfig({ ...qqbotConfig, enabled: val })}
                    />
                    <span className="text-xs font-semibold text-foreground">Enable Bot WebSocket Gateway</span>
                  </div>
                  <div className="flex items-center gap-2">
                    <Switch
                      checked={qqbotConfig.sandbox}
                      onCheckedChange={(val) => setQqbotConfig({ ...qqbotConfig, sandbox: val })}
                    />
                    <span className="text-xs font-semibold text-foreground">Sandbox Mode</span>
                  </div>
                </div>
              </div>
            </div>
          )}

          {subTab === 'lspmcp' && lspmcpConfig && (
            <div className="rounded-xl border bg-card p-6 shadow-sm space-y-6">
              <div className="flex items-center justify-between border-b pb-3">
                <div className="flex items-center gap-2">
                  <Settings className="h-4 w-4 text-primary" />
                  <h3 className="font-semibold text-foreground">Built-in LSP MCP Server Overrides</h3>
                </div>
                <div className="flex gap-2">
                  <Button size="sm" variant="outline" onClick={handleAddLSPServer}>
                    <Plus className="h-3.5 w-3.5 mr-1" /> Add Server
                  </Button>
                  <Button size="sm" onClick={handleSaveLSPMCP}>
                    Save LSP MCP Settings
                  </Button>
                </div>
              </div>

              <div className="space-y-4">
                {(lspmcpConfig.servers || []).map((srv, idx) => (
                  <div key={srv.id || idx} className="p-4 border rounded-lg space-y-4 relative bg-card/40">
                    <button
                      type="button"
                      onClick={() => handleRemoveLSPServer(idx)}
                      className="absolute top-4 right-4 text-muted-foreground hover:text-destructive transition-colors"
                    >
                      <Trash2 className="h-4 w-4" />
                    </button>

                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                      <div className="flex flex-col gap-1.5">
                        <label className="text-xs font-semibold text-muted-foreground font-mono">Server ID / Identifier</label>
                        <Input
                          type="text"
                          placeholder="e.g. gopls"
                          value={srv.id || ''}
                          onChange={(e) => handleUpdateLSPServer(idx, 'id', e.target.value)}
                        />
                      </div>
                      <div className="flex flex-col gap-1.5">
                        <label className="text-xs font-semibold text-muted-foreground">Command</label>
                        <Input
                          type="text"
                          placeholder="e.g. gopls"
                          value={srv.command || ''}
                          onChange={(e) => handleUpdateLSPServer(idx, 'command', e.target.value)}
                        />
                      </div>
                      <div className="flex flex-col gap-1.5">
                        <label className="text-xs font-semibold text-muted-foreground">Arguments (comma separated)</label>
                        <Input
                          type="text"
                          placeholder="-mode=mcp, -v"
                          value={srv.args?.join(', ') || ''}
                          onChange={(e) => handleUpdateLSPServer(idx, 'args', e.target.value.split(',').map(s => s.trim()).filter(Boolean))}
                        />
                      </div>
                      <div className="flex flex-col gap-1.5">
                        <label className="text-xs font-semibold text-muted-foreground">Languages (comma separated)</label>
                        <Input
                          type="text"
                          placeholder="go, golang"
                          value={srv.languages?.join(', ') || ''}
                          onChange={(e) => handleUpdateLSPServer(idx, 'languages', e.target.value.split(',').map(s => s.trim()).filter(Boolean))}
                        />
                      </div>
                      <div className="flex flex-col gap-1.5">
                        <label className="text-xs font-semibold text-muted-foreground">Extensions (comma separated)</label>
                        <Input
                          type="text"
                          placeholder=".go"
                          value={srv.extensions?.join(', ') || ''}
                          onChange={(e) => handleUpdateLSPServer(idx, 'extensions', e.target.value.split(',').map(s => s.trim()).filter(Boolean))}
                        />
                      </div>
                      <div className="flex items-center gap-2 pt-6">
                        <Switch
                          checked={!srv.disabled}
                          onCheckedChange={(val) => handleUpdateLSPServer(idx, 'disabled', !val)}
                        />
                        <span className="text-xs font-semibold text-foreground">Enabled</span>
                      </div>
                    </div>
                  </div>
                ))}

                {(lspmcpConfig.servers || []).length === 0 && (
                  <div className="text-center p-6 border border-dashed rounded-xl text-muted-foreground text-sm">
                    No custom LSP servers defined. Using default built-in servers.
                  </div>
                )}
              </div>
            </div>
          )}

          {subTab === 'embedding' && embeddingConfig && (
            <div className="rounded-xl border bg-card p-6 shadow-sm space-y-8">
              <div className="flex items-center justify-between border-b pb-3">
                <div className="flex items-center gap-2">
                  <Database className="h-4 w-4 text-primary" />
                  <h3 className="font-semibold text-foreground">Embedding (Vector Store) Settings</h3>
                </div>
                <Button size="sm" onClick={handleSaveEmbedding}>
                  Save Embedding Settings
                </Button>
              </div>

              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div className="flex items-center gap-2">
                  <Switch
                    checked={embeddingConfig.enabled}
                    onCheckedChange={(val) => setEmbeddingConfig({ ...embeddingConfig, enabled: val })}
                  />
                  <span className="text-xs font-semibold text-foreground">Enable Permanent Memory / Vector Store</span>
                </div>
                <div className="flex flex-col gap-1.5">
                  <label className="text-xs font-semibold text-muted-foreground">
                    Min Similarity Threshold (0.0–1.0)
                  </label>
                  <Input
                    type="number"
                    step="0.01"
                    min="0"
                    max="1"
                    value={embeddingConfig.minSimilarity ?? 0.65}
                    onChange={(e) => setEmbeddingConfig({ ...embeddingConfig, minSimilarity: Number(e.target.value) })}
                  />
                </div>
              </div>


              {/* Embedding Providers Section */}
              <div className="space-y-4 pt-4 border-t">
                <div className="flex items-center justify-between">
                  <h4 className="text-sm font-semibold text-foreground">Embedding Providers</h4>
                  <Button size="xs" variant="outline" onClick={handleAddEmbeddingProvider}>
                    <Plus className="h-3 w-3 mr-1" /> Add Provider
                  </Button>
                </div>
                
                <div className="space-y-3">
                  {(embeddingConfig.providers || []).map((prov, idx) => (
                    <div key={prov.id || idx} className="p-4 border rounded-lg relative space-y-4 bg-muted/20">
                      <button
                        type="button"
                        onClick={() => handleRemoveEmbeddingProvider(idx)}
                        className="absolute top-4 right-4 text-muted-foreground hover:text-destructive transition-colors"
                      >
                        <Trash2 className="h-4 w-4" />
                      </button>
                      <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-4">
                        <div className="flex flex-col gap-1">
                          <label className="text-xs font-semibold text-muted-foreground font-mono">Provider ID</label>
                          <Input
                            type="text"
                            placeholder="e.g. local"
                            value={prov.id || ''}
                            onChange={(e) => handleUpdateEmbeddingProvider(idx, 'id', e.target.value)}
                          />
                        </div>
                        <div className="flex flex-col gap-1">
                          <label className="text-xs font-semibold text-muted-foreground">Provider Name</label>
                          <Input
                            type="text"
                            placeholder="e.g. Ollama"
                            value={prov.name || ''}
                            onChange={(e) => handleUpdateEmbeddingProvider(idx, 'name', e.target.value)}
                          />
                        </div>
                        <div className="flex flex-col gap-1">
                          <label className="text-xs font-semibold text-muted-foreground">Base URL</label>
                          <Input
                            type="text"
                            placeholder="http://localhost:11434"
                            value={prov.baseUrl || ''}
                            onChange={(e) => handleUpdateEmbeddingProvider(idx, 'baseUrl', e.target.value)}
                          />
                        </div>
                        <div className="flex flex-col gap-1">
                          <label className="text-xs font-semibold text-muted-foreground">API Key (Direct)</label>
                          <Input
                            type="password"
                            placeholder="sk-..."
                            value={prov.apiKey || ''}
                            onChange={(e) => handleUpdateEmbeddingProvider(idx, 'apiKey', e.target.value)}
                          />
                        </div>
                        <div className="flex flex-col gap-1">
                          <label className="text-xs font-semibold text-muted-foreground font-mono">API Key Env Variable</label>
                          <Input
                            type="text"
                            placeholder="e.g. OLLAMA_API_KEY"
                            value={prov.apiKeyEnv || ''}
                            onChange={(e) => handleUpdateEmbeddingProvider(idx, 'apiKeyEnv', e.target.value)}
                          />
                        </div>
                        <div className="flex items-center gap-2 pt-6">
                          <Switch
                            checked={prov.enabled}
                            onCheckedChange={(val) => handleUpdateEmbeddingProvider(idx, 'enabled', val)}
                          />
                          <span className="text-xs font-semibold text-foreground">Enabled</span>
                        </div>
                      </div>
                    </div>
                  ))}
                  {(embeddingConfig.providers || []).length === 0 && (
                    <div className="text-center p-6 border border-dashed rounded-xl text-muted-foreground text-xs">
                      No embedding providers defined.
                    </div>
                  )}
                </div>
              </div>

              {/* Embedding Models Section */}
              <div className="space-y-4 pt-4 border-t">
                <div className="flex items-center justify-between">
                  <h4 className="text-sm font-semibold text-foreground">Embedding Models</h4>
                  <Button size="xs" variant="outline" onClick={handleAddEmbeddingModel}>
                    <Plus className="h-3 w-3 mr-1" /> Add Model
                  </Button>
                </div>

                <div className="space-y-3">
                  {(embeddingConfig.models || []).map((mdl, idx) => (
                    <div key={mdl.id || idx} className="p-4 border rounded-lg relative space-y-4 bg-muted/20">
                      <button
                        type="button"
                        onClick={() => handleRemoveEmbeddingModel(idx)}
                        className="absolute top-4 right-4 text-muted-foreground hover:text-destructive transition-colors"
                      >
                        <Trash2 className="h-4 w-4" />
                      </button>
                      <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-4">
                        <div className="flex flex-col gap-1">
                          <label className="text-xs font-semibold text-muted-foreground font-mono">Model ID</label>
                          <Input
                            type="text"
                            placeholder="e.g. nomic-embed-text"
                            value={mdl.id || ''}
                            onChange={(e) => handleUpdateEmbeddingModel(idx, 'id', e.target.value)}
                          />
                        </div>
                        <div className="flex flex-col gap-1 font-mono">
                          <label className="text-xs font-semibold text-muted-foreground">Provider ID</label>
                          <select
                            value={mdl.providerId || ''}
                            onChange={(e) => handleUpdateEmbeddingModel(idx, 'providerId', e.target.value)}
                            className="flex h-9 w-full rounded-md border bg-card px-3 py-1.5 text-sm text-foreground outline-none"
                          >
                            <option value="">Select Provider</option>
                            {(embeddingConfig.providers || []).map(p => (
                              <option key={p.id} value={p.id}>{p.name} ({p.id})</option>
                            ))}
                          </select>
                        </div>
                        <div className="flex flex-col gap-1">
                          <label className="text-xs font-semibold text-muted-foreground">Model Name</label>
                          <Input
                            type="text"
                            placeholder="e.g. Nomic Text Embeddings"
                            value={mdl.name || ''}
                            onChange={(e) => handleUpdateEmbeddingModel(idx, 'name', e.target.value)}
                          />
                        </div>
                        <div className="flex flex-col gap-1">
                          <label className="text-xs font-semibold text-muted-foreground">Dimension Size</label>
                          <Input
                            type="number"
                            value={mdl.dimension || 1024}
                            onChange={(e) => handleUpdateEmbeddingModel(idx, 'dimension', Number(e.target.value))}
                          />
                        </div>
                        <div className="flex flex-col gap-1">
                          <label className="text-xs font-semibold text-muted-foreground">Batch Size</label>
                          <Input
                            type="number"
                            value={mdl.batchSize || 32}
                            onChange={(e) => handleUpdateEmbeddingModel(idx, 'batchSize', Number(e.target.value))}
                          />
                        </div>
                        <div className="flex items-center gap-6 pt-6 flex-wrap">
                          <div className="flex items-center gap-2">
                            <Switch
                              checked={mdl.normalize}
                              onCheckedChange={(val) => handleUpdateEmbeddingModel(idx, 'normalize', val)}
                            />
                            <span className="text-xs font-semibold text-foreground">Normalize</span>
                          </div>
                          <div className="flex items-center gap-2">
                            <Switch
                              checked={mdl.isDefault}
                              onCheckedChange={(val) => handleUpdateEmbeddingModel(idx, 'isDefault', val)}
                            />
                            <span className="text-xs font-semibold text-foreground">Is Default Model</span>
                          </div>
                          <div className="flex items-center gap-2">
                            <Switch
                              checked={mdl.enabled}
                              onCheckedChange={(val) => handleUpdateEmbeddingModel(idx, 'enabled', val)}
                            />
                            <span className="text-xs font-semibold text-foreground">Enabled</span>
                          </div>
                        </div>
                      </div>
                    </div>
                  ))}
                  {(embeddingConfig.models || []).length === 0 && (
                    <div className="text-center p-6 border border-dashed rounded-xl text-muted-foreground text-xs">
                      No embedding models defined.
                    </div>
                  )}
                </div>
              </div>
            </div>
          )}

          {subTab === 'session' && sessionConfig && (
            <div className="rounded-xl border bg-card p-6 shadow-sm space-y-6">
              <div className="flex items-center justify-between border-b pb-3">
                <div className="flex items-center gap-2">
                  <Database className="h-4 w-4 text-primary" />
                  <h3 className="font-semibold text-foreground">Session / Timeline Config</h3>
                </div>
                <Button size="sm" onClick={handleSaveSession}>
                  Save Session Settings
                </Button>
              </div>

              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div className="flex flex-col gap-1.5">
                  <label className="text-xs font-semibold text-muted-foreground">Timeline Max File Size (MB)</label>
                  <Input
                    type="number"
                    value={sessionConfig.timelineMaxFileMB || 50}
                    onChange={(e) => setSessionConfig({ ...sessionConfig, timelineMaxFileMB: Number(e.target.value) })}
                  />
                </div>
              </div>
            </div>
          )}
        </div>
      )}

    </div>
  )
}
