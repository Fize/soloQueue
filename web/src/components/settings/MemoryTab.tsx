import { useState, useEffect } from 'react'
import {
  getEmbeddingConfig,
  updateEmbeddingConfig,
  getLSPMCPConfig,
  updateLSPMCPConfig,
} from '@/lib/api'
import type { EmbeddingConfig, LSPMCPConfig, LSPMCPEntry } from '@/types'
import { toast } from 'sonner'
import { useTranslation } from '@/lib/i18n'
import { EmbeddingSection } from './ConfigTab/EmbeddingSection'
import { LSPMCPSection } from './ConfigTab/LSPMCPSection'

export function MemoryTab() {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(true)
  const [embeddingConfig, setEmbeddingConfig] = useState<EmbeddingConfig | null>(null)
  const [lspmcpConfig, setLspmcpConfig] = useState<LSPMCPConfig | null>(null)

  const loadData = async () => {
    setLoading(true)
    try {
      const [dbEmbedding, dbLspmcp] = await Promise.all([
        getEmbeddingConfig(),
        getLSPMCPConfig(),
      ])
      setEmbeddingConfig(dbEmbedding)
      setLspmcpConfig(dbLspmcp)
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadData()
  }, [])

  // ─── Embedding CRUD ─────────────────────────────────────────────────────────

  const handleSaveEmbedding = async () => {
    if (!embeddingConfig) return
    try {
      await updateEmbeddingConfig(embeddingConfig)
      toast.success(t('config.toastEmbeddingUpdated'))
      loadData()
    } catch (err) {
      toast.error((err as Error).message)
    }
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

  const handleSaveLSPMCP = async () => {
    if (!lspmcpConfig) return
    try {
      await updateLSPMCPConfig(lspmcpConfig)
      toast.success(t('config.toastLSPMCPUpdated'))
      loadData()
    } catch (err) {
      toast.error((err as Error).message)
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

  if (loading) {
    return (
      <div className="text-sm font-mono text-muted-foreground p-6">
        {t('common.loading')}
      </div>
    )
  }

  return (
    <div className="space-y-8 pb-10">
      {embeddingConfig && (
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

      {lspmcpConfig && (
        <LSPMCPSection
          config={lspmcpConfig}
          onSave={handleSaveLSPMCP}
          onAddServer={handleAddLSPServer}
          onRemoveServer={handleRemoveLSPServer}
          onUpdateServer={handleUpdateLSPServer}
        />
      )}
    </div>
  )
}

export default MemoryTab
