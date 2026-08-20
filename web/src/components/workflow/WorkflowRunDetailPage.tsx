import { useEffect, useState, useMemo, useRef } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  ArrowLeft,
  Square,
  Pause,
  Play,
  RotateCw,
  Clock,
  Loader2,
  ChevronDown,
  ChevronRight,
  Trash2,
} from 'lucide-react'
import { toast } from 'sonner'
import { useTranslation } from '@/lib/i18n'
import { useWorkflowStore } from '@/stores/workflowStore'
import { useConnectionStore } from '@/stores/connectionStore'
import { useRuntimeStore } from '@/stores/runtimeStore'
import { listWorkflowRunEvents, resolveWorkflowConfirmation } from '@/lib/api/workflow-api'
import { DAGPreview } from './DAGPreview'
import { WorkflowStatusBadge, getStateBorderClass } from './WorkflowStatusBadge'
import { BackendUnavailable } from '@/components/BackendUnavailable'
import { cn } from '@/lib/utils'
import type { NodeRunDTO, NodeRunState, WorkflowRunEvent } from '@/types'

// ─── Timeline Tab ──────────────────────────────────────────────────────

function TimelineTab({ nodeRuns }: { nodeRuns: NodeRunDTO[] }) {
  // Sort by time
  const sorted = useMemo(
    () =>
      [...nodeRuns].sort(
        (a, b) => new Date(a.started_at).getTime() - new Date(b.started_at).getTime()
      ),
    [nodeRuns]
  )

  return (
    <div className="space-y-1 p-4">
      {sorted.map((nr) => (
        <div key={nr.id} className={cn('border-l-[3px] pl-3 py-2', getStateBorderClass(nr.state))}>
          <div className="flex items-center gap-2">
            <WorkflowStatusBadge state={nr.state} size="sm" showDot showLabel={false} />
            <span className="text-xs font-mono font-semibold text-foreground">{nr.node_id}</span>
            <span className="text-[9px] text-muted-foreground font-mono">
              {new Date(nr.started_at).toLocaleTimeString('en-US', {
                hour: '2-digit',
                minute: '2-digit',
                second: '2-digit',
              })}
            </span>
          </div>
          {nr.error && (
            <div className="mt-1 text-[10px] text-rose-500 font-mono truncate">{nr.error}</div>
          )}
        </div>
      ))}
      {sorted.length === 0 && (
        <p className="text-xs text-muted-foreground text-center py-8">No events yet</p>
      )}
    </div>
  )
}

// ─── Node Runs Tab ──────────────────────────────────────────────────────

