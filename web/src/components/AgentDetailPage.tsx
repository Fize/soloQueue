import { useState, useEffect, useMemo } from 'react'
import { useParams, useNavigate, useSearchParams } from 'react-router-dom'
import { useAgentStore } from '@/stores/agentStore'
import { useAgentProfile } from '@/hooks/useAgentProfile'
import { useAgentConfig } from '@/hooks/useAgentConfig'
import { useAgentStream } from '@/hooks/useAgentStream'
import { AgentStreamView } from '@/components/AgentStreamView'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import { MarkdownPreview } from '@/components/ui/markdown-preview'
import { GlassCard } from '@/components/ui/glass-card'
import { StatusBadge } from '@/components/ui/status-badge'
import { Button } from '@/components/ui/button'
import { ArrowLeft, Terminal, Loader2, AlertTriangle, Mail, Info } from 'lucide-react'

// ─── InlineContent Component ──────────────────────────────────────────────
interface InlineContentProps {
  content: string
  height?: string
  type?: 'yaml' | 'markdown'
}

function InlineContent({ content, height = 'min-h-[45vh]', type = 'yaml' }: InlineContentProps) {
  return (
    <div className="space-y-3 bg-card/40 rounded-xl border border-border/80 p-0">
      <div className="flex items-center border-b border-border/40 px-4 py-2.5">
        <span className="text-[10px] text-muted-foreground uppercase font-bold tracking-wider">
          {type === 'yaml' ? 'YAML Frontmatter Config' : 'Markdown Prompt Body'}
        </span>
      </div>
      <div className="px-4 pb-4">
        <ScrollArea className={`${height} rounded-lg border border-border/40 bg-card p-4`}>
          {content ? (
            type === 'markdown' ? (
              <MarkdownPreview content={content} />
            ) : (
              <pre className="whitespace-pre-wrap font-mono text-[11px] leading-relaxed text-foreground/90">
                {content}
              </pre>
            )
          ) : (
            <p className="text-xs text-muted-foreground italic py-4 text-center">
              No content configured
            </p>
          )}
        </ScrollArea>
      </div>
    </div>
  )
}

