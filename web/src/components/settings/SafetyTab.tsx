import { useState, useEffect } from 'react'
import {
  getToolsConfig,
  updateToolsConfig,
  getSessionConfig,
  updateSessionConfig,
} from '@/lib/api'
import type { ToolsConfig, SessionConfig } from '@/types'
import { toast } from 'sonner'
import { useTranslation } from '@/lib/i18n'
import { ToolsSection } from './ConfigTab/ToolsSection'
import { SessionSection } from './ConfigTab/SessionSection'

export function SafetyTab() {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(true)
  const [toolsConfig, setToolsConfig] = useState<ToolsConfig | null>(null)
  const [sessionConfig, setSessionConfig] = useState<SessionConfig | null>(null)

  const loadData = async () => {
    setLoading(true)
    try {
      const [dbTools, dbSession] = await Promise.all([
        getToolsConfig(),
        getSessionConfig(),
      ])
      setToolsConfig(dbTools)
      setSessionConfig(dbSession)
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    const loadTimer = window.setTimeout(() => {
      void loadData()
    }, 0)
    return () => window.clearTimeout(loadTimer)
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
    </div>
  )
}

export default SafetyTab