function NodeRunsTab({ nodeRuns }: { nodeRuns: NodeRunDTO[] }) {
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})

  const toggleExpand = (id: string) => {
    setExpanded((prev) => ({ ...prev, [id]: !prev[id] }))
  }

  return (
    <div className="space-y-2 p-4">
      {nodeRuns.map((nr) => {
        const isExpanded = expanded[nr.id] || false
        const duration = nr.finished_at
          ? Math.round(
              (new Date(nr.finished_at).getTime() - new Date(nr.started_at).getTime()) / 1000
            )
          : null

        return (
          <div
            key={nr.id}
            className="rounded-xl border bg-card/35 border-border/40 overflow-hidden"
          >
            {/* Main row */}
            <button
              type="button"
              onClick={() => toggleExpand(nr.id)}
              className="flex w-full items-center justify-between px-3 py-2.5 hover:bg-muted/20 transition-colors text-left"
            >
              <div className="flex items-center gap-2 min-w-0">
                <WorkflowStatusBadge state={nr.state} size="sm" showDot showLabel={false} />
                <span className="text-xs font-mono font-semibold text-foreground truncate">
                  {nr.node_id}
                </span>
              </div>
              <div className="flex items-center gap-2 shrink-0">
                <span className="text-[9px] text-muted-foreground font-mono">
                  {nr.attempt > 1 ? `#${nr.attempt}` : ''}
                  {duration !== null ? ` ${duration}s` : ''}
                </span>
                {isExpanded ? (
                  <ChevronDown className="h-3 w-3 text-muted-foreground" />
                ) : (
                  <ChevronRight className="h-3 w-3 text-muted-foreground" />
                )}
              </div>
            </button>

            {/* Expanded detail */}
            {isExpanded && (
              <div className="border-t border-border/30 px-3 py-2.5 space-y-2 bg-muted/10">
                {/* Error */}
                {nr.error && (
                  <div className="rounded-lg bg-rose-500/10 border border-rose-500/20 px-2.5 py-2">
                    <span className="text-[10px] text-rose-500 font-mono">{nr.error}</span>
                  </div>
                )}

                {/* Inputs */}
                {nr.inputs.length > 0 && (
                  <div>
                    <span className="text-[9px] font-bold text-muted-foreground uppercase tracking-wider font-mono">
                      Inputs
                    </span>
                    <div className="mt-1 space-y-1">
                      {nr.inputs.map((input, i) => (
                        <div
                          key={i}
                          className="rounded bg-card/50 border border-border/40 px-2 py-1.5"
                        >
                          <div className="text-[9px] text-muted-foreground font-mono">
                            ← {input.from_node} · {input.outcome}
                          </div>
                          <div className="text-[10px] text-foreground mt-0.5 line-clamp-2 font-mono">
                            {input.content.slice(0, 200)}
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                {/* Result */}
                {nr.result && (
                  <div>
                    <span className="text-[9px] font-bold text-muted-foreground uppercase tracking-wider font-mono">
                      Result · {nr.result.outcome}
                    </span>
                    <div className="mt-1 rounded bg-card/50 border border-border/40 px-2 py-1.5">
                      <div className="text-[10px] text-foreground line-clamp-4 font-mono">
                        {nr.result.content.slice(0, 400)}
                      </div>
                    </div>
                  </div>
                )}

                {/* Timestamps */}
                <div className="flex items-center gap-3 text-[9px] text-muted-foreground/60 font-mono">
                  <span className="flex items-center gap-1">
                    <Clock className="h-2.5 w-2.5" />
                    {new Date(nr.started_at).toLocaleString()}
                  </span>
                  {nr.finished_at && <span>→ {new Date(nr.finished_at).toLocaleString()}</span>}
                </div>
              </div>
            )}
          </div>
        )
      })}
      {nodeRuns.length === 0 && (
        <p className="text-xs text-muted-foreground text-center py-8">No node runs yet</p>
      )}
    </div>
  )
}

// ─── Outputs Tab ────────────────────────────────────────────────────────

function OutputsTab({
  outputs,
}: {
  outputs: { node: string; outcome: string; content: string }[]
}) {
  return (
    <div className="space-y-2 p-4">
      {outputs.map((out, i) => (
        <div key={i} className="rounded-xl border bg-card/35 border-border/40 p-3">
          <div className="flex items-center gap-2 mb-2">
            <span className="text-[10px] font-mono font-semibold text-foreground">{out.node}</span>
            <span className="text-[9px] text-muted-foreground font-mono">→ {out.outcome}</span>
          </div>
          <div className="rounded-lg bg-card/50 border border-border/40 px-2.5 py-2">
            <pre className="text-[10px] text-foreground font-mono whitespace-pre-wrap break-words">
              {out.content.slice(0, 1000)}
            </pre>
          </div>
        </div>
      ))}
      {outputs.length === 0 && (
        <p className="text-xs text-muted-foreground text-center py-8">No outputs yet</p>
      )}
    </div>
  )
}

function AuditTab({ events }: { events: WorkflowRunEvent[] }) {
  return (
    <div className="space-y-1 p-4">
      {events.map((event) => (
        <div
          key={event.id}
          className="rounded-lg border border-border/40 bg-card/35 px-3 py-2 font-mono text-[10px]"
        >
          <div className="flex items-center justify-between gap-2 text-muted-foreground">
            <span>
              {event.id} · {event.type}
            </span>
            <span>{new Date(event.created_at).toLocaleTimeString()}</span>
          </div>
          <pre className="mt-1 whitespace-pre-wrap break-words text-foreground/80">
            {JSON.stringify(event.payload, null, 2)}
          </pre>
        </div>
      ))}
      {events.length === 0 && (
        <p className="py-8 text-center text-xs text-muted-foreground">No audit events yet</p>
      )}
    </div>
  )
}

// ─── Main Page ──────────────────────────────────────────────────────────

export function WorkflowRunDetailPage() {
  const { name, runId } = useParams<{ name: string; runId: string }>()
  const navigate = useNavigate()
  const { t } = useTranslation()
  const backendReady = useConnectionStore((state) => state.backendReady)
  const sidebarCollapsed = useRuntimeStore((state) => state.sidebarCollapsed)
  const {
    activeRunDetail,
    activeRunDetailLoading,
    fetchRunDetail,
    clearActiveRunDetail,
    cancelRun,
    pauseRun,
    resumeRun,
    restartRun,
    abandonRun,
    cleanupRun,
    activeWorkflowGraph,
    setActiveWorkflow,
  } = useWorkflowStore()

  const [activeTab, setActiveTab] = useState<'timeline' | 'nodeRuns' | 'outputs' | 'audit'>(
    'timeline'
  )
  const [events, setEvents] = useState<WorkflowRunEvent[]>([])
  const [cancelling, setCancelling] = useState(false)
  const [resolvingConfirmation, setResolvingConfirmation] = useState<string | null>(null)
  const [cleaningUp, setCleaningUp] = useState(false)
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    if (name) {
      setActiveWorkflow(name)
    }
    if (name && runId) fetchRunDetail(name, runId)
    return () => clearActiveRunDetail()
  }, [name, runId, fetchRunDetail, clearActiveRunDetail, setActiveWorkflow])

  useEffect(() => {
    if (!name || !runId) return
    void listWorkflowRunEvents(name, runId, 0, 200)
      .then((response) => setEvents(response.data || []))
      .catch(() => setEvents([]))
  }, [name, runId, activeRunDetail?.status])

  const prevBackendReadyRef = useRef(false)
  useEffect(() => {
    const wasReady = prevBackendReadyRef.current
    prevBackendReadyRef.current = backendReady
    if (!backendReady || wasReady) return
    if (!name || !runId || activeRunDetailLoading) return
    void fetchRunDetail(name, runId)
    void listWorkflowRunEvents(name, runId, 0, 200)
      .then((response) => setEvents(response.data || []))
      .catch(() => setEvents([]))
  }, [backendReady, activeRunDetailLoading, name, runId, fetchRunDetail])

  useEffect(() => {
    if (
      !name ||
      !runId ||
      !['running', 'preparing_worktree', 'pause_requested', 'resuming'].includes(
        activeRunDetail?.status || ''
      )
    )
      return
    const timer = window.setInterval(() => {
      fetchRunDetail(name, runId)
      setNow(Date.now())
    }, 1000)
    return () => window.clearInterval(timer)
  }, [name, runId, activeRunDetail?.status, fetchRunDetail])

  const handleCancel = async () => {
    if (!name || !runId) return
    setCancelling(true)
    try {
      await cancelRun(name, runId)
      await fetchRunDetail(name, runId)
      toast.success(t('workflow.runCancelled'))
    } catch {
      toast.error(t('workflow.cancelRunFailed'))
    } finally {
      setCancelling(false)
    }
  }

  const handlePause = async () => {
    if (!name || !runId) return
    try {
      await pauseRun(name, runId, 'graceful')
      await fetchRunDetail(name, runId)
    } catch (err: any) {
      toast.error(err.message || 'Pause failed')
    }
  }

  const handleResume = async () => {
    if (!name || !runId) return
    try {
      await resumeRun(name, runId)
      await fetchRunDetail(name, runId)
    } catch (err: any) {
      toast.error(err.message || 'Resume failed')
    }
  }

  const handleConfirmation = async (callID: string, choice: string) => {
    if (!name || !runId) return
    setResolvingConfirmation(callID)
    try {
      await resolveWorkflowConfirmation(name, runId, callID, choice)
      await fetchRunDetail(name, runId)
      toast.success(choice ? 'Confirmation submitted' : 'Tool execution denied')
    } catch (err: any) {
      toast.error(err.message || 'Confirmation failed')
    } finally {
      setResolvingConfirmation(null)
    }
  }

  const handleCleanup = async () => {
    if (
      !name ||
      !runId ||
      !window.confirm('Remove this run worktree? The audit history will be kept.')
    )
      return
    setCleaningUp(true)
    try {
      await cleanupRun(name, runId)
      await fetchRunDetail(name, runId)
      toast.success('Worktree removed')
    } catch (err: any) {
      toast.error(err.message || 'Worktree cleanup failed')
    } finally {
      setCleaningUp(false)
    }
  }

  // Build node states map from run data
  const nodeStates = useMemo(() => {
    const map: Record<string, NodeRunState> = {}
    activeRunDetail?.node_runs?.forEach((nr) => {
      map[nr.node_id] = nr.state
    })
    return map
  }, [activeRunDetail])

  // Entry nodes
  const entryNodes = useMemo(() => {
    return activeWorkflowGraph.nodes
      .filter((n) => !activeWorkflowGraph.edges.some((e) => e.target === n.id))
      .map((n) => n.id)
  }, [activeWorkflowGraph])

  // Progress
  const totalNodes = activeRunDetail?.node_count || 0
  const completedNodes = activeRunDetail?.completed_count || 0
  const isRunning =
    activeRunDetail?.status === 'running' ||
    activeRunDetail?.status === 'preparing_worktree' ||
    activeRunDetail?.status === 'pause_requested' ||
    activeRunDetail?.status === 'resuming'
  const isPaused = activeRunDetail?.status === 'paused' || activeRunDetail?.status === 'interrupted'

  // Elapsed time
  const elapsed = useMemo(() => {
    if (!activeRunDetail?.started_at) return ''
    const start = new Date(activeRunDetail.started_at).getTime()
    const end = activeRunDetail.finished_at ? new Date(activeRunDetail.finished_at).getTime() : now
    const sec = Math.round((end - start) / 1000)
    const min = Math.floor(sec / 60)
    const s = sec % 60
    return `${min}:${s.toString().padStart(2, '0')}`
  }, [activeRunDetail, now])

  return (
    <div className="flex h-full flex-col bg-background text-foreground overflow-hidden">
      {/* Header */}
      <header
        className={cn(
          'shrink-0 flex items-center justify-between border-b border-border/60 bg-card/20 px-6 py-2.5',
          sidebarCollapsed && 'pl-[115px]'
        )}
      >
        <div className="flex items-center gap-4 min-w-0">
          <button
            type="button"
            onClick={() => navigate(`/workflows/${name}`)}
            className="rounded-lg p-1.5 text-muted-foreground hover:text-foreground hover:bg-muted/40 transition-colors shrink-0"
          >
            <ArrowLeft className="h-4 w-4" />
          </button>
          <div className="min-w-0">
            <h1 className="text-sm font-bold text-foreground truncate">
              {name} · {runId?.slice(0, 12)}
            </h1>
          </div>
          {activeRunDetail && (
            <WorkflowStatusBadge
              state={activeRunDetail.status}
              size="md"
              showDot
              showIcon
              showLabel
            />
          )}
        </div>

        <div className="flex items-center gap-2">
          {isRunning && (
            <>
              <button
                type="button"
                onClick={handlePause}
                className="flex items-center gap-1.5 rounded-lg border border-warning/30 px-3 py-2 text-xs text-warning hover:bg-warning/10 transition-colors"
              >
                <Pause className="h-3.5 w-3.5" /> Pause
              </button>
              <button
                type="button"
                onClick={handleCancel}
                disabled={cancelling}
                className="flex items-center gap-1.5 rounded-lg bg-rose-500/10 hover:bg-rose-500/20 disabled:opacity-40 border border-rose-500/25 px-3 py-2 text-xs font-semibold text-rose-500 transition-colors cursor-pointer disabled:cursor-not-allowed"
              >
                {cancelling ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  <Square className="h-3.5 w-3.5" />
                )}
                {t('common.cancel')}
              </button>
            </>
          )}
          {isPaused && activeRunDetail?.resume_available && (
            <button
              type="button"
              onClick={handleResume}
              className="flex items-center gap-1.5 rounded-lg border border-success/30 px-3 py-2 text-xs text-success hover:bg-success/10 transition-colors"
            >
              <Play className="h-3.5 w-3.5" /> Resume
            </button>
          )}
          {!isRunning && activeRunDetail?.restart_available && (
            <button
              type="button"
              onClick={async () => {
                if (!name || !runId) return
                const newID = await restartRun(name, runId)
                if (newID) navigate(`/workflows/${name}/runs/${newID}`)
              }}
              className="flex items-center gap-1.5 rounded-lg border border-border/60 px-3 py-2 text-xs text-muted-foreground hover:text-foreground hover:bg-muted/40 transition-colors"
            >
              <RotateCw className="h-3.5 w-3.5" /> Restart
            </button>
          )}
          {isPaused && (
            <button
              type="button"
              onClick={() => name && runId && abandonRun(name, runId)}
              className="flex items-center gap-1.5 rounded-lg border border-border/60 px-3 py-2 text-xs text-muted-foreground hover:text-foreground hover:bg-muted/40 transition-colors"
            >
              Abandon
            </button>
          )}
          {!isRunning && activeRunDetail?.cleanup_available && (
            <button
              type="button"
              onClick={handleCleanup}
              disabled={cleaningUp}
              className="flex items-center gap-1.5 rounded-lg border border-rose-500/25 px-3 py-2 text-xs text-rose-500 hover:bg-rose-500/10 disabled:opacity-40 transition-colors"
            >
              {cleaningUp ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Trash2 className="h-3.5 w-3.5" />
              )}{' '}
              Cleanup
            </button>
          )}
        </div>
      </header>

      {activeRunDetail?.confirmations
        ?.filter((confirmation) => confirmation.status === 'pending')
        .map((confirmation) => {
          const choices = confirmation.options?.length ? confirmation.options : ['yes']
          const busy = resolvingConfirmation === confirmation.call_id
          return (
            <div
              key={confirmation.call_id}
              className="shrink-0 border-b border-warning/30 bg-warning/5 px-6 py-3"
            >
              <div className="flex items-start justify-between gap-4">
                <div className="min-w-0">
                  <div className="text-xs font-semibold text-warning">
                    Tool confirmation required
                  </div>
                  <div className="mt-1 text-xs text-foreground/90 break-words">
                    {confirmation.prompt}
                  </div>
                  <div className="mt-1 text-[10px] font-mono text-muted-foreground">
                    {confirmation.tool_name || 'tool'}
                    {confirmation.node_run_id ? ` · ${confirmation.node_run_id}` : ''}
                  </div>
                </div>
                <div className="flex shrink-0 flex-wrap justify-end gap-2">
                  {choices.map((choice) => (
                    <button
                      key={choice}
                      type="button"
                      disabled={busy}
                      onClick={() => handleConfirmation(confirmation.call_id, choice)}
                      className="rounded-lg border border-success/30 px-3 py-1.5 text-xs text-success hover:bg-success/10 disabled:opacity-40"
                    >
                      {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : choice}
                    </button>
                  ))}
                  {confirmation.allow_in_session && !choices.includes('allow-in-session') && (
                    <button
                      type="button"
                      disabled={busy}
                      onClick={() => handleConfirmation(confirmation.call_id, 'allow-in-session')}
                      className="rounded-lg border border-signal/30 px-3 py-1.5 text-xs text-signal hover:bg-signal/10 disabled:opacity-40"
                    >
                      Allow in session
                    </button>
                  )}
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => handleConfirmation(confirmation.call_id, '')}
                    className="rounded-lg border border-rose-500/30 px-3 py-1.5 text-xs text-rose-500 hover:bg-rose-500/10 disabled:opacity-40"
                  >
                    Deny
                  </button>
                </div>
              </div>
            </div>
          )
        })}

      {/* Body */}
      <div className="flex flex-1 min-h-0 overflow-hidden">
        {/* Left: Live DAG */}
        <div className="flex-1 relative bg-background min-w-0">
          {!backendReady ? (
            <div className="flex items-center justify-center h-full">
              <BackendUnavailable
                onRetry={() => {
                  if (name && runId) {
                    void fetchRunDetail(name, runId)
                    void listWorkflowRunEvents(name, runId, 0, 200)
                      .then((response) => setEvents(response.data || []))
                      .catch(() => setEvents([]))
                  }
                }}
              />
            </div>
          ) : activeRunDetailLoading ? (
            <div className="flex items-center justify-center h-full">
              <Loader2 className="h-8 w-8 text-signal animate-spin" />
            </div>
          ) : (
            <>
              <DAGPreview
                nodes={activeWorkflowGraph.nodes}
                edges={activeWorkflowGraph.edges}
                entryNodes={entryNodes}
                nodeStates={nodeStates}
              />

              {/* Progress overlay */}
              <div className="absolute top-4 left-4">
                <div className="rounded-xl bg-card/90 backdrop-blur-md border border-border/60 p-3 space-y-2 shadow-lg min-w-[200px]">
                  <div className="text-xs font-semibold text-foreground">
                    {t('workflow.progress', {
                      completed: String(completedNodes),
                      total: String(totalNodes),
                    })}
                  </div>
                  <div className="relative h-1.5 w-full overflow-hidden rounded-full bg-muted">
                    <div
                      className="h-full bg-signal transition-all duration-700 rounded-full"
                      style={{
                        width: `${totalNodes > 0 ? Math.round((completedNodes / totalNodes) * 100) : 0}%`,
                      }}
                    />
                  </div>
                  {elapsed && (
                    <div className="flex items-center gap-1 text-[10px] text-muted-foreground font-mono">
                      <Clock className="h-3 w-3" />
                      {elapsed} {t('workflow.elapsed')}
                    </div>
                  )}
                </div>
              </div>
            </>
          )}
        </div>

        {/* Right: Detail Panel */}
        <div className="w-[420px] shrink-0 border-l border-border/40 bg-card/5 flex flex-col min-h-0">
          {/* Tabs */}
          <div className="shrink-0 flex border-b border-border/40 bg-muted/10">
            {(['timeline', 'nodeRuns', 'outputs', 'audit'] as const).map((tab) => (
              <button
                key={tab}
                type="button"
                onClick={() => setActiveTab(tab)}
                className={cn(
                  'flex-1 px-4 py-2.5 text-[10px] font-bold uppercase tracking-wider font-mono transition-colors',
                  activeTab === tab
                    ? 'text-foreground border-b-2 border-primary bg-card/30'
                    : 'text-muted-foreground hover:text-foreground hover:bg-muted/20'
                )}
              >
                {t(
                  `workflow.${tab === 'nodeRuns' ? 'nodeRuns' : tab === 'outputs' ? 'outputsTab' : tab === 'audit' ? 'audit' : 'timeline'}`
                )}
              </button>
            ))}
          </div>

          {/* Tab content */}
          <div className="flex-1 overflow-y-auto">
            {activeTab === 'timeline' && (
              <TimelineTab nodeRuns={activeRunDetail?.node_runs || []} />
            )}
            {activeTab === 'nodeRuns' && (
              <NodeRunsTab nodeRuns={activeRunDetail?.node_runs || []} />
            )}
            {activeTab === 'outputs' && (
              <OutputsTab outputs={activeRunDetail?.terminal_outputs || []} />
            )}
            {activeTab === 'audit' && <AuditTab events={events} />}
          </div>
        </div>
      </div>
    </div>
  )
}
