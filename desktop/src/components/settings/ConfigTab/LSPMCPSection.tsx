import { Database, Plus, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import type { LSPMCPConfig, LSPMCPEntry } from '@/types'

interface LSPMCPSectionProps {
  config: LSPMCPConfig
  onSave: () => void
  onAddServer: () => void
  onRemoveServer: (index: number) => void
  onUpdateServer: (index: number, field: keyof LSPMCPEntry, val: any) => void
}

export function LSPMCPSection({
  config,
  onSave,
  onAddServer,
  onRemoveServer,
  onUpdateServer,
}: LSPMCPSectionProps) {
  return (
    <div className="rounded-xl border bg-card p-6 shadow-sm space-y-6">
      <div className="flex items-center justify-between border-b pb-3">
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-2">
            <Database className="h-4 w-4 text-primary" />
            <h3 className="font-semibold text-foreground">LSP MCP Config</h3>
          </div>
          <p className="text-xs text-muted-foreground mt-0.5 leading-relaxed">
            LSP MCP Config: Configure LSP (Language Server Protocol) tool integrations that provide
            agents with intelligent code navigation and analysis capabilities.
          </p>
        </div>
        <Button size="sm" onClick={onSave}>
          Save LSP MCP Settings
        </Button>
      </div>

      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <h4 className="text-sm font-semibold text-foreground">Custom LSP Servers</h4>
          <Button size="xs" variant="outline" onClick={onAddServer}>
            <Plus className="h-3 w-3 mr-1" /> Add Server
          </Button>
        </div>

        <div className="space-y-3">
          {(config.servers || []).map((srv: LSPMCPEntry, idx: number) => (
            <div key={srv.id || idx} className="p-4 border rounded-lg relative space-y-4">
              <button
                type="button"
                onClick={() => onRemoveServer(idx)}
                className="absolute top-4 right-4 text-muted-foreground hover:text-destructive transition-colors"
              >
                <Trash2 className="h-4 w-4" />
              </button>
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
                <div className="flex flex-col gap-1.5">
                  <label className="text-xs font-semibold text-muted-foreground font-mono">
                    Server ID
                  </label>
                  <Input
                    type="text"
                    placeholder="e.g. my-lsp"
                    value={srv.id || ''}
                    onChange={(e) => onUpdateServer(idx, 'id', e.target.value)}
                  />
                </div>
                <div className="flex flex-col gap-1.5">
                  <label className="text-xs font-semibold text-muted-foreground">Command</label>
                  <Input
                    type="text"
                    placeholder="e.g. /usr/bin/gopls"
                    value={srv.command || ''}
                    onChange={(e) => onUpdateServer(idx, 'command', e.target.value)}
                  />
                </div>
                <div className="flex flex-col gap-1.5">
                  <label className="text-xs font-semibold text-muted-foreground">
                    Args (comma separated)
                  </label>
                  <Input
                    type="text"
                    placeholder="e.g. -mode=stdio"
                    value={(srv.args || []).join(', ')}
                    onChange={(e) =>
                      onUpdateServer(
                        idx,
                        'args',
                        e.target.value
                          .split(',')
                          .map((s) => s.trim())
                          .filter(Boolean)
                      )
                    }
                  />
                </div>
                <div className="flex flex-col gap-1.5">
                  <label className="text-xs font-semibold text-muted-foreground">
                    Languages (comma separated)
                  </label>
                  <Input
                    type="text"
                    placeholder="e.g. go"
                    value={(srv.languages || []).join(', ')}
                    onChange={(e) =>
                      onUpdateServer(
                        idx,
                        'languages',
                        e.target.value
                          .split(',')
                          .map((s) => s.trim())
                          .filter(Boolean)
                      )
                    }
                  />
                </div>
                <div className="flex flex-col gap-1.5">
                  <label className="text-xs font-semibold text-muted-foreground">
                    Extensions (comma separated)
                  </label>
                  <Input
                    type="text"
                    placeholder=".go"
                    value={(srv.extensions || []).join(', ')}
                    onChange={(e) =>
                      onUpdateServer(
                        idx,
                        'extensions',
                        e.target.value
                          .split(',')
                          .map((s) => s.trim())
                          .filter(Boolean)
                      )
                    }
                  />
                </div>
                <div className="flex items-center gap-2 pt-6">
                  <Switch
                    checked={!srv.disabled}
                    onCheckedChange={(val: boolean) => onUpdateServer(idx, 'disabled', !val)}
                  />
                  <span className="text-xs font-semibold text-foreground">Enabled</span>
                </div>
              </div>
            </div>
          ))}

          {(config.servers || []).length === 0 && (
            <div className="text-center p-6 border border-dashed rounded-xl text-muted-foreground text-sm">
              No custom LSP servers defined. Using default built-in servers.
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
