import { useState } from 'react'
import { useConfig } from '@/hooks/useConfig'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Select } from '@/components/ui/select'
import { Button } from '@/components/ui/button'
import { Save } from 'lucide-react'
import type { SessionConfig, LogConfig } from '@/types'

const LOG_LEVELS = [
  { value: 'debug', label: 'Debug' },
  { value: 'info', label: 'Info' },
  { value: 'warn', label: 'Warn' },
  { value: 'error', label: 'Error' },
]

export function GeneralTab() {
  const { config, patch, saving, error } = useConfig()

  const [session, setSession] = useState<SessionConfig | null>(null)
  const [log, setLog] = useState<LogConfig | null>(null)

  // Initialize local state from remote config
  if (config && !session) {
    setSession({ ...config.session })
    setLog({ ...config.log })
  }

  if (!config || !session || !log) {
    return <div className="text-sm text-muted-foreground">Loading configuration...</div>
  }

  const handleSaveSession = async () => {
    await patch({ session })
  }

  const handleSaveLog = async () => {
    await patch({ log })
  }

  return (
    <div className="space-y-6">
      {/* Session Config */}
      <div className="nb-border rounded-lg bg-card p-5 nb-shadow-sm">
        <h3 className="text-sm font-bold text-foreground mb-4">Session</h3>
        <div className="grid gap-4 text-sm">
          <div className="grid grid-cols-[1fr_auto] items-center gap-3">
            <div className="flex flex-col gap-1">
              <Label htmlFor="timelineMaxFileMB">Timeline Max File Size</Label>
              <span className="text-[10px] text-muted-foreground">
                Single file limit for timeline logs, capped at 50 MB
              </span>
            </div>
            <div className="flex items-center gap-1">
              <Input
                id="timelineMaxFileMB"
                type="number"
                min={1}
                max={50}
                className="w-24 text-right"
                value={session.timelineMaxFileMB}
                onChange={(e) =>
                  setSession({
                    ...session,
                    timelineMaxFileMB: Math.min(Number(e.target.value), 50),
                  })
                }
              />
              <span className="text-xs text-muted-foreground">MB</span>
            </div>
          </div>
          <div className="grid grid-cols-[1fr_auto] items-center gap-3">
            <div className="flex flex-col gap-1">
              <Label>Timeline Retention</Label>
              <span className="text-[10px] text-muted-foreground">
                Timeline logs are retained for up to 15 days
              </span>
            </div>
            <span className="text-xs font-medium text-muted-foreground">15 days</span>
          </div>
          <div className="grid grid-cols-[1fr_auto] items-center gap-3">
            <div className="flex flex-col gap-1">
              <Label htmlFor="contextIdleThresholdMin">Context Idle Threshold</Label>
              <span className="text-[10px] text-muted-foreground">
                Auto-clear idle context after this period
              </span>
            </div>
            <div className="flex items-center gap-1">
              <Input
                id="contextIdleThresholdMin"
                type="number"
                min={1}
                className="w-24 text-right"
                value={session.contextIdleThresholdMin}
                onChange={(e) =>
                  setSession({ ...session, contextIdleThresholdMin: Number(e.target.value) })
                }
              />
              <span className="text-xs text-muted-foreground">min</span>
            </div>
          </div>
        </div>
        <div className="mt-4 flex items-center gap-3 border-t-2 border-[#EEEEEE] pt-3">
          <Button size="sm" onClick={handleSaveSession} disabled={saving}>
            <Save className="mr-1 h-3 w-3" />
            {saving ? 'Saving...' : 'Save Session'}
          </Button>
          {error && <span className="text-[10px] text-destructive">{error}</span>}
        </div>
      </div>

      {/* Log Config */}
      <div className="nb-border rounded-lg bg-card p-5 nb-shadow-sm">
        <h3 className="text-sm font-bold text-foreground mb-4">Logging</h3>
        <div className="grid gap-4 text-sm">
          <div className="grid grid-cols-[1fr_auto] items-center gap-3">
            <Label htmlFor="logLevel">Log Level</Label>
            <Select
              id="logLevel"
              options={LOG_LEVELS}
              value={log.level}
              onChange={(v) => setLog({ ...log, level: v })}
              className="w-32"
            />
          </div>
          <div className="grid grid-cols-[1fr_auto] items-center gap-3">
            <div className="flex flex-col gap-1">
              <Label>Console Output</Label>
              <span className="text-[10px] text-muted-foreground">Print logs to console</span>
            </div>
            <Switch checked={log.console} onCheckedChange={(v) => setLog({ ...log, console: v })} />
          </div>
          <div className="grid grid-cols-[1fr_auto] items-center gap-3">
            <div className="flex flex-col gap-1">
              <Label>File Output</Label>
              <span className="text-[10px] text-muted-foreground">Write logs to file</span>
            </div>
            <Switch checked={log.file} onCheckedChange={(v) => setLog({ ...log, file: v })} />
          </div>
        </div>
        <div className="mt-4 flex items-center gap-3 border-t-2 border-[#EEEEEE] pt-3">
          <Button size="sm" onClick={handleSaveLog} disabled={saving}>
            <Save className="mr-1 h-3 w-3" />
            {saving ? 'Saving...' : 'Save Logging'}
          </Button>
        </div>
      </div>
    </div>
  )
}
