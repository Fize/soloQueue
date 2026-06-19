import { Database } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { SessionConfig } from '@/types'

interface SessionSectionProps {
  config: SessionConfig
  onChange: (config: SessionConfig) => void
  onSave: () => void
}

export function SessionSection({ config, onChange, onSave }: SessionSectionProps) {
  return (
    <div className="rounded-xl border bg-card p-6 shadow-sm space-y-6">
      <div className="flex items-center justify-between border-b pb-3">
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-2">
            <Database className="h-4 w-4 text-primary" />
            <h3 className="font-semibold text-foreground">Session / Timeline Config</h3>
          </div>
          <p className="text-xs text-muted-foreground mt-0.5 leading-relaxed">
            Session / Timeline Config: Configure session timeline log parameters, including the
            maximum log file size before rotation.
          </p>
        </div>
        <Button size="sm" onClick={onSave}>
          Save Session Settings
        </Button>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground">
            Timeline Max File Size (MB)
          </label>
          <Input
            type="number"
            value={config.timelineMaxFileMB || 50}
            onChange={(e) => onChange({ ...config, timelineMaxFileMB: Number(e.target.value) })}
          />
        </div>
      </div>
    </div>
  )
}
