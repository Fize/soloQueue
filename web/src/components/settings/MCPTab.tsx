import { useState, useEffect } from 'react'
import { useMCPConfigStore } from '@/stores/mcpConfigStore'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Button } from '@/components/ui/button'
import { Save, Plus, Trash2, ChevronDown, ChevronRight } from 'lucide-react'
import type { MCPServerConfig, MCPConfig } from '@/types'

function emptyServer(): MCPServerConfig {
  return { name: '', command: '', args: [], transport: 'stdio', enabled: true }
}

function fromWire(cfg: MCPConfig): MCPServerConfig[] {
  return Object.entries(cfg.mcpServers ?? {}).map(([name, s]) => ({
    name,
    command: s.command ?? '',
    args: s.args ?? [],
    env: s.env,
    transport: s.transport ?? 'stdio',
    enabled: s.enabled ?? true,
  }))
}

function toWire(servers: MCPServerConfig[]): MCPConfig {
  const mcpServers: MCPConfig['mcpServers'] = {}
  for (const s of servers) {
    const { name, ...rest } = s
    mcpServers[name] = rest
  }
  return { mcpServers }
}

export function MCPTab() {
  const config = useMCPConfigStore((state) => state.config)
  const fetchConfig = useMCPConfigStore((state) => state.fetch)
  const save = useMCPConfigStore((state) => state.save)
  const saving = useMCPConfigStore((state) => state.saving)
  const error = useMCPConfigStore((state) => state.error)
  const [local, setLocal] = useState<MCPServerConfig[] | null>(null)
  const [expanded, setExpanded] = useState<Record<number, boolean>>({})

  useEffect(() => {
    fetchConfig()
  }, [fetchConfig])

  useEffect(() => {
    if (config && !local) {
      setLocal(fromWire(config))
    }
  }, [config, local])

  if (!config || !local) {
    return <div className="text-sm text-muted-foreground">Loading MCP configuration...</div>
  }

  const handleSave = async () => {
    await save(toWire(local))
  }

  const toggleExpand = (i: number) => {
    setExpanded((prev) => ({ ...prev, [i]: !prev[i] }))
  }

  const update = (i: number, patch: Partial<MCPServerConfig>) => {
    setLocal((prev) => prev!.map((s, j) => (j === i ? { ...s, ...patch } : s)))
  }

  const remove = (i: number) => {
    setLocal((prev) => prev!.filter((_, j) => j !== i))
  }

  const add = () => {
    setLocal((prev) => [...prev!, emptyServer()])
    setExpanded((prev) => ({ ...prev, [local.length]: true }))
  }

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-sm font-bold text-foreground">MCP Servers</h2>
        <p className="text-[10px] text-muted-foreground">
          Configure Model Context Protocol servers. Agents opt in via <code>mcp_servers</code> in
          their YAML frontmatter.
        </p>
      </div>

      {local.length === 0 && (
        <div className="text-sm text-muted-foreground py-8 text-center border rounded-lg">
          No MCP servers configured. Click "Add Server" to get started.
        </div>
      )}

      <div className="space-y-3">
        {local.map((srv, i) => {
          const open = expanded[i] ?? false
          return (
            <div key={i} className="border rounded-lg bg-card p-4 shadow-sm">
              <div className="flex items-center justify-between">
                <button
                  type="button"
                  onClick={() => toggleExpand(i)}
                  className="flex items-center gap-2 text-sm font-medium text-foreground hover:text-primary"
                >
                  {open ? (
                    <ChevronDown className="h-4 w-4" />
                  ) : (
                    <ChevronRight className="h-4 w-4" />
                  )}
                  {srv.name || 'Unnamed Server'}
                </button>
                <div className="flex items-center gap-3">
                  <Switch
                    checked={srv.enabled}
                    onCheckedChange={(v) => update(i, { enabled: v })}
                  />
                  <Button size="sm" variant="outline" onClick={() => remove(i)}>
                    <Trash2 className="h-3 w-3" />
                  </Button>
                </div>
              </div>

              {open && (
                <div className="mt-4 grid gap-4 border-t border-border pt-4">
                  <Input
                    label="Name"
                    value={srv.name}
                    onChange={(e) => update(i, { name: e.target.value })}
                    placeholder="e.g. github, filesystem"
                  />
                  <Input
                    label="Command"
                    value={srv.command}
                    onChange={(e) => update(i, { command: e.target.value })}
                    placeholder="e.g. npx, uvx, node"
                  />
                  <div className="flex flex-col gap-1.5">
                    <Label>Arguments</Label>
                    <Input
                      value={srv.args.join(' ')}
                      onChange={(e) =>
                        update(i, { args: e.target.value.split(/\s+/).filter(Boolean) })
                      }
                      placeholder="e.g. -y @modelcontextprotocol/server-github"
                    />
                    <span className="text-[10px] text-muted-foreground">
                      Space-separated arguments
                    </span>
                  </div>
                  <Input label="Transport" value={srv.transport} disabled />
                  <div className="flex flex-col gap-1.5">
                    <Label>Environment Variables</Label>
                    <div className="space-y-1 mb-1">
                      {srv.env &&
                        Object.entries(srv.env).map(([key, value]) => (
                          <div key={key} className="flex gap-2 items-center">
                            <Input
                              value={key}
                              onChange={(e) => {
                                const newEnv = { ...srv.env }
                                delete newEnv[key]
                                newEnv[e.target.value] = value
                                update(i, { env: newEnv })
                              }}
                              placeholder="KEY"
                              className="flex-1"
                            />
                            <Input
                              value={value}
                              onChange={(e) => {
                                const newEnv = { ...srv.env, [key]: e.target.value }
                                update(i, { env: newEnv })
                              }}
                              placeholder="VALUE"
                              className="flex-1"
                            />
                            <Button
                              size="sm"
                              variant="outline"
                              onClick={() => {
                                const newEnv = { ...srv.env }
                                delete newEnv[key]
                                update(i, { env: Object.keys(newEnv).length ? newEnv : undefined })
                              }}
                            >
                              <Trash2 className="h-3 w-3" />
                            </Button>
                          </div>
                        ))}
                    </div>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => {
                        const newEnv = { ...(srv.env || {}), '': '' }
                        update(i, { env: newEnv })
                      }}
                    >
                      <Plus className="mr-1 h-3 w-3" /> Add Variable
                    </Button>
                  </div>
                </div>
              )}
            </div>
          )
        })}
      </div>

      <div className="flex items-center gap-3">
        <Button size="sm" variant="outline" onClick={add}>
          <Plus className="mr-1 h-3 w-3" /> Add Server
        </Button>
        <Button size="sm" onClick={handleSave} disabled={saving}>
          <Save className="mr-1 h-3 w-3" /> {saving ? 'Saving...' : 'Save'}
        </Button>
        {error && <span className="text-[10px] text-destructive">{error}</span>}
      </div>
    </div>
  )
}
