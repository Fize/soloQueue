import { useState, useEffect } from 'react'
import type { AgentInfo, AgentState } from '@/types'
import { useAgentProfile } from '@/hooks/useAgentProfile'
import { useAgentConfig } from '@/hooks/useAgentConfig'
import { useAgentStream } from '@/hooks/useAgentStream'
import { AgentStreamView } from '@/components/AgentStreamView'
import { updateAgentConfig, updateAgentProfile } from '@/lib/api'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { MarkdownPreview } from '@/components/ui/markdown-preview'
import { cn } from '@/lib/utils'
import { Save, Pencil, Eye, Loader2, Terminal } from 'lucide-react'

interface AgentDetailDialogProps {
  agent: AgentInfo | null
  templateId?: string | null
  templateName?: string | null
  isL1?: boolean
  open: boolean
  onOpenChange: (open: boolean) => void
}

const stateVariant: Record<AgentState, 'default' | 'secondary' | 'outline' | 'destructive'> = {
  processing: 'default',
  idle: 'secondary',
  stopping: 'outline',
  stopped: 'outline',
}

const stateLabel: Record<AgentState, string> = {
  processing: 'Running',
  idle: 'Idle',
  stopping: 'Stopping',
  stopped: 'Stopped',
}

// ─── Inline Editor ──────────────────────────────────────────────────────────

function InlineEditor({
  content,
  onSave,
  saving,
  height = 'h-[50vh]',
  type = 'yaml',
}: {
  content: string
  onSave: (draft: string) => Promise<void>
  saving: boolean
  height?: string
  type?: 'yaml' | 'markdown'
}) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(content)
  const [error, setError] = useState<string | null>(null)

  const handleSave = async () => {
    setError(null)
    try {
      await onSave(draft)
      setEditing(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Save failed')
    }
  }

  const handleCancel = () => {
    setDraft(content)
    setEditing(false)
    setError(null)
  }

  // Sync draft when external content changes (e.g. after save or tab switch)
  if (!editing && draft !== content) {
    // Will be updated on next render
  }

  return (
    <div>
      <div className="flex items-center justify-end gap-1 mb-2">
        <Button
          size="sm"
          variant={editing ? 'outline' : 'default'}
          className="h-7 gap-1 text-xs"
          onClick={() => {
            setDraft(content)
            setEditing(false)
          }}
          disabled={!editing}
        >
          <Eye className="h-3 w-3" />
          Preview
        </Button>
        <Button
          size="sm"
          variant={editing ? 'default' : 'outline'}
          className="h-7 gap-1 text-xs"
          onClick={() => {
            setDraft(content)
            setEditing(true)
          }}
          disabled={editing}
        >
          <Pencil className="h-3 w-3" />
          Edit
        </Button>
      </div>

      {editing ? (
        <textarea
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          className={`w-full ${height} resize-y rounded-md border-2 border-border bg-card p-4 font-mono text-xs leading-relaxed focus:outline-none focus:ring-2 focus:ring-primary/50`}
          spellCheck={false}
        />
      ) : (
        <ScrollArea className={`${height} rounded-md border border-border bg-muted/30 p-4`}>
          {content ? (
            type === 'markdown' ? (
              <MarkdownPreview content={content} />
            ) : (
              <pre className="whitespace-pre-wrap font-mono text-xs leading-relaxed">{content}</pre>
            )
          ) : (
            <p className="text-sm text-muted-foreground">No content</p>
          )}
        </ScrollArea>
      )}

      {editing && (
        <div className="mt-3 flex items-center gap-2">
          <Button size="sm" onClick={handleSave} disabled={saving || draft === content}>
            {saving ? (
              <>
                <Loader2 className="mr-1 h-3 w-3 animate-spin" /> Saving...
              </>
            ) : (
              <>
                <Save className="mr-1 h-3 w-3" /> Save
              </>
            )}
          </Button>
          <Button size="sm" variant="outline" onClick={handleCancel} disabled={saving}>
            Cancel
          </Button>
          {error && <span className="text-xs text-destructive">{error}</span>}
        </div>
      )}
    </div>
  )
}

// ─── Main Component ─────────────────────────────────────────────────────────

