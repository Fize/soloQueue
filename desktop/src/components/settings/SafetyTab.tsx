import { useState, useEffect } from 'react'
import {
  getToolsConfig,
  updateToolsConfig,
  getSessionConfig,
  updateSessionConfig,
  getSimulationConfig,
  updateSimulationConfig,
  listProviders,
  listModels,
} from '@/lib/api'
import type { ToolsConfig, SessionConfig, SimulationConfig, LLMProvider, LLMModel } from '@/types'
import { toast } from 'sonner'
import { useTranslation } from '@/lib/i18n'
import { ToolsSection } from './ConfigTab/ToolsSection'
import { SessionSection } from './ConfigTab/SessionSection'
import { SimulationSection } from './ConfigTab/SimulationSection'

export function SafetyTab() {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(true)
  const [toolsConfig, setToolsConfig] = useState<ToolsConfig | null>(null)
  const [sessionConfig, setSessionConfig] = useState<SessionConfig | null>(null)
  const [simulationConfig, setSimulationConfig] = useState<SimulationConfig | null>(null)
  const [providers, setProviders] = useState<LLMProvider[]>([])
  const [models, setModels] = useState<LLMModel[]>([])

  const loadData = async () => {
    setLoading(true)
    try {
      const [dbTools, dbSession, dbSimulation, dbProviders, dbModels] = await Promise.all([
        getToolsConfig(),
        getSessionConfig(),
        getSimulationConfig(),
        listProviders(),
        listModels(),
      ])
      setToolsConfig(dbTools)
      setSessionConfig(dbSession)
      setSimulationConfig(dbSimulation)
      setProviders(dbProviders || [])
      setModels(dbModels || [])
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadData()
  }, [])

  const handleSaveTools = async () => {
    if (!toolsConfig) return
    try {
      await updateToolsConfig(toolsConfig)
      toast.success(t('config.toastToolsUpdated'))
      loadData()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  const handleSaveSession = async () => {
    if (!sessionConfig) return
    try {
      await updateSessionConfig(sessionConfig)
      toast.success(t('config.toastSessionUpdated'))
      loadData()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  const handleSaveSimulation = async () => {
    if (!simulationConfig) return
    try {
      await updateSimulationConfig(simulationConfig)
      toast.success(t('config.toastSimulationUpdated'))
      loadData()
    } catch (err) {
      toast.error((err as Error).message)
    }
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
      {toolsConfig && (
        <ToolsSection
          config={toolsConfig}
          onChange={setToolsConfig}
          onSave={handleSaveTools}
        />
      )}

      {sessionConfig && (
        <SessionSection
          config={sessionConfig}
          onChange={setSessionConfig}
          onSave={handleSaveSession}
        />
      )}

      {simulationConfig && (
        <SimulationSection
          config={simulationConfig}
          onChange={setSimulationConfig}
          onSave={handleSaveSimulation}
          providers={providers}
          models={models}
        />
      )}
    </div>
  )
}

export default SafetyTab
