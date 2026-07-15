import { useState, useEffect } from 'react'
import {
  getQQBotsConfig,
  updateQQBotsConfig,
} from '@/lib/api'
import type { QQBotConfig } from '@/types'
import { toast } from 'sonner'
import { useTranslation } from '@/lib/i18n'
import { QQBotSection } from './ConfigTab/QQBotSection'

export function QQBotTab() {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(true)
  const [qqbotsConfig, setQqbotsConfig] = useState<QQBotConfig[]>([])

  const loadData = async () => {
    setLoading(true)
    try {
      const dbQqbots = await getQQBotsConfig()
      setQqbotsConfig(dbQqbots || [])
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadData()
  }, [])

  const handleSaveQQBots = async () => {
    if (!qqbotsConfig) return
    try {
      await updateQQBotsConfig(qqbotsConfig)
      toast.success(t('config.toastQQBotsUpdated'))
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
    <div className="space-y-6 pb-10">
      <QQBotSection
        config={qqbotsConfig}
        onChange={setQqbotsConfig}
        onSave={handleSaveQQBots}
      />
    </div>
  )
}

export default QQBotTab
