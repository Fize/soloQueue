import { Database } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import type { QQBotConfig } from '@/types'

interface QQBotSectionProps {
  config: QQBotConfig
  onChange: (config: QQBotConfig) => void
  onSave: () => void
}

export function QQBotSection({ config, onChange, onSave }: QQBotSectionProps) {
  return (
    <div className="rounded-xl border bg-card p-6 shadow-sm space-y-6">
      <div className="flex items-center justify-between border-b pb-3">
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-2">
            <Database className="h-4 w-4 text-primary" />
            <h3 className="font-semibold text-foreground">QQ Bot WebSocket Config</h3>
          </div>
          <p className="text-xs text-muted-foreground mt-0.5 leading-relaxed">
            QQ Bot WebSocket Config: Connect to the QQ Open Platform WebSocket Gateway to allow
            agents to interact in QQ chats and run scheduled tasks.
          </p>
        </div>
        <Button size="sm" onClick={onSave}>
          Save QQ Bot Settings
        </Button>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground">App ID</label>
          <Input
            type="text"
            placeholder="Enter AppID"
            value={config.appId || ''}
            onChange={(e) => onChange({ ...config, appId: e.target.value })}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground">App Secret</label>
          <Input
            type="password"
            placeholder="Enter AppSecret"
            value={config.appSecret || ''}
            onChange={(e) => onChange({ ...config, appSecret: e.target.value })}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground">Intents Mask</label>
          <Input
            type="number"
            value={config.intents || 0}
            onChange={(e) => onChange({ ...config, intents: Number(e.target.value) })}
          />
        </div>
        <div className="flex items-center gap-6 pt-4 flex-wrap">
          <div className="flex items-center gap-2">
            <Switch
              checked={config.enabled || false}
              onCheckedChange={(val) => onChange({ ...config, enabled: val })}
            />
            <span className="text-xs font-semibold text-foreground">Enabled</span>
          </div>
        </div>
      </div>
    </div>
  )
}
