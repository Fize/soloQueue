import { MessageSquare, Plus, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Select } from '@/components/ui/select'
import { useTranslation } from '@/lib/i18n'
import type { QQBotConfig } from '@/types'
import { useState, useEffect } from 'react'
import { listL2Groups } from '@/lib/api'

interface QQBotSectionProps {
  config: QQBotConfig[]
  onChange: (config: QQBotConfig[]) => void
  onSave: () => void
}

export function QQBotSection({ config, onChange, onSave }: QQBotSectionProps) {
  const { t } = useTranslation()
  const [bots, setBots] = useState<QQBotConfig[]>(config || [])
  const [l2Groups, setL2Groups] = useState<string[]>([])

  useEffect(() => {
    listL2Groups().then(setL2Groups).catch(console.error)
  }, [])

  const handleAdd = () => {
    const newBot: QQBotConfig = {
      id: crypto.randomUUID().slice(0, 8),
      name: 'New Bot',
      enabled: false,
      appId: '',
      appSecret: '',
      intents: 0,
      sandbox: true,
      bind_type: 'l1',
      bind_agent: '',
      whitelist_enabled: false,
      whitelist: []
    }
    const newBots = [...bots, newBot]
    setBots(newBots)
    onChange(newBots)
  }

  const handleRemove = (index: number) => {
    const newBots = [...bots]
    newBots.splice(index, 1)
    setBots(newBots)
    onChange(newBots)
  }

  const handleChange = (index: number, updated: Partial<QQBotConfig>) => {
    const newBots = [...bots]
    newBots[index] = { ...newBots[index], ...updated }
    setBots(newBots)
    onChange(newBots)
  }

  return (
    <div className="rounded-xl border bg-card p-6 shadow-sm space-y-6">
      <div className="flex items-center justify-between border-b pb-3">
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-2">
            <MessageSquare className="h-4 w-4 text-primary" />
            <h3 className="font-semibold text-foreground">{t('config.qqTitle')}</h3>
          </div>
          <p className="text-xs text-muted-foreground mt-0.5 leading-relaxed">
            {t('config.qqTitleDesc')}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button size="sm" variant="outline" onClick={handleAdd}>
            <Plus className="h-4 w-4 mr-1" /> {t('config.qqAddBot')}
          </Button>
          <Button size="sm" onClick={onSave}>
            {t('config.qqSave')}
          </Button>
        </div>
      </div>

      <div className="space-y-6">
        {bots.length === 0 && (
          <div className="text-center py-6 text-sm text-muted-foreground">
            {t('config.qqNoBots')}
          </div>
        )}
        
        {bots.map((bot, idx) => (
          <div key={idx} className="border rounded-md p-4 space-y-4 relative bg-muted/20">
            <Button
              size="icon"
              variant="ghost"
              className="absolute top-2 right-2 h-8 w-8 text-destructive hover:text-destructive hover:bg-destructive/10"
              onClick={() => handleRemove(idx)}
            >
              <Trash2 className="h-4 w-4" />
            </Button>
            
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 mr-8">
              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-semibold text-muted-foreground">{t('config.qqBotName')}</label>
                <Input
                  type="text"
                  placeholder={t('config.qqBotNamePlaceholder')}
                  value={bot.name || ''}
                  onChange={(e) => handleChange(idx, { name: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-semibold text-muted-foreground">{t('config.qqAppId')}</label>
                <Input
                  type="text"
                  placeholder={t('config.qqAppIdPlaceholder')}
                  value={bot.appId || ''}
                  onChange={(e) => handleChange(idx, { appId: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-semibold text-muted-foreground">{t('config.qqAppSecret')}</label>
                <Input
                  type="password"
                  placeholder={t('config.qqAppSecretPlaceholder')}
                  value={bot.appSecret || ''}
                  onChange={(e) => handleChange(idx, { appSecret: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-semibold text-muted-foreground">{t('config.qqIntentsMask')}</label>
                <Input
                  type="number"
                  value={bot.intents || 0}
                  onChange={(e) => handleChange(idx, { intents: Number(e.target.value) })}
                />
              </div>
              
              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-semibold text-muted-foreground">{t('config.qqBindType')}</label>
                <Select
                  value={bot.bind_type || 'l1'}
                  onChange={(val) => handleChange(idx, { bind_type: val })}
                  options={[
                    { value: 'l1', label: t('config.qqBindL1') },
                    { value: 'l2', label: t('config.qqBindL2') }
                  ]}
                />
              </div>
              
              {bot.bind_type === 'l2' && (
                <div className="flex flex-col gap-1.5">
                  <label className="text-xs font-semibold text-muted-foreground">{t('config.qqTargetL2Agent')}</label>
                  <Select
                    placeholder={t('config.qqSelectAgent')}
                    value={bot.bind_agent || null}
                    onChange={(val) => handleChange(idx, { bind_agent: val })}
                    options={l2Groups.map((group) => ({
                      value: group,
                      label: group
                    }))}
                  />
                </div>
              )}
            </div>
            
            <div className="flex items-center gap-6 pt-2 flex-wrap">
              <div className="flex items-center gap-2">
                <Switch
                  checked={bot.enabled || false}
                  onCheckedChange={(val) => handleChange(idx, { enabled: val })}
                />
                <span className="text-xs font-semibold text-foreground">{t('common.enabled')}</span>
              </div>
              <div className="flex items-center gap-2">
                <Switch
                  checked={bot.sandbox || false}
                  onCheckedChange={(val) => handleChange(idx, { sandbox: val })}
                />
                <span className="text-xs font-semibold text-foreground">{t('config.qqSandboxMode')}</span>
              </div>
              <div className="flex items-center gap-2">
                <Switch
                  checked={bot.whitelist_enabled || false}
                  onCheckedChange={(val) => handleChange(idx, { whitelist_enabled: val })}
                />
                <span className="text-xs font-semibold text-foreground">{t('config.qqWhitelistMode')}</span>
              </div>
            </div>

            {bot.whitelist_enabled && (
              <div className="flex flex-col gap-1.5 pt-1">
                <label className="text-xs font-semibold text-muted-foreground">
                  {t('config.qqWhitelistDesc')}
                </label>
                <textarea
                  className="flex min-h-[80px] w-full rounded-md border border-input bg-background px-3 py-2 text-xs font-mono ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
                  placeholder={t('config.qqWhitelistPlaceholder')}
                  value={bot.whitelist?.join('\n') || ''}
                  onChange={(e) => {
                    const list = e.target.value.split('\n').map(x => x.trim()).filter(Boolean);
                    handleChange(idx, { whitelist: list });
                  }}
                />
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}
