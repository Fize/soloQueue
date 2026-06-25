import { useState, useEffect } from 'react'
import { useSearchParams } from 'react-router-dom'
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
  getSimulationConfig,
  updateSimulationConfig,
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
  SimulationConfig,
  LSPMCPEntry,
} from '@/types'
import { FileText, Database } from 'lucide-react'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { toast } from 'sonner'
import { LLMSection } from './LLMSection'
import { ToolsSection } from './ToolsSection'
import { QQBotSection } from './QQBotSection'
import { LSPMCPSection } from './LSPMCPSection'
import { EmbeddingSection } from './EmbeddingSection'
import { SessionSection } from './SessionSection'
import { SimulationSection } from './SimulationSection'

SyntaxHighlighter.registerLanguage('toml', toml)

type TabType = 'db' | 'toml'

export function ConfigTabShell() {
  const [searchParams, setSearchParams] = useSearchParams()
  const activeTab = (searchParams.get('tab') as TabType) || 'db'
  const subTab = (searchParams.get('subTab') || 'llm') as
    | 'llm'
    | 'tools'
    | 'qqbot'
    | 'lspmcp'
    | 'embedding'
    | 'session'
    | 'simulation'

  const handleTabChange = (val: string) => {
    setSearchParams({ tab: val, subTab })
  }

  const handleSubTabChange = (val: string) => {
    setSearchParams({ tab: activeTab, subTab: val })
  }

  const [tomlContent, setTomlContent] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  // DB settings state
  const [providers, setProviders] = useState<LLMProvider[]>([])
  const [deleteProviderTarget, setDeleteProviderTarget] = useState<LLMProvider | null>(null)
  const [deleteModelTarget, setDeleteModelTarget] = useState<LLMModel | null>(null)
  const [models, setModels] = useState<LLMModel[]>([])
  const [defaultModels, setDefaultModels] = useState<DefaultModelsConfig>({
    expert: '',
    superior: '',
    universal: '',
    fast: '',
    fallback: '',
  })

  // DB sub-tabs config state
  const [toolsConfig, setToolsConfig] = useState<ToolsConfig | null>(null)
  const [qqbotConfig, setQqbotConfig] = useState<QQBotConfig | null>(null)
  const [lspmcpConfig, setLspmcpConfig] = useState<LSPMCPConfig | null>(null)
  const [embeddingConfig, setEmbeddingConfig] = useState<EmbeddingConfig | null>(null)
  const [sessionConfig, setSessionConfig] = useState<SessionConfig | null>(null)
  const [simulationConfig, setSimulationConfig] = useState<SimulationConfig | null>(null)

  useEffect(() => {
    loadAll()
  }, [])

  const loadAll = async () => {
    setLoading(true)
    try {
      const rawToml = await getConfigToml()
      setTomlContent(rawToml)

      const [
        dbProviders,
        dbModels,
        dbDefaults,
        dbTools,
        dbQqbot,
        dbLspmcp,
        dbEmbedding,
        dbSession,
        dbSimulation,
      ] = await Promise.all([
        listProviders(),
        listModels(),
        getDefaultModels(),
        getToolsConfig(),
        getQQBotConfig(),
        getLSPMCPConfig(),
        getEmbeddingConfig(),
        getSessionConfig(),
        getSimulationConfig(),
      ])

      setProviders(dbProviders || [])
      setModels(dbModels || [])
      setDefaultModels(
        dbDefaults || { expert: '', superior: '', universal: '', fast: '', fallback: '' }
      )
      setToolsConfig(dbTools)
      setQqbotConfig(dbQqbot)
      setLspmcpConfig(dbLspmcp)
      setEmbeddingConfig(dbEmbedding)
      setSessionConfig(dbSession)
      setSimulationConfig(dbSimulation)
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  // ─── Save Handlers ──────────────────────────────────────────────────────────

  const handleSaveDefaults = async () => {
    try {
      await updateDefaultModels(defaultModels)
      toast.success('Default models updated successfully!')
      loadAll()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  const handleSaveTools = async () => {
    if (!toolsConfig) return
    try {
      await updateToolsConfig(toolsConfig)
      toast.success('Tools configuration updated successfully!')
      loadAll()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  const handleSaveQQBot = async () => {
    if (!qqbotConfig) return
    try {
      await updateQQBotConfig(qqbotConfig)
      toast.success('QQ Bot configuration updated successfully!')
      loadAll()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  const handleSaveLSPMCP = async () => {
    if (!lspmcpConfig) return
    try {
      await updateLSPMCPConfig(lspmcpConfig)
      toast.success('LSP MCP configuration updated successfully!')
      loadAll()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  const handleSaveEmbedding = async () => {
    if (!embeddingConfig) return
    try {
      await updateEmbeddingConfig(embeddingConfig)
      toast.success('Embedding configuration updated successfully!')
      loadAll()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  const handleSaveSession = async () => {
    if (!sessionConfig) return
    try {
      await updateSessionConfig(sessionConfig)
      toast.success('Session configuration updated successfully!')
      loadAll()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  const handleSaveSimulation = async () => {
    if (!simulationConfig) return
    try {
      await updateSimulationConfig(simulationConfig)
      toast.success('Simulation configuration updated successfully!')
      loadAll()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  // ─── Embedding CRUD ─────────────────────────────────────────────────────────

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
      providers: [...(embeddingConfig.providers || []), newProvider],
    })
  }

  const handleRemoveEmbeddingProvider = (index: number) => {
    if (!embeddingConfig) return
    const updated = [...(embeddingConfig.providers || [])]
    updated.splice(index, 1)
    setEmbeddingConfig({ ...embeddingConfig, providers: updated })
  }

  const handleUpdateEmbeddingProvider = (index: number, field: string, val: any) => {
    if (!embeddingConfig) return
    const updated = [...(embeddingConfig.providers || [])]
    updated[index] = { ...updated[index], [field]: val }
    setEmbeddingConfig({ ...embeddingConfig, providers: updated })
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
      models: [...(embeddingConfig.models || []), newModel],
    })
  }

  const handleRemoveEmbeddingModel = (index: number) => {
    if (!embeddingConfig) return
    const updated = [...(embeddingConfig.models || [])]
    updated.splice(index, 1)
    setEmbeddingConfig({ ...embeddingConfig, models: updated })
  }

  const handleUpdateEmbeddingModel = (index: number, field: string, val: any) => {
    if (!embeddingConfig) return
    const updated = [...(embeddingConfig.models || [])]
    updated[index] = { ...updated[index], [field]: val }
    if (field === 'isDefault' && val === true) {
      updated.forEach((m, idx) => {
        if (idx !== index) m.isDefault = false
      })
    }
    setEmbeddingConfig({ ...embeddingConfig, models: updated })
  }

  // ─── LSP MCP CRUD ───────────────────────────────────────────────────────────

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
      servers: [...(lspmcpConfig.servers || []), newServer],
    })
  }

  const handleRemoveLSPServer = (index: number) => {
    if (!lspmcpConfig) return
    const updated = [...lspmcpConfig.servers]
    updated.splice(index, 1)
    setLspmcpConfig({ ...lspmcpConfig, servers: updated })
  }

  const handleUpdateLSPServer = (index: number, field: keyof LSPMCPEntry, val: any) => {
    if (!lspmcpConfig) return
    const updated = [...lspmcpConfig.servers]
    updated[index] = { ...updated[index], [field]: val }
    setLspmcpConfig({ ...lspmcpConfig, servers: updated })
  }

  // ─── LLM Provider Actions ──────────────────────────────────────────────────

  const handleCreateProvider = async (payload: LLMProvider) => {
    await createProvider(payload)
    toast.success(`Provider ${payload.name} created successfully!`)
    loadAll()
  }

  const handleUpdateProviderAction = async (id: string, payload: LLMProvider) => {
    await updateProvider(id, payload)
    toast.success(`Provider ${payload.name} updated successfully!`)
    loadAll()
  }

  const handleDeleteProviderAction = (id: string) => {
    const p = providers.find((p) => p.id === id)
    if (p) setDeleteProviderTarget(p)
  }

  const confirmDeleteProvider = async () => {
    if (!deleteProviderTarget) return
    try {
      await deleteProvider(deleteProviderTarget.id)
      setDeleteProviderTarget(null)
      loadAll()
      toast.success('Provider deleted')
    } catch (err) {
      toast.error((err as Error).message)
      setDeleteProviderTarget(null)
    }
  }

  const handleToggleProviderStatus = async (p: LLMProvider) => {
    try {
      await updateProvider(p.id, { ...p, enabled: !p.enabled })
      loadAll()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  const handleSetProviderAsDefault = async (p: LLMProvider) => {
    try {
      await updateProvider(p.id, { ...p, isDefault: true })
      loadAll()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  // ─── LLM Model Actions ─────────────────────────────────────────────────────

  const handleCreateModel = async (payload: LLMModel) => {
    await createModel(payload)
    toast.success(`Model ${payload.name} created successfully!`)
    loadAll()
  }

  const handleUpdateModelAction = async (id: string, payload: LLMModel) => {
    await updateModel(id, payload)
    toast.success(`Model ${payload.name} updated successfully!`)
    loadAll()
  }

  const handleDeleteModelAction = (id: string) => {
    const m = models.find((m) => m.id === id)
    if (m) setDeleteModelTarget(m)
  }

  const confirmDeleteModel = async () => {
    if (!deleteModelTarget) return
    try {
      await deleteModel(deleteModelTarget.id)
      setDeleteModelTarget(null)
      loadAll()
      toast.success('Model deleted')
    } catch (err) {
      toast.error((err as Error).message)
      setDeleteModelTarget(null)
    }
  }

  const handleToggleModelStatus = async (m: LLMModel) => {
    try {
      await updateModel(m.id, { ...m, enabled: !m.enabled })
      loadAll()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  // ─── Render ─────────────────────────────────────────────────────────────────

  if (loading) {
    return (
      <div className="text-sm font-mono text-muted-foreground p-6">
        Loading LLM configurations...
      </div>
    )
  }

  const subTabButtons: { key: string; label: string }[] = [
    { key: 'llm', label: 'Models & Providers' },
    { key: 'tools', label: 'Tools Settings' },
    { key: 'qqbot', label: 'QQ Bot' },
    { key: 'lspmcp', label: 'LSP MCP' },
    { key: 'embedding', label: 'Embedding' },
    { key: 'session', label: 'Session Settings' },
    { key: 'simulation', label: 'Simulation Settings' },
  ]

  return (
    <div className="space-y-6">
      <Tabs value={activeTab} onValueChange={handleTabChange}>
        <TabsList className="border-b border-border w-full justify-start gap-0 bg-transparent p-0">
          <TabsTrigger
            value="db"
            className="flex items-center gap-2 px-5 py-3 text-sm font-semibold border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-transparent"
          >
            <Database className="h-4 w-4" />
            Database Settings
          </TabsTrigger>
          <TabsTrigger
            value="toml"
            className="flex items-center gap-2 px-5 py-3 text-sm font-semibold border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-transparent"
          >
            <FileText className="h-4 w-4" />
            settings.toml File (ReadOnly)
          </TabsTrigger>
        </TabsList>

        <TabsContent value="toml" className="space-y-4">
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
        </TabsContent>

        <TabsContent value="db" className="space-y-6 pb-10">
          {/* Sub-tab Switcher */}
          <div className="flex flex-wrap gap-2 p-1 bg-muted rounded-lg w-full mb-6">
            {subTabButtons.map((btn) => (
              <button
                key={btn.key}
                type="button"
                onClick={() => handleSubTabChange(btn.key)}
                className={`px-3 py-1.5 text-xs font-semibold rounded-md transition-all ${
                  subTab === btn.key
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                {btn.label}
              </button>
            ))}
          </div>

          {/* Sub-tab Content */}
          <div className="space-y-6">
            {subTab === 'llm' && (
              <LLMSection
                providers={providers}
                models={models}
                defaultModels={defaultModels}
                onSaveDefaults={handleSaveDefaults}
                onDefaultModelsChange={setDefaultModels}
                onCreateProvider={handleCreateProvider}
                onUpdateProvider={handleUpdateProviderAction}
                onDeleteProvider={handleDeleteProviderAction}
                onToggleProviderStatus={handleToggleProviderStatus}
                onSetProviderAsDefault={handleSetProviderAsDefault}
                onCreateModel={handleCreateModel}
                onUpdateModel={handleUpdateModelAction}
                onDeleteModel={handleDeleteModelAction}
                onToggleModelStatus={handleToggleModelStatus}
              />
            )}

            {subTab === 'tools' && toolsConfig && (
              <ToolsSection
                config={toolsConfig}
                onChange={setToolsConfig}
                onSave={handleSaveTools}
              />
            )}

            {subTab === 'qqbot' && qqbotConfig && (
              <QQBotSection
                config={qqbotConfig}
                onChange={setQqbotConfig}
                onSave={handleSaveQQBot}
              />
            )}

            {subTab === 'lspmcp' && lspmcpConfig && (
              <LSPMCPSection
                config={lspmcpConfig}
                onSave={handleSaveLSPMCP}
                onAddServer={handleAddLSPServer}
                onRemoveServer={handleRemoveLSPServer}
                onUpdateServer={handleUpdateLSPServer}
              />
            )}

            {subTab === 'embedding' && embeddingConfig && (
              <EmbeddingSection
                config={embeddingConfig}
                onChange={setEmbeddingConfig}
                onSave={handleSaveEmbedding}
                onAddProvider={handleAddEmbeddingProvider}
                onRemoveProvider={handleRemoveEmbeddingProvider}
                onUpdateProvider={handleUpdateEmbeddingProvider}
                onAddModel={handleAddEmbeddingModel}
                onRemoveModel={handleRemoveEmbeddingModel}
                onUpdateModel={handleUpdateEmbeddingModel}
              />
            )}

            {subTab === 'session' && sessionConfig && (
              <SessionSection
                config={sessionConfig}
                onChange={setSessionConfig}
                onSave={handleSaveSession}
              />
            )}

            {subTab === 'simulation' && simulationConfig && (
              <SimulationSection
                config={simulationConfig}
                onChange={setSimulationConfig}
                onSave={handleSaveSimulation}
                providers={providers}
                models={models}
              />
            )}
          </div>
        </TabsContent>
      </Tabs>

      <ConfirmDialog
        open={!!deleteProviderTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteProviderTarget(null)
        }}
        title="Delete Provider"
        message={`Deleting provider "${deleteProviderTarget?.id}" will also remove all associated models. This action cannot be undone.`}
        destructive
        onConfirm={confirmDeleteProvider}
        confirmLabel="Delete Provider"
      />
      <ConfirmDialog
        open={!!deleteModelTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteModelTarget(null)
        }}
        title="Delete Model"
        message={`Delete model "${deleteModelTarget?.id}"? This action cannot be undone.`}
        destructive
        onConfirm={confirmDeleteModel}
        confirmLabel="Delete Model"
      />
    </div>
  )
}