// ─── Main Page Component ───────────────────────────────────────────────────────
export function AgentDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()

  // Find agent in websocket stream or team list
  const data = useAgentStore((state) => state.agents)
  const teamsData = useAgentStore((state) => state.teams)
  const fetchLiveAgents = useAgentStore((state) => state.fetchLiveAgents)
  const fetchTeams = useAgentStore((state) => state.fetchTeams)

  useEffect(() => {
    fetchLiveAgents()
    fetchTeams()
  }, [fetchLiveAgents, fetchTeams])

  // Resolve the agent
  const agent = useMemo(() => {
    if (!data || !id) return null
    return data.agents.find((a) => a.instance_id === id || a.id === id) || null
  }, [data, id])

  // L1 Coordinator detection
  const isL1 = useMemo(() => {
    if (id === 'main' || id === 'l1-agent') return true
    if (!data) return false
    const { supervisors } = data
    const l2Ids = new Set(supervisors.map((sv) => sv.leader_id).filter(Boolean))
    const l3Ids = new Set(supervisors.flatMap((sv) => sv.children_ids))
    const resolvedL1 = data.agents.find(
      (a) => !l2Ids.has(a.instance_id) && !l3Ids.has(a.instance_id)
    )
    return resolvedL1 ? resolvedL1.instance_id === id || resolvedL1.id === id : false
  }, [data, id])

  // Find template name if no active agent instance
  const templateName = useMemo(() => {
    if (isL1) return 'L1 Agent'
    if (!teamsData || !id) return ''
    for (const team of teamsData.teams) {
      const match = team.agents.find((a) => a.id === id)
      if (match) return match.name
    }
    return ''
  }, [teamsData, id, isL1])

  const effectiveId = agent?.id ?? id ?? null
  const effectiveName = agent?.name ?? templateName ?? 'Unknown Agent'
  const hasAgent = !!agent

  // Fetch configs/profile hooks
  const { profile, loading: profileLoading } = useAgentProfile(isL1 ? agent?.id || 'main' : null)
  const { config, loading: configLoading } = useAgentConfig(
    !isL1 && effectiveId && effectiveId !== 'l1-agent' && effectiveId !== 'main'
      ? effectiveId
      : null
  )

  // Stream output hook
  const stream = useAgentStream(agent?.instance_id ?? null)
  const hasOutput =
    agent?.state === 'processing' || (stream && (stream.segments.length > 0 || stream.error))

  // Editing state
  const [localSoul, setLocalSoul] = useState('')
  const [localRules, setLocalRules] = useState('')
  const [activeTab, setActiveTab] = useState(isL1 ? 'soul' : 'status')

  // Load profile values
  useEffect(() => {
    if (profile) {
      setLocalSoul(profile.soul || '')
      setLocalRules(profile.rules || '')
    }
  }, [profile])

  // Auto-select best default tab
  useEffect(() => {
    // URL ?tab= parameter takes priority
    const tabParam = searchParams.get('tab')
    if (
      tabParam &&
      ['output', 'status', 'details', 'config', 'prompt', 'soul', 'rules'].includes(tabParam)
    ) {
      setActiveTab(tabParam as typeof activeTab)
      return
    }
    if (hasAgent && agent.state === 'processing') {
      setActiveTab('output')
    } else if (isL1) {
      setActiveTab('soul')
    } else {
      setActiveTab(hasAgent ? 'status' : 'config')
    }
  }, [searchParams, hasAgent, agent?.state, isL1])

  return (
    <div className="flex h-full flex-col min-w-0 bg-background overflow-hidden pb-16 md:pb-0">
      {/* Sticky Header */}
      <header className="flex shrink-0 items-center justify-between border-b border-border/80 px-4 py-3 md:px-6 bg-card/65 backdrop-blur-md sticky top-0 z-10">
        <div className="flex items-center gap-3 min-w-0">
          <Button
            variant="ghost"
            size="icon"
            onClick={() => navigate('/')}
            className="h-8 w-8 shrink-0"
          >
            <ArrowLeft className="h-4.5 w-4.5 text-foreground" />
          </Button>
          <div className="min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <h1 className="text-base font-bold text-foreground truncate">{effectiveName}</h1>
              {isL1 ? (
                <Badge
                  variant="primary"
                  className="text-[9px] uppercase tracking-wider py-0 px-1.5 shrink-0"
                >
                  L1 Coordinator
                </Badge>
              ) : agent?.is_leader ? (
                <Badge
                  variant="primary"
                  className="text-[9px] uppercase tracking-wider py-0 px-1.5 shrink-0"
                >
                  Leader
                </Badge>
              ) : null}
              {hasAgent ? (
                <StatusBadge state={agent.state} size="sm" />
              ) : (
                <Badge
                  variant="outline"
                  className="text-[10px] text-muted-foreground border-dashed"
                >
                  Offline
                </Badge>
              )}
            </div>
            {hasAgent && (
              <p className="font-mono text-[9px] text-muted-foreground/60 truncate mt-0.5">
                {agent.model_id} · {agent.instance_id}
              </p>
            )}
          </div>
        </div>
      </header>

      {/* Tabs and Tab Content (Self-scrolling) */}
      <Tabs
        value={activeTab}
        onValueChange={(val) => {
          setActiveTab(val as any)
          setSearchParams({ tab: val })
        }}
        className="flex-1 flex flex-col min-h-0"
      >
        {/* Horizontal Tab Bar (Sticky) */}
        <div className="shrink-0 border-b border-border/40 bg-card/45 px-4 md:px-6 py-1 overflow-x-auto no-scrollbar">
          <TabsList className="flex bg-transparent border-0 gap-1.5 min-w-max">
            {isL1 ? (
              /* L1 Coordinator Tabs */
              <>
                <TabsTrigger
                  value="output"
                  disabled={!hasOutput || !hasAgent}
                  className="px-3.5 py-1 text-xs font-semibold rounded-md transition-all disabled:opacity-40"
                >
                  <Terminal className="mr-1.5 h-3.5 w-3.5" />
                  Output
                </TabsTrigger>
                <TabsTrigger
                  value="soul"
                  className="px-3.5 py-1 text-xs font-semibold rounded-md transition-all"
                >
                  Soul
                </TabsTrigger>
                <TabsTrigger
                  value="rules"
                  className="px-3.5 py-1 text-xs font-semibold rounded-md transition-all"
                >
                  Rules
                </TabsTrigger>
              </>
            ) : (
              /* L2/L3 Worker Tabs */
              <>
                <TabsTrigger
                  value="output"
                  disabled={!hasOutput || !hasAgent}
                  className="px-3.5 py-1 text-xs font-semibold rounded-md transition-all disabled:opacity-40"
                >
                  <Terminal className="mr-1.5 h-3.5 w-3.5" />
                  Output
                </TabsTrigger>
                <TabsTrigger
                  value="status"
                  disabled={!hasAgent}
                  className="px-3.5 py-1 text-xs font-semibold rounded-md transition-all disabled:opacity-40"
                >
                  <Info className="mr-1.5 h-3.5 w-3.5" />
                  Status
                </TabsTrigger>
                <TabsTrigger
                  value="details"
                  disabled={!hasAgent}
                  className="px-3.5 py-1 text-xs font-semibold rounded-md transition-all disabled:opacity-40"
                >
                  Details
                </TabsTrigger>
                <TabsTrigger
                  value="config"
                  className="px-3.5 py-1 text-xs font-semibold rounded-md transition-all"
                >
                  Config
                </TabsTrigger>
                <TabsTrigger
                  value="prompt"
                  className="px-3.5 py-1 text-xs font-semibold rounded-md transition-all"
                >
                  Prompt
                </TabsTrigger>
              </>
            )}
          </TabsList>
        </div>

        {/* Tab Content Areas */}
        <div className="flex-1 overflow-hidden relative">
          {/* L1 Coordinator Tabs Content */}
          {isL1 && (
            <>
              <TabsContent value="output" className="h-full mt-0 focus-visible:outline-none">
                <ScrollArea className="h-full p-4 md:p-6 bg-card/20">
                  {stream ? (
                    <div className="max-w-3xl mx-auto">
                      <AgentStreamView state={stream} />
                    </div>
                  ) : (
                    <p className="text-xs text-muted-foreground py-8 text-center italic">
                      Waiting for stream output...
                    </p>
                  )}
                </ScrollArea>
              </TabsContent>

              <TabsContent value="soul" className="h-full mt-0 focus-visible:outline-none">
                <ScrollArea className="h-full p-4 md:p-6 bg-card/20">
                  <div className="max-w-3xl mx-auto">
                    {profileLoading ? (
                      <div className="flex justify-center py-10">
                        <Loader2 className="h-5 w-5 animate-spin text-primary" />
                      </div>
                    ) : (
                      <InlineContent content={localSoul} type="markdown" />
                    )}
                  </div>
                </ScrollArea>
              </TabsContent>

              <TabsContent value="rules" className="h-full mt-0 focus-visible:outline-none">
                <ScrollArea className="h-full p-4 md:p-6 bg-card/20">
                  <div className="max-w-3xl mx-auto">
                    {profileLoading ? (
                      <div className="flex justify-center py-10">
                        <Loader2 className="h-5 w-5 animate-spin text-primary" />
                      </div>
                    ) : (
                      <InlineContent content={localRules} type="markdown" />
                    )}
                  </div>
                </ScrollArea>
              </TabsContent>
            </>
          )}

          {/* L2/L3 Worker Tabs Content */}
          {!isL1 && (
            <>
              <TabsContent value="output" className="h-full mt-0 focus-visible:outline-none">
                <ScrollArea className="h-full p-4 md:p-6 bg-card/20">
                  {stream ? (
                    <div className="max-w-3xl mx-auto">
                      <AgentStreamView state={stream} />
                    </div>
                  ) : (
                    <p className="text-xs text-muted-foreground py-8 text-center italic">
                      Waiting for stream output...
                    </p>
                  )}
                </ScrollArea>
              </TabsContent>

              <TabsContent value="status" className="h-full mt-0 focus-visible:outline-none">
                <ScrollArea className="h-full p-4 md:p-6 bg-card/20">
                  {hasAgent ? (
                    <div className="max-w-3xl mx-auto space-y-4">
                      {/* Workload Status Card */}
                      <GlassCard className="space-y-4">
                        <h2 className="text-sm font-bold text-foreground border-b border-border/40 pb-2">
                          Workload Status
                        </h2>
                        <div className="grid grid-cols-2 gap-4">
                          <div className="space-y-1">
                            <span className="text-[10px] text-muted-foreground font-bold uppercase tracking-wider">
                              Pending Delegations
                            </span>
                            <p className="text-xl font-bold tracking-tight text-foreground tabular-nums">
                              {agent.pending_delegations}
                            </p>
                          </div>
                          <div className="space-y-1">
                            <span className="text-[10px] text-muted-foreground font-bold uppercase tracking-wider">
                              Mailbox (High / Normal)
                            </span>
                            <div className="flex items-center gap-1 text-xl font-bold tracking-tight text-foreground tabular-nums">
                              <Mail className="h-4.5 w-4.5 text-muted-foreground" />
                              <span>
                                {agent.mailbox_high} / {agent.mailbox_normal}
                              </span>
                            </div>
                          </div>
                        </div>
                      </GlassCard>

                      {/* Error Info Card */}
                      {agent.error_count > 0 && (
                        <GlassCard variant="error" className="space-y-3">
                          <div className="flex items-center gap-2 text-destructive">
                            <AlertTriangle className="h-4.5 w-4.5 shrink-0" />
                            <h2 className="text-sm font-bold">
                              Errors Detected ({agent.error_count})
                            </h2>
                          </div>
                          <ScrollArea className="max-h-[20vh] bg-destructive/5 rounded-md border border-destructive/25 p-3">
                            <pre className="whitespace-pre-wrap font-mono text-[10px] leading-relaxed text-destructive">
                              {agent.last_error || 'No error details recorded'}
                            </pre>
                          </ScrollArea>
                        </GlassCard>
                      )}
                    </div>
                  ) : (
                    <p className="text-xs text-muted-foreground py-8 text-center italic">
                      Agent offline, no status available
                    </p>
                  )}
                </ScrollArea>
              </TabsContent>

              <TabsContent value="details" className="h-full mt-0 focus-visible:outline-none">
                <ScrollArea className="h-full p-4 md:p-6 bg-card/20">
                  {hasAgent ? (
                    <div className="max-w-3xl mx-auto">
                      <GlassCard className="space-y-4">
                        <h2 className="text-sm font-bold text-foreground border-b border-border/40 pb-2">
                          Agent Details
                        </h2>
                        <dl className="grid grid-cols-1 sm:grid-cols-2 gap-4 text-xs">
                          <div className="space-y-1">
                            <dt className="text-muted-foreground font-bold uppercase tracking-wider text-[9px]">
                              Display Name
                            </dt>
                            <dd className="font-semibold text-foreground">{agent.name}</dd>
                          </div>
                          <div className="space-y-1">
                            <dt className="text-muted-foreground font-bold uppercase tracking-wider text-[9px]">
                              Model ID
                            </dt>
                            <dd className="font-mono text-foreground">{agent.model_id}</dd>
                          </div>
                          <div className="space-y-1">
                            <dt className="text-muted-foreground font-bold uppercase tracking-wider text-[9px]">
                              Group / Team
                            </dt>
                            <dd className="font-semibold text-foreground">{agent.group || '-'}</dd>
                          </div>
                          <div className="space-y-1">
                            <dt className="text-muted-foreground font-bold uppercase tracking-wider text-[9px]">
                              Task Level
                            </dt>
                            <dd className="font-semibold text-foreground">
                              {agent.task_level ? `Level ${agent.task_level}` : '-'}
                            </dd>
                          </div>
                          <div className="space-y-1 sm:col-span-2">
                            <dt className="text-muted-foreground font-bold uppercase tracking-wider text-[9px]">
                              Instance ID
                            </dt>
                            <dd className="font-mono text-foreground break-all">
                              {agent.instance_id}
                            </dd>
                          </div>
                          <div className="space-y-1 sm:col-span-2">
                            <dt className="text-muted-foreground font-bold uppercase tracking-wider text-[9px]">
                              Template ID
                            </dt>
                            <dd className="font-mono text-foreground break-all">{agent.id}</dd>
                          </div>
                        </dl>
                      </GlassCard>
                    </div>
                  ) : (
                    <p className="text-xs text-muted-foreground py-8 text-center italic">
                      Agent offline, no details available
                    </p>
                  )}
                </ScrollArea>
              </TabsContent>

              <TabsContent value="config" className="h-full mt-0 focus-visible:outline-none">
                <ScrollArea className="h-full p-4 md:p-6 bg-card/20">
                  <div className="max-w-3xl mx-auto">
                    {configLoading ? (
                      <div className="flex justify-center py-10">
                        <Loader2 className="h-5 w-5 animate-spin text-primary" />
                      </div>
                    ) : config ? (
                      <InlineContent content={config.raw_config || ''} type="yaml" />
                    ) : (
                      <p className="text-xs text-muted-foreground py-8 text-center italic">
                        No config details loaded
                      </p>
                    )}
                  </div>
                </ScrollArea>
              </TabsContent>

              <TabsContent value="prompt" className="h-full mt-0 focus-visible:outline-none">
                <ScrollArea className="h-full p-4 md:p-6 bg-card/20">
                  <div className="max-w-3xl mx-auto">
                    {configLoading ? (
                      <div className="flex justify-center py-10">
                        <Loader2 className="h-5 w-5 animate-spin text-primary" />
                      </div>
                    ) : config ? (
                      <InlineContent content={config.system_prompt || ''} type="markdown" />
                    ) : (
                      <p className="text-xs text-muted-foreground py-8 text-center italic">
                        No system prompt details loaded
                      </p>
                    )}
                  </div>
                </ScrollArea>
              </TabsContent>
            </>
          )}
        </div>
      </Tabs>
    </div>
  )
}
