import { useState, useEffect, useMemo, useRef } from 'react'
import { useParams, useNavigate, useSearchParams } from 'react-router-dom'
import { useAgentStore } from '@/stores/agentStore'
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
import { useTranslation } from '@/lib/i18n'
import { cn } from '@/lib/utils'
import { useRuntimeStore } from '@/stores/runtimeStore'

// ─── InlineContent Component ──────────────────────────────────────────────
interface InlineContentProps {
  content: string
  height?: string
  type?: 'yaml' | 'markdown'
}

function InlineContent({ content, height = 'min-h-[45vh]', type = 'yaml' }: InlineContentProps) {
  const { t } = useTranslation()
  return (
    <div className="space-y-3 bg-card/40 rounded-xl border border-border/80 p-0">
      <div className="flex items-center border-b border-border/40 px-4 py-2.5">
        <span className="text-[10px] text-muted-foreground uppercase font-bold tracking-wider">
          {type === 'yaml' ? t('agent.yamlFrontmatter') : t('agent.markdownPrompt')}
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
              {t('agent.noContent')}
            </p>
          )}
        </ScrollArea>
      </div>
    </div>
  )
}

// ─── Main Page Component ───────────────────────────────────────────────────────
export function AgentDetailPage() {
  const { t } = useTranslation()
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const sidebarCollapsed = useRuntimeStore((s) => s.sidebarCollapsed)
  const status = useRuntimeStore((s) => s.status)
  const outputScrollRef = useRef<HTMLDivElement>(null)

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

  // {t('agent.coordinator')} detection
  const isL1 = useMemo(() => {
    if (id === 'main' || id === 'l1-agent') return true
    // L1 is identified by known ID, not subtraction heuristic.
    if (!data) return false
    const l1 = data.agents.find((a) => a.id === 'l1-agent')
    if (!l1) return false
    return l1.instance_id === id || l1.id === id
  }, [data, id])

  // Find template name if no active agent instance
  const templateName = useMemo(() => {
    if (isL1) return t('agent.l1Agent')
    if (!teamsData || !id) return ''
    for (const team of teamsData.teams) {
      const match = team.agents.find((a) => a.id === id)
      if (match) return match.name
    }
    return ''
  }, [teamsData, id, isL1, t])

  const effectiveId = agent?.id ?? id ?? null
  const effectiveName = isL1 ? t('sidebar.assistant') : agent?.name ?? templateName ?? t('agent.unknownAgent')
  const hasAgent = !!agent

  // Fetch configs/profile from the shared store (single source of truth)
  const storeProfile = useAgentStore((s) => s.profile)
  const storeProfileLoading = useAgentStore((s) => s.profileLoading)
  const storeConfig = useAgentStore((s) => s.config)
  const storeConfigLoading = useAgentStore((s) => s.configLoading)
  const fetchProfile = useAgentStore((s) => s.fetchProfile)
  const fetchConfig = useAgentStore((s) => s.fetchConfig)

  const profileAgentId = isL1 ? agent?.id || 'main' : null
  const configAgentId =
    !isL1 && effectiveId && effectiveId !== 'l1-agent' && effectiveId !== 'main'
      ? effectiveId
      : null

  useEffect(() => {
    if (profileAgentId) fetchProfile(profileAgentId)
  }, [profileAgentId, fetchProfile])

  useEffect(() => {
    if (configAgentId) fetchConfig(configAgentId)
  }, [configAgentId, fetchConfig])

  const profile = storeProfile
  const profileLoading = storeProfileLoading
  const config = storeConfig
  const configLoading = storeConfigLoading

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

  const fmtTokens = (v: number) => {
    if (v >= 1_000_000) return `${(v / 1_000_000).toFixed(1)}M`
    if (v >= 1_000) return `${(v / 1_000).toFixed(1)}k`
    return String(v)
  }

  return (
    <div className="flex h-full flex-col min-w-0 bg-background overflow-hidden pb-16 md:pb-0">
      <header
        className={cn(
          'flex shrink-0 items-center justify-between border-b border-border/80 px-4 py-3 md:px-6 bg-card/65 backdrop-blur-md sticky top-0 z-10',
          sidebarCollapsed && 'pl-[115px]'
        )}
      >
        <div className="flex items-center gap-3 min-w-0 electron-no-drag">
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
                  {t('agent.coordinator')}
                </Badge>
              ) : agent?.is_leader ? (
                <Badge
                  variant="primary"
                  className="text-[9px] uppercase tracking-wider py-0 px-1.5 shrink-0"
                >
                  {t('agent.leader')}
                </Badge>
              ) : null}
              {hasAgent ? (
                <StatusBadge state={agent.state} size="sm" errorCount={agent.error_count} iteration={agent.iteration} />
              ) : (
                <Badge
                  variant="outline"
                  className="text-[10px] text-muted-foreground border-dashed"
                >
                  {t('agent.offline')}
                </Badge>
              )}
            </div>
            {hasAgent && (
              <p className="font-mono text-[9px] text-muted-foreground/60 truncate mt-0.5">
                {agent.model_id} · {agent.instance_id}
              </p>
            )}
            {hasAgent && status && (
              <div className="flex items-center gap-2 mt-1">
                <div className="flex-1 h-1 bg-muted/60 rounded-full overflow-hidden max-w-[120px]">
                  <div
                    className="h-full rounded-full transition-all duration-500"
                    style={{
                      width: `${Math.min(status.context_pct || 0, 100)}%`,
                      backgroundColor:
                        (status.context_pct || 0) > 80
                          ? 'var(--destructive)'
                          : 'var(--color-signal)',
                    }}
                  />
                </div>
                <span className="text-[9px] font-mono text-muted-foreground/70 tabular-nums">
                  {fmtTokens(status.current_tokens)}/{fmtTokens(status.max_tokens)}
                </span>
                <span className="text-[9px] font-mono text-muted-foreground/50 tabular-nums">
                  P:{fmtTokens(status.prompt_tokens)} O:{fmtTokens(status.output_tokens)}
                </span>
              </div>
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
              /* {t('agent.coordinator')} Tabs */
              <>
                <TabsTrigger
                  value="output"
                  disabled={!hasOutput || !hasAgent}
                  className="px-3.5 py-1 text-xs font-semibold rounded-md transition-all disabled:opacity-40"
                >
                  <Terminal className="mr-1.5 h-3.5 w-3.5" />
                  {t('agent.output')}
                </TabsTrigger>
                <TabsTrigger
                  value="soul"
                  className="px-3.5 py-1 text-xs font-semibold rounded-md transition-all"
                >
                  {t('agent.soul')}
                </TabsTrigger>
                <TabsTrigger
                  value="rules"
                  className="px-3.5 py-1 text-xs font-semibold rounded-md transition-all"
                >
                  {t('agent.rules')}
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
                  {t('agent.output')}
                </TabsTrigger>
                <TabsTrigger
                  value="status"
                  disabled={!hasAgent}
                  className="px-3.5 py-1 text-xs font-semibold rounded-md transition-all disabled:opacity-40"
                >
                  <Info className="mr-1.5 h-3.5 w-3.5" />
                  {t('agent.status')}
                </TabsTrigger>
                <TabsTrigger
                  value="details"
                  disabled={!hasAgent}
                  className="px-3.5 py-1 text-xs font-semibold rounded-md transition-all disabled:opacity-40"
                >
                  {t('agent.details')}
                </TabsTrigger>
                <TabsTrigger
                  value="config"
                  className="px-3.5 py-1 text-xs font-semibold rounded-md transition-all"
                >
                  {t('agent.config')}
                </TabsTrigger>
                <TabsTrigger
                  value="prompt"
                  className="px-3.5 py-1 text-xs font-semibold rounded-md transition-all"
                >
                  {t('agent.prompt')}
                </TabsTrigger>
              </>
            )}
          </TabsList>
        </div>

        {/* Tab Content Areas */}
        <div className="flex-1 overflow-hidden relative">
          {/* {t('agent.coordinator')} Tabs Content */}
          {isL1 && (
            <>
              <TabsContent value="output" className="h-full mt-0 focus-visible:outline-none">
                <ScrollArea
                  viewportRef={outputScrollRef}
                  className="h-full p-4 md:p-6 bg-card/20"
                >
                  {stream ? (
                    <div className="max-w-3xl mx-auto">
                      <AgentStreamView state={stream} scrollContainerRef={outputScrollRef} />
                    </div>
                  ) : (
                    <p className="text-xs text-muted-foreground py-8 text-center italic">
                      {t('agent.waitingStream')}
                    </p>
                  )}
                </ScrollArea>
              </TabsContent>

              <TabsContent value="soul" className="h-full mt-0 focus-visible:outline-none">
                <ScrollArea className="h-full p-4 md:p-6 bg-card/20">
                  <div className="max-w-3xl mx-auto">
                    {profileLoading ? (
                      <div className="flex justify-center py-10">
                        <Loader2 className="h-5 w-5 animate-spin text-signal" />
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
                        <Loader2 className="h-5 w-5 animate-spin text-signal" />
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
                <ScrollArea
                  viewportRef={outputScrollRef}
                  className="h-full p-4 md:p-6 bg-card/20"
                >
                  {stream ? (
                    <div className="max-w-3xl mx-auto">
                      <AgentStreamView state={stream} scrollContainerRef={outputScrollRef} />
                    </div>
                  ) : (
                    <p className="text-xs text-muted-foreground py-8 text-center italic">
                      {t('agent.waitingStream')}
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
                          {t('agent.workloadStatus')}
                        </h2>
                        <div className="grid grid-cols-2 gap-4">
                          <div className="space-y-1">
                            <span className="text-[10px] text-muted-foreground font-bold uppercase tracking-wider">
                              {t('agent.pendingDelegations')}
                            </span>
                            <p className="text-xl font-bold tracking-tight text-foreground tabular-nums">
                              {agent.pending_delegations}
                            </p>
                          </div>
                          <div className="space-y-1">
                            <span className="text-[10px] text-muted-foreground font-bold uppercase tracking-wider">
                              {t('agent.mailbox')}
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
                              {t('agent.errorsDetected', { count: agent.error_count })}
                            </h2>
                          </div>
                          <ScrollArea className="max-h-[20vh] bg-destructive/5 rounded-md border border-destructive/25 p-3">
                            <pre className="whitespace-pre-wrap font-mono text-[10px] leading-relaxed text-destructive">
                              {agent.last_error || t('common.none')}
                            </pre>
                          </ScrollArea>
                        </GlassCard>
                      )}

                      {/* Token Stats Card */}
                      {status && (
                        <GlassCard className="space-y-3">
                          <h2 className="text-sm font-bold text-foreground border-b border-border/40 pb-2">
                            Token Consumption
                          </h2>
                          <div className="grid grid-cols-2 gap-3">
                            <div className="space-y-1">
                              <span className="text-[10px] text-muted-foreground font-bold uppercase tracking-wider">
                                Prompt
                              </span>
                              <p className="text-lg font-bold tracking-tight text-foreground tabular-nums">
                                {fmtTokens(Number(status.prompt_tokens))}
                              </p>
                            </div>
                            <div className="space-y-1">
                              <span className="text-[10px] text-muted-foreground font-bold uppercase tracking-wider">
                                Completion
                              </span>
                              <p className="text-lg font-bold tracking-tight text-foreground tabular-nums">
                                {fmtTokens(Number(status.output_tokens))}
                              </p>
                            </div>
                            <div className="space-y-1">
                              <span className="text-[10px] text-muted-foreground font-bold uppercase tracking-wider">
                                Cache Hit
                              </span>
                              <p className="text-lg font-bold tracking-tight text-[var(--color-chart-3)] tabular-nums">
                                {fmtTokens(Number(status.cache_hit_tokens))}
                              </p>
                            </div>
                            <div className="space-y-1">
                              <span className="text-[10px] text-muted-foreground font-bold uppercase tracking-wider">
                                Cache Miss
                              </span>
                              <p className="text-lg font-bold tracking-tight text-[var(--warning)] tabular-nums">
                                {fmtTokens(Number(status.cache_miss_tokens))}
                              </p>
                            </div>
                          </div>
                          {Number(status.cache_hit_tokens) + Number(status.cache_miss_tokens) >
                            0 && (
                            <div className="flex items-center gap-2 pt-1 border-t border-border/20">
                              <span className="text-[10px] text-muted-foreground font-bold uppercase tracking-wider">
                                Cache Hit Rate
                              </span>
                              <span className="text-xs font-bold tabular-nums text-[var(--color-chart-3)]">
                                {Math.round(
                                  (Number(status.cache_hit_tokens) /
                                    (Number(status.cache_hit_tokens) +
                                      Number(status.cache_miss_tokens))) *
                                    100
                                )}
                                %
                              </span>
                            </div>
                          )}
                        </GlassCard>
                      )}
                    </div>
                  ) : (
                    <p className="text-xs text-muted-foreground py-8 text-center italic">
                      {t('agent.noStatusAvailable')}
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
                          {t('agent.agentDetails')}
                        </h2>
                        <dl className="grid grid-cols-1 sm:grid-cols-2 gap-4 text-xs">
                          <div className="space-y-1">
                            <dt className="text-muted-foreground font-bold uppercase tracking-wider text-[9px]">
                              {t('agent.displayName')}
                            </dt>
                            <dd className="font-semibold text-foreground">{agent.name}</dd>
                          </div>
                          <div className="space-y-1">
                            <dt className="text-muted-foreground font-bold uppercase tracking-wider text-[9px]">
                              {t('agent.modelId')}
                            </dt>
                            <dd className="font-mono text-foreground">{agent.model_id}</dd>
                          </div>
                          <div className="space-y-1">
                            <dt className="text-muted-foreground font-bold uppercase tracking-wider text-[9px]">
                              {t('agent.groupTeam')}
                            </dt>
                            <dd className="font-semibold text-foreground">{agent.group || '-'}</dd>
                          </div>
                          <div className="space-y-1">
                            <dt className="text-muted-foreground font-bold uppercase tracking-wider text-[9px]">
                              {t('agent.taskLevel')}
                            </dt>
                            <dd className="font-semibold text-foreground">
                              {agent.task_level
                                ? t('agent.level', { level: agent.task_level })
                                : '-'}
                            </dd>
                          </div>
                          <div className="space-y-1 sm:col-span-2">
                            <dt className="text-muted-foreground font-bold uppercase tracking-wider text-[9px]">
                              {t('agent.instanceId')}
                            </dt>
                            <dd className="font-mono text-foreground break-all">
                              {agent.instance_id}
                            </dd>
                          </div>
                          <div className="space-y-1 sm:col-span-2">
                            <dt className="text-muted-foreground font-bold uppercase tracking-wider text-[9px]">
                              {t('agent.templateId')}
                            </dt>
                            <dd className="font-mono text-foreground break-all">{agent.id}</dd>
                          </div>
                        </dl>
                      </GlassCard>
                    </div>
                  ) : (
                    <p className="text-xs text-muted-foreground py-8 text-center italic">
                      {t('agent.noDetailsAvailable')}
                    </p>
                  )}
                </ScrollArea>
              </TabsContent>

              <TabsContent value="config" className="h-full mt-0 focus-visible:outline-none">
                <ScrollArea className="h-full p-4 md:p-6 bg-card/20">
                  <div className="max-w-3xl mx-auto">
                    {configLoading ? (
                      <div className="flex justify-center py-10">
                        <Loader2 className="h-5 w-5 animate-spin text-signal" />
                      </div>
                    ) : config ? (
                      <InlineContent content={config.raw_config || ''} type="yaml" />
                    ) : (
                      <p className="text-xs text-muted-foreground py-8 text-center italic">
                        {t('agent.noConfigDetails')}
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
                        <Loader2 className="h-5 w-5 animate-spin text-signal" />
                      </div>
                    ) : config ? (
                      <InlineContent content={config.system_prompt || ''} type="markdown" />
                    ) : (
                      <p className="text-xs text-muted-foreground py-8 text-center italic">
                        {t('agent.noPromptDetails')}
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
