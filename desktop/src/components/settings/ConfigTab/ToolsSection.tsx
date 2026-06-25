import { Shield } from 'lucide-react'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Button } from '@/components/ui/button'
import type { ToolsConfig } from '@/types'

interface ToolsSectionProps {
  config: ToolsConfig
  onChange: (config: ToolsConfig) => void
  onSave: () => void
}

export function ToolsSection({ config, onChange, onSave }: ToolsSectionProps) {
  return (
    <div className="rounded-xl border bg-card p-6 shadow-sm space-y-8">
      <div className="flex items-center justify-between border-b pb-3">
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-2">
            <Shield className="h-4 w-4 text-primary" />
            <h3 className="font-semibold text-foreground">Tools Settings</h3>
          </div>
          <p className="text-xs text-muted-foreground mt-0.5 leading-relaxed">
            Tools Settings: Configure tool permissions, resource limits, and security boundaries for
            code execution, file operations, and shell commands.
          </p>
        </div>
        <Button size="sm" onClick={onSave}>
          Save Tools Settings
        </Button>
      </div>

      {/* Grep/Search Limits */}
      <div>
        <h4 className="text-sm font-semibold text-foreground border-b pb-1 mb-3">
          Grep & Search Limits
        </h4>
        <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-4">
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">Max Matches</label>
            <Input
              type="number"
              value={config.maxMatches}
              onChange={(e) => onChange({ ...config, maxMatches: Number(e.target.value) })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">Max Line Length</label>
            <Input
              type="number"
              value={config.maxLineLen}
              onChange={(e) => onChange({ ...config, maxLineLen: Number(e.target.value) })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">
              Max Glob List Items
            </label>
            <Input
              type="number"
              value={config.maxGlobItems}
              onChange={(e) => onChange({ ...config, maxGlobItems: Number(e.target.value) })}
            />
          </div>
        </div>
      </div>

      {/* File Write Limits */}
      <div>
        <h4 className="text-sm font-semibold text-foreground border-b pb-1 mb-3">
          File Write Limits
        </h4>
        <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-4 gap-4">
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">
              Max Write Size (Bytes)
            </label>
            <Input
              type="number"
              value={config.maxWriteSize}
              onChange={(e) => onChange({ ...config, maxWriteSize: Number(e.target.value) })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">
              Max Multi-Write (Bytes)
            </label>
            <Input
              type="number"
              value={config.maxMultiWriteBytes}
              onChange={(e) => onChange({ ...config, maxMultiWriteBytes: Number(e.target.value) })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">
              Max Multi-Write Files
            </label>
            <Input
              type="number"
              value={config.maxMultiWriteFiles}
              onChange={(e) => onChange({ ...config, maxMultiWriteFiles: Number(e.target.value) })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">Max Replace Edits</label>
            <Input
              type="number"
              value={config.maxReplaceEdits}
              onChange={(e) => onChange({ ...config, maxReplaceEdits: Number(e.target.value) })}
            />
          </div>
        </div>
      </div>

      {/* Web Search & Fetch */}
      <div>
        <h4 className="text-sm font-semibold text-foreground border-b pb-1 mb-3">
          Web Search & Fetch
        </h4>
        <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-4">
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">
              HTTP Max Body (Bytes)
            </label>
            <Input
              type="number"
              value={config.httpMaxBody}
              onChange={(e) => onChange({ ...config, httpMaxBody: Number(e.target.value) })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">HTTP Timeout (ms)</label>
            <Input
              type="number"
              value={config.httpTimeoutMs}
              onChange={(e) => onChange({ ...config, httpTimeoutMs: Number(e.target.value) })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">
              Web Search Timeout (ms)
            </label>
            <Input
              type="number"
              value={config.webSearchTimeoutMs}
              onChange={(e) => onChange({ ...config, webSearchTimeoutMs: Number(e.target.value) })}
            />
          </div>
          <div className="flex items-center gap-2 pt-4">
            <Switch
              checked={config.httpBlockPrivate}
              onCheckedChange={(val) => onChange({ ...config, httpBlockPrivate: val })}
            />
            <span className="text-xs font-semibold text-foreground">Block Private Networks</span>
          </div>
        </div>
        <div className="mt-4 flex flex-col gap-1.5">
          <label className="text-xs font-semibold text-muted-foreground">
            Allowed HTTP Hosts (comma separated)
          </label>
          <Input
            type="text"
            placeholder="e.g. api.github.com, google.com"
            value={config.httpAllowedHosts?.join(', ') || ''}
            onChange={(e) =>
              onChange({
                ...config,
                httpAllowedHosts: e.target.value
                  .split(',')
                  .map((s) => s.trim())
                  .filter(Boolean),
              })
            }
          />
        </div>
      </div>

      {/* Shell Execute / Bash */}
      <div>
        <h4 className="text-sm font-semibold text-foreground border-b pb-1 mb-3">
          Shell Execute / Bash
        </h4>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">
              Shell Max Output Captured (Bytes)
            </label>
            <Input
              type="number"
              value={config.shellMaxOutput}
              onChange={(e) => onChange({ ...config, shellMaxOutput: Number(e.target.value) })}
            />
          </div>
        </div>
        <div className="grid grid-cols-1 gap-4 mt-4">
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">
              Block Command Regexes (comma separated)
            </label>
            <Input
              type="text"
              placeholder="e.g. ^\s*sudo\b, ^\s*poweroff\b"
              value={config.shellBlockRegexes?.join(', ') || ''}
              onChange={(e) =>
                onChange({
                  ...config,
                  shellBlockRegexes: e.target.value
                    .split(',')
                    .map((s) => s.trim())
                    .filter(Boolean),
                })
              }
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-semibold text-muted-foreground">
              Confirm Command Regexes (comma separated)
            </label>
            <Input
              type="text"
              placeholder="e.g. ^\s*rm\b, ^\s*dd\b"
              value={config.shellConfirmRegexes?.join(', ') || ''}
              onChange={(e) =>
                onChange({
                  ...config,
                  shellConfirmRegexes: e.target.value
                    .split(',')
                    .map((s) => s.trim())
                    .filter(Boolean),
                })
              }
            />
          </div>
        </div>
      </div>
    </div>
  )
}