export function AgentDetailDialog({
  agent,
  templateId,
  templateName,
  isL1 = false,
  open,
  onOpenChange,
}: AgentDetailDialogProps) {
  const effectiveId = agent?.id ?? templateId ?? null
  const effectiveName = agent?.name ?? templateName ?? ''
  const hasAgent = !!agent

  const { profile, loading } = useAgentProfile(isL1 ? agent?.id || 'main' : null)
  const {
    config,
    loading: configLoading,
    refetch,
  } = useAgentConfig(!isL1 && effectiveId ? effectiveId : null)

  // Editing state — must be before any early return (Rules of Hooks).
  const [savingYaml, setSavingYaml] = useState(false)
  const [savingPrompt, setSavingPrompt] = useState(false)
  const [savingSoul, setSavingSoul] = useState(false)
  const [savingRules, setSavingRules] = useState(false)
  const [localSoul, setLocalSoul] = useState('')
  const [localRules, setLocalRules] = useState('')
  const [activeTab, setActiveTab] = useState('soul')

  useEffect(() => {
    if (profile) {
      setLocalSoul(profile.soul || '')
      setLocalRules(profile.rules || '')
    }
  }, [profile])

  // Reset to default tab when dialog opens, based on current agent state.
  useEffect(() => {
    if (!open) return
    if (isL1) {
      setActiveTab(agent?.state === 'processing' ? 'output' : 'soul')
    } else {
      setActiveTab(agent?.state === 'processing' ? 'output' : 'status')
    }
  }, [open, isL1, agent?.state])

  const stream = useAgentStream(agent?.instance_id ?? null)
  const hasOutput =
    agent?.state === 'processing' || (stream && (stream.segments.length > 0 || stream.error))

  if (!agent && !templateId) return null

  const handleSaveSoul = async (soul: string) => {
    setSavingSoul(true)
    try {
      const agentId = agent?.id || templateId || 'main'
      const updated = await updateAgentProfile(agentId, { soul })
      setLocalSoul(updated.soul || '')
    } finally {
      setSavingSoul(false)
    }
  }

  const handleSaveRules = async (rules: string) => {
    setSavingRules(true)
    try {
      const agentId = agent?.id || templateId || 'main'
      const updated = await updateAgentProfile(agentId, { rules })
      setLocalRules(updated.rules || '')
    } finally {
      setSavingRules(false)
    }
  }

  // L1 Agent 特殊展示：Soul 和 Rules
  if (isL1) {
    return (
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent>
          <DialogHeader>
            <div className="flex items-center gap-2 pr-12">
              <DialogTitle className="flex items-center gap-2">
                <span>{effectiveName}</span>
                <Badge variant="default" className="text-xs">
                  Main Agent
                </Badge>
              </DialogTitle>
            </div>
            {hasAgent && (
              <p className="font-mono text-xs text-muted-foreground">{agent!.instance_id}</p>
            )}
          </DialogHeader>

          <Tabs value={activeTab} onValueChange={setActiveTab} className="mt-2">
            <TabsList className="grid w-full grid-cols-3">
              <TabsTrigger value="output" disabled={!hasOutput || !hasAgent}>
                <Terminal className="mr-1 h-3 w-3" />
                Output
              </TabsTrigger>
              <TabsTrigger value="soul">Soul</TabsTrigger>
              <TabsTrigger value="rules">Rules</TabsTrigger>
            </TabsList>

            <TabsContent value="output" className="mt-3">
              <ScrollArea className="h-[50vh] rounded-md border border-border p-4">
                {stream ? (
                  <AgentStreamView state={stream} />
                ) : (
                  <p className="text-sm text-muted-foreground py-8 text-center">
                    {agent?.state === 'processing' ? 'Waiting for output...' : 'Agent not running'}
                  </p>
                )}
              </ScrollArea>
            </TabsContent>

            <TabsContent value="soul" className="mt-3">
              {loading ? (
                <p className="text-sm text-muted-foreground">Loading...</p>
              ) : (
                <InlineEditor
                  content={localSoul}
                  onSave={handleSaveSoul}
                  saving={savingSoul}
                  type="markdown"
                />
              )}
            </TabsContent>

            <TabsContent value="rules" className="mt-3">
              {loading ? (
                <p className="text-sm text-muted-foreground">Loading...</p>
              ) : (
                <InlineEditor
                  content={localRules}
                  onSave={handleSaveRules}
                  saving={savingRules}
                  type="markdown"
                />
              )}
            </TabsContent>
          </Tabs>
        </DialogContent>
      </Dialog>
    )
  }

  // 普通 Agent (L2/L3) — 可能无运行实例（仅模板）
  const handleSaveYaml = async (draft: string) => {
    setSavingYaml(true)
    try {
      await updateAgentConfig(effectiveId!, { raw_config: draft })
      refetch()
    } finally {
      setSavingYaml(false)
    }
  }

  const handleSavePrompt = async (draft: string) => {
    setSavingPrompt(true)
    try {
      await updateAgentConfig(effectiveId!, { system_prompt: draft })
      refetch()
    } finally {
      setSavingPrompt(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <span>{effectiveName}</span>
            {hasAgent ? (
              <Badge variant={stateVariant[agent.state]} className="text-xs capitalize">
                {stateLabel[agent.state] || agent.state}
              </Badge>
            ) : (
              <Badge variant="outline" className="text-xs">
                Not started
              </Badge>
            )}
          </DialogTitle>
          {hasAgent && (
            <p className="font-mono text-xs text-muted-foreground">{agent.instance_id}</p>
          )}
        </DialogHeader>

        <Tabs value={activeTab} onValueChange={setActiveTab} className="mt-2">
          <TabsList className="grid w-full grid-cols-5">
            <TabsTrigger value="output" disabled={!hasOutput || !hasAgent}>
              <Terminal className="mr-1 h-3 w-3" />
              Output
            </TabsTrigger>
            <TabsTrigger value="status" disabled={!hasAgent}>
              Status
            </TabsTrigger>
            <TabsTrigger value="details" disabled={!hasAgent}>
              Details
            </TabsTrigger>
            <TabsTrigger value="config">YAML</TabsTrigger>
            <TabsTrigger value="prompt">Prompt</TabsTrigger>
          </TabsList>

          {/* Output Tab — streaming agent output */}
          <TabsContent value="output" className="mt-3">
            <ScrollArea className="h-[50vh] rounded-md border border-border p-4">
              {stream ? (
                <AgentStreamView state={stream} />
              ) : (
                <p className="text-sm text-muted-foreground py-8 text-center">
                  {agent?.state === 'processing' ? 'Waiting for output...' : 'Agent not running'}
                </p>
              )}
            </ScrollArea>
          </TabsContent>

          {/* 状态 Tab */}
          <TabsContent value="status" className="mt-3">
            <ScrollArea className="h-[50vh] rounded-md border border-border p-4">
              <div className="space-y-3">
                {hasAgent ? (
                  <>
                    <div className="space-y-1.5">
                      <h4 className="text-xs font-semibold text-muted-foreground uppercase">
                        Work Status
                      </h4>
                      <div className="rounded-md border border-border p-3 space-y-2">
                        <div className="flex justify-between text-sm">
                          <span className="text-muted-foreground">Status</span>
                          <Badge variant={stateVariant[agent.state]} className="text-xs">
                            {stateLabel[agent.state] || agent.state}
                          </Badge>
                        </div>
                        {agent.error_count > 0 && (
                          <div className="flex justify-between text-sm">
                            <span className="text-muted-foreground">Error count</span>
                            <span className="text-destructive font-medium">
                              {agent.error_count}
                            </span>
                          </div>
                        )}
                        {agent.last_error && (
                          <div className="space-y-1">
                            <span className="text-xs text-muted-foreground">Last error</span>
                            <p className="rounded-md bg-destructive/10 p-2 text-xs text-destructive break-all">
                              {agent.last_error}
                            </p>
                          </div>
                        )}
                      </div>
                    </div>
                    <div className="space-y-1.5">
                      <h4 className="text-xs font-semibold text-muted-foreground uppercase">
                        Workload
                      </h4>
                      <div className="rounded-md border border-border p-3 space-y-2">
                        <div className="flex justify-between text-sm">
                          <span className="text-muted-foreground">Pending delegations</span>
                          <span className="font-medium">{agent.pending_delegations}</span>
                        </div>
                        <div className="flex justify-between text-sm">
                          <span className="text-muted-foreground">High priority mailbox</span>
                          <span className="font-medium">{agent.mailbox_high}</span>
                        </div>
                        <div className="flex justify-between text-sm">
                          <span className="text-muted-foreground">Normal mailbox</span>
                          <span className="font-medium">{agent.mailbox_normal}</span>
                        </div>
                      </div>
                    </div>
                  </>
                ) : (
                  <p className="text-sm text-muted-foreground py-8 text-center">
                    Agent not started, no runtime status
                  </p>
                )}
              </div>
            </ScrollArea>
          </TabsContent>

          {/* 详情 Tab */}
          <TabsContent value="details" className="mt-3">
            <ScrollArea className="h-[50vh] rounded-md border border-border p-4">
              <div className="space-y-3">
                {hasAgent ? (
                  <>
                    <div className="space-y-1.5">
                      <h4 className="text-xs font-semibold text-muted-foreground uppercase">
                        Basic Info
                      </h4>
                      <div className="rounded-md border border-border p-3 space-y-2">
                        <div className="flex justify-between text-sm">
                          <span className="text-muted-foreground">Name</span>
                          <span className="font-medium">{agent.name}</span>
                        </div>
                        <div className="flex justify-between text-sm">
                          <span className="text-muted-foreground">Model</span>
                          <span className="font-mono text-xs">{agent.model_id}</span>
                        </div>
                        <div className="flex justify-between text-sm">
                          <span className="text-muted-foreground">Group</span>
                          <span className="font-medium">{agent.group || 'Session'}</span>
                        </div>
                        <div className="flex justify-between text-sm">
                          <span className="text-muted-foreground">Task level</span>
                          <span className="font-medium">{agent.task_level || '-'}</span>
                        </div>
                        <div className="flex justify-between text-sm">
                          <span className="text-muted-foreground">Is Leader</span>
                          <span
                            className={cn('font-medium', agent.is_leader ? 'text-primary' : '')}
                          >
                            {agent.is_leader ? 'Yes' : 'No'}
                          </span>
                        </div>
                        <div className="flex justify-between text-sm">
                          <span className="text-muted-foreground">Error count</span>
                          <span
                            className={cn(
                              'font-medium',
                              agent.error_count > 0 ? 'text-destructive' : ''
                            )}
                          >
                            {agent.error_count}
                          </span>
                        </div>
                      </div>
                    </div>
                    <div className="space-y-1.5">
                      <h4 className="text-xs font-semibold text-muted-foreground uppercase">
                        Identifiers
                      </h4>
                      <div className="rounded-md border border-border p-3 space-y-2">
                        <div className="flex justify-between text-sm">
                          <span className="text-muted-foreground">ID</span>
                          <span className="font-mono text-xs text-muted-foreground">
                            {agent.id}
                          </span>
                        </div>
                        <div className="flex justify-between text-sm">
                          <span className="text-muted-foreground">Instance ID</span>
                          <span className="font-mono text-xs text-muted-foreground">
                            {agent.instance_id}
                          </span>
                        </div>
                      </div>
                    </div>
                  </>
                ) : (
                  <p className="text-sm text-muted-foreground py-8 text-center">
                    Agent not started, no details
                  </p>
                )}
              </div>
            </ScrollArea>
          </TabsContent>

          {/* YAML Tab — editable frontmatter config */}
          <TabsContent value="config" className="mt-3">
            {configLoading ? (
              <p className="text-sm text-muted-foreground">Loading...</p>
            ) : config ? (
              <InlineEditor
                content={config.raw_config || ''}
                onSave={handleSaveYaml}
                saving={savingYaml}
              />
            ) : (
              <p className="text-sm text-muted-foreground">No config info</p>
            )}
          </TabsContent>

          {/* Prompt Tab — editable markdown body / system prompt */}
          <TabsContent value="prompt" className="mt-3">
            {configLoading ? (
              <p className="text-sm text-muted-foreground">Loading...</p>
            ) : config ? (
              <InlineEditor
                content={config.system_prompt || ''}
                onSave={handleSavePrompt}
                saving={savingPrompt}
                type="markdown"
              />
            ) : (
              <p className="text-sm text-muted-foreground">No Prompt config</p>
            )}
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  )
}
