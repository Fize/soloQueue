import { useEffect, useState, useRef } from 'react'
import {
  Bot,
  Cpu,
  Activity,
  AlertCircle,
  ShieldAlert,
  Terminal,
  FileText,
  CheckCircle2,
  Wifi,
  WifiOff,
  Loader2,
  Database,
  Layers,
  RefreshCw,
} from 'lucide-react'
import { ThemeToggle } from './theme'
import { useTranslation } from './i18n'

// ─── Types ───
type AgentState = 'idle' | 'processing' | 'stopping' | 'stopped'
type ConnectionStatus = 'connected' | 'disconnected' | 'reconnecting'

interface AgentInfo {
  id: string
  name: string
  state: AgentState
  model_id: string
  provider_id: string
  group: string
  is_leader: boolean
  task_level: string
  error_count: number
  last_error: string
}

interface Segment {
  type: 'thinking' | 'content' | 'tool_call'
  text?: string
  name?: string
  result?: string
  error?: string
}

interface AgentStreamState {
  agent_id: string
  processing: boolean
  segments: Segment[]
  iteration: number
}

interface CronTaskStatus {
  id: string
  title: string
  task_level: 'L0' | 'L1' | 'L2' | 'L3'
  expression: string
  instruction: string
  target_agent: string
  status: string // active, paused, completed, running, failed
  last_run_at: string | null
  next_run_at: string
  is_one_time: boolean
}

interface RuntimeStatus {
  phase: string
  prompt_tokens: number
  output_tokens: number
  cache_hit_tokens: number
  cache_miss_tokens: number
  context_pct: number
  total_agents: number
  running_agents: number
  idle_agents: number
  total_errors: number
  agent_streams: Record<string, AgentStreamState>
}

// ─── Constants ───
const RECONNECT_DELAY = 2000

// ─── Helpers ───
function formatTokenCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`
  return String(n)
}

// ─── Metric Card ───
interface MetricCardProps {
  title: string
  icon: React.ReactNode
  iconColor: string
  mainValue: string | number | undefined
  subValue: string | undefined
  detail?: string
  progress?: number
  progressColor?: string
  isEmpty?: boolean
}

function MetricCard({ title, icon, iconColor, mainValue, subValue, detail, progress, progressColor, isEmpty }: MetricCardProps) {
  return (
    <div
      className="rounded-xl p-5 flex flex-col gap-2 animate-slide-up"
      style={{
        backgroundColor: 'var(--md-surface-container-low)',
        boxShadow: 'var(--md-elevation-1)',
      }}
    >
      <div className="flex items-center justify-between">
        <span className="text-sm font-medium" style={{ color: 'var(--md-on-surface-variant)' }}>
          {title}
        </span>
        <span className={iconColor as string}>{icon}</span>
      </div>

      <div className="flex flex-col gap-0.5">
        <span
          className={`text-2xl font-bold tabular-nums tracking-tight ${
            isEmpty ? 'opacity-40' : ''
          }`}
          style={{ color: 'var(--md-on-surface)' }}
        >
          {isEmpty ? (
            <span className="inline-flex items-center gap-2">
              <Loader2 className="h-4 w-4 animate-spin" />
              <span className="text-base font-normal opacity-60">Waiting for connection...</span>
            </span>
          ) : (
            mainValue
          )}
        </span>
        <span className="text-xs" style={{ color: 'var(--md-on-surface-variant)' }}>
          {isEmpty ? 'Auto-reconnect after startup' : subValue}
        </span>
      </div>

      {detail && !isEmpty && (
        <span className="text-xs font-mono" style={{ color: 'var(--md-outline)' }}>
          {detail}
        </span>
      )}

      {progress !== undefined && !isEmpty && (
        <div className="w-full h-1.5 rounded-full overflow-hidden mt-1" style={{ backgroundColor: 'var(--md-surface-container-highest)' }}>
          <div
            className="h-full rounded-full transition-all duration-500 ease-out"
            style={{
              width: `${Math.min(progress, 100)}%`,
              backgroundColor: progressColor ?? 'var(--md-primary)',
            }}
          />
        </div>
      )}
    </div>
  )
}

// ─── Connection Badge ───
function ConnectionBadge({ status }: { status: ConnectionStatus }) {
  const { t } = useTranslation()
  if (status === 'connected') {
    return (
      <span
        className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-semibold"
        style={{
          backgroundColor: 'color-mix(in srgb, var(--md-success) 12%, transparent)',
          color: 'var(--md-success)',
        }}
      >
        <span className="relative flex h-2 w-2">
          <span
            className="absolute inline-flex h-full w-full rounded-full opacity-75 animate-ping"
            style={{ backgroundColor: 'var(--md-success)' }}
          />
          <span
            className="relative inline-flex h-2 w-2 rounded-full"
            style={{ backgroundColor: 'var(--md-success)' }}
          />
        </span>
        <Wifi className="h-3.5 w-3.5" />
        {t('connection.connected')}
      </span>
    )
  }

  if (status === 'reconnecting') {
    return (
      <span
        className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-semibold"
        style={{
          backgroundColor: 'color-mix(in srgb, var(--md-warning) 12%, transparent)',
          color: 'var(--md-warning)',
        }}
      >
        <RefreshCw className="h-3.5 w-3.5 animate-spin" />
        {t('connection.reconnecting')}
      </span>
    )
  }

  return (
    <span
      className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-semibold"
      style={{
        backgroundColor: 'color-mix(in srgb, var(--md-outline) 12%, transparent)',
        color: 'var(--md-outline)',
      }}
    >
      <WifiOff className="h-3.5 w-3.5" />
      {t('connection.disconnected')}
    </span>
  )
}

// ─── Agent State Badge ───
function AgentStateBadge({ state }: { state: AgentState }) {
  const { t } = useTranslation()
  if (state === 'processing') {
    return (
      <span
        className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-semibold"
        style={{
          backgroundColor: 'color-mix(in srgb, var(--md-primary) 12%, transparent)',
          color: 'var(--md-primary)',
        }}
      >
        <span className="relative flex h-1.5 w-1.5">
          <span
            className="absolute inline-flex h-full w-full rounded-full opacity-75 animate-ping"
            style={{ backgroundColor: 'var(--md-primary)' }}
          />
          <span className="relative inline-flex h-1.5 w-1.5 rounded-full" style={{ backgroundColor: 'var(--md-primary)' }} />
        </span>
        {t('table.badges.processing')}
      </span>
    )
  }

  if (state === 'idle') {
    return (
      <span
        className="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-semibold"
        style={{
          backgroundColor: 'color-mix(in srgb, var(--md-outline) 8%, transparent)',
          color: 'var(--md-on-surface-variant)',
        }}
      >
        <span className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: 'var(--md-outline)' }} />
        {t('table.badges.idle')}
      </span>
    )
  }

  return (
    <span
      className="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-semibold"
      style={{
        backgroundColor: 'color-mix(in srgb, var(--md-error) 12%, transparent)',
        color: 'var(--md-error)',
      }}
    >
      <span className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: 'var(--md-error)' }} />
      {t('table.badges.stopped')}
    </span>
  )
}

// ─── Empty State ───
function EmptyState({ icon, title, description }: { icon: React.ReactNode; title: string; description: string }) {
  return (
    <div className="flex flex-col items-center justify-center py-16 gap-3 text-center px-6">
      <div
        className="w-12 h-12 rounded-full flex items-center justify-center"
        style={{ backgroundColor: 'var(--md-surface-container-high)' }}
      >
        <span style={{ color: 'var(--md-on-surface-variant)' }}>{icon}</span>
      </div>
      <span className="text-base font-medium" style={{ color: 'var(--md-on-surface)' }}>
        {title}
      </span>
      <span className="text-sm max-w-xs" style={{ color: 'var(--md-on-surface-variant)' }}>
        {description}
      </span>
    </div>
  )
}

// ════════════════════════════════════════════════════════════
//  App
// ════════════════════════════════════════════════════════════
export default function App() {
  const { t, language, setLanguage } = useTranslation()
  const [connStatus, setConnStatus] = useState<ConnectionStatus>('disconnected')
  const [runtime, setRuntime] = useState<RuntimeStatus | null>(null)
  const [agents, setAgents] = useState<AgentInfo[]>([])
  const [cronTasks, setCronTasks] = useState<CronTaskStatus[]>([])

  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const mountedRef = useRef(true)

  const connect = async () => {
    if (wsRef.current) return

    let token = ''
    try {
      const res = await fetch('/api/auth/token')
      if (res.ok) {
        const data = await res.json()
        token = data.token
      }
    } catch {
      // server might not have auth, continue without token
    }

    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    let url = `${proto}//${window.location.host}/ws`
    if (token) url += `?token=${encodeURIComponent(token)}`

    const ws = new WebSocket(url)
    wsRef.current = ws

    ws.onopen = () => {
      if (mountedRef.current) setConnStatus('connected')
    }

    ws.onmessage = (event) => {
      if (event.data === 'ping') {
        ws.send('pong')
        return
      }
      try {
        const msg = JSON.parse(event.data)
        if (msg.type === 'state') {
          if (msg.runtime) setRuntime(msg.runtime)
          if (msg.agents?.agents) setAgents(msg.agents.agents)
          if (msg.cron_tasks) setCronTasks(msg.cron_tasks)
        }
      } catch {
        // ignore malformed messages
      }
    }

    ws.onclose = () => {
      wsRef.current = null
      if (mountedRef.current) {
        setConnStatus('reconnecting')
        scheduleReconnect()
      }
    }

    ws.onerror = () => {
      ws.close()
    }
  }

  const scheduleReconnect = () => {
    if (reconnectTimerRef.current) clearTimeout(reconnectTimerRef.current)
    reconnectTimerRef.current = setTimeout(() => {
      if (mountedRef.current) connect()
    }, RECONNECT_DELAY)
  }

  useEffect(() => {
    mountedRef.current = true
    connect()
    return () => {
      mountedRef.current = false
      if (wsRef.current) wsRef.current.close()
      if (reconnectTimerRef.current) clearTimeout(reconnectTimerRef.current)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const isConnected = connStatus === 'connected'
  const activeStreams = runtime?.agent_streams
    ? Object.entries(runtime.agent_streams).filter(([_, s]) => s.processing)
    : []

  // Calculate token totals
  const promptTokens = runtime?.prompt_tokens ?? 0
  const outputTokens = runtime?.output_tokens ?? 0
  const totalTokens = promptTokens + outputTokens
  const contextPct = runtime?.context_pct ?? 0
  const totalErrors = runtime?.total_errors ?? 0
  const runningAgents = runtime?.running_agents ?? 0
  const totalAgents = runtime?.total_agents ?? 0

  return (
    <div
      className="min-h-screen flex flex-col transition-colors duration-250"
      style={{ backgroundColor: 'var(--md-background)', color: 'var(--md-on-surface)' }}
    >
      {/* ═══ Header ═══ */}
      <header
        className="sticky top-0 z-50 px-4 sm:px-6 py-3 flex items-center justify-between border-b transition-colors duration-250"
        style={{
          backgroundColor: 'color-mix(in srgb, var(--md-surface) 80%, transparent)',
          borderColor: 'var(--md-outline-variant)',
          backdropFilter: 'blur(12px)',
        }}
      >
        <div className="flex items-center gap-3 min-w-0">
          <div
            className="h-9 w-9 rounded-xl flex items-center justify-center shrink-0"
            style={{
              backgroundColor: 'color-mix(in srgb, var(--md-primary) 12%, transparent)',
              color: 'var(--md-primary)',
            }}
          >
            <Activity className="h-5 w-5" />
          </div>
          <div className="min-w-0">
            <h1 className="text-base font-bold tracking-tight truncate" style={{ color: 'var(--md-on-surface)' }}>
              {t('header.title')}
            </h1>
            <p className="text-xs font-mono truncate" style={{ color: 'var(--md-on-surface-variant)' }}>
              {t('header.desc')}
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2 shrink-0">
          <button
            onClick={() => setLanguage(language === 'en' ? 'zh' : 'en')}
            className="px-2.5 py-1 text-xs font-semibold rounded-full border border-[var(--md-outline-variant)] bg-[var(--md-surface-container-high)] hover:bg-[var(--md-surface-container-highest)] cursor-pointer text-[var(--md-on-surface)] transition-all select-none"
          >
            {language === 'en' ? 'EN' : '中文'}
          </button>
          <ThemeToggle />
          <ConnectionBadge status={connStatus} />
        </div>
      </header>

      {/* ═══ Main Content ═══ */}
      <main className="flex-1 w-full max-w-7xl mx-auto px-4 sm:px-6 py-6 space-y-6">
        {/* ─── Metric Cards ─── */}
        <section className="metric-grid">
          <MetricCard
            title={t('metrics.activeAgents.title')}
            icon={<Bot className="h-5 w-5" />}
            iconColor="color: var(--md-primary)"
            mainValue={
              isConnected
                ? `${runningAgents}`
                : undefined
            }
            subValue={isConnected ? t('metrics.activeAgents.sub', { count: totalAgents }) : undefined}
            detail={isConnected ? t('metrics.activeAgents.detail', { running: runningAgents, idle: runtime?.idle_agents ?? 0 }) : undefined}
            isEmpty={!isConnected}
          />

          <MetricCard
            title={t('metrics.tokenUsage.title')}
            icon={<Cpu className="h-5 w-5" />}
            iconColor="color: var(--md-tertiary)"
            mainValue={isConnected ? formatTokenCount(totalTokens) : undefined}
            subValue={isConnected ? t('metrics.tokenUsage.sub') : undefined}
            detail={isConnected ? t('metrics.tokenUsage.detail', { input: formatTokenCount(promptTokens), output: formatTokenCount(outputTokens) }) : undefined}
            isEmpty={!isConnected}
          />

          <MetricCard
            title={t('metrics.contextOccupancy.title')}
            icon={<FileText className="h-5 w-5" />}
            iconColor="color: var(--md-secondary)"
            mainValue={isConnected ? `${contextPct}%` : undefined}
            subValue={isConnected ? t('metrics.contextOccupancy.sub') : undefined}
            progress={isConnected ? contextPct : undefined}
            progressColor={
              contextPct > 85
                ? 'var(--md-error)'
                : contextPct > 60
                  ? 'var(--md-warning)'
                  : 'var(--md-primary)'
            }
            isEmpty={!isConnected}
          />

          <MetricCard
            title={t('metrics.systemErrors.title')}
            icon={<AlertCircle className="h-5 w-5" />}
            iconColor={totalErrors > 0 ? 'color: var(--md-error)' : 'color: var(--md-outline)'}
            mainValue={isConnected ? totalErrors : undefined}
            subValue={isConnected ? t('metrics.systemErrors.sub') : undefined}
            detail={
              isConnected && totalErrors > 0 && runtime?.phase
                ? t('metrics.systemErrors.detail', { phase: runtime.phase })
                : undefined
            }
            isEmpty={!isConnected}
          />
        </section>

        {/* ─── Live Inference Stream ─── */}
        {isConnected && activeStreams.length > 0 && (
          <section className="space-y-3 animate-slide-up">
            <h2
              className="text-sm font-semibold tracking-wider uppercase flex items-center gap-2"
              style={{ color: 'var(--md-on-surface-variant)' }}
            >
              <Terminal className="h-4 w-4" style={{ color: 'var(--md-primary)' }} />
              {t('stream.title')}
              <span
                className="text-xs font-normal normal-case"
                style={{ color: 'var(--md-outline)' }}
              >
                {t('stream.activeAgents', { count: activeStreams.length })}
              </span>
            </h2>

            <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
              {activeStreams.map(([id, stream], idx) => {
                const agentName = agents.find(a => a.id === id)?.name || id.slice(0, 8)
                const thinkingSegments = stream.segments
                  ?.filter(s => s.type === 'thinking')
                  .map(s => s.text)
                  .join('') || ''
                const contentSegments = stream.segments
                  ?.filter(s => s.type === 'content')
                  .map(s => s.text)
                  .join('') || ''

                return (
                  <div
                    key={id}
                    className="rounded-xl overflow-hidden flex flex-col h-80 animate-slide-up"
                    style={{
                      backgroundColor: 'var(--md-surface-container-low)',
                      boxShadow: 'var(--md-elevation-1)',
                      animationDelay: `${idx * 80}ms`,
                    }}
                  >
                    {/* Stream header */}
                    <div
                      className="px-4 py-3 border-b flex items-center justify-between shrink-0"
                      style={{ borderColor: 'var(--md-outline-variant)', backgroundColor: 'var(--md-surface-container)' }}
                    >
                      <span className="text-sm font-semibold flex items-center gap-2" style={{ color: 'var(--md-on-surface)' }}>
                        <span className="relative flex h-2 w-2">
                          <span
                            className="absolute inline-flex h-full w-full rounded-full opacity-75 animate-ping"
                            style={{ backgroundColor: 'var(--md-primary)' }}
                          />
                          <span
                            className="relative inline-flex h-2 w-2 rounded-full"
                            style={{ backgroundColor: 'var(--md-primary)' }}
                          />
                        </span>
                        {agentName}
                      </span>
                      <span className="text-xs font-mono" style={{ color: 'var(--md-on-surface-variant)' }}>
                        {t('stream.iteration', { iteration: stream.iteration })}
                      </span>
                    </div>

                    {/* Stream body */}
                    <div className="flex-1 p-4 overflow-y-auto font-mono text-xs leading-relaxed space-y-3">
                      {thinkingSegments && (
                        <div
                          className="border-l-2 pl-3"
                          style={{ borderColor: 'color-mix(in srgb, var(--md-primary) 50%, transparent)' }}
                        >
                          <span className="font-semibold block mb-1 text-xs" style={{ color: 'var(--md-primary)' }}>
                            {t('stream.thinking')}
                          </span>
                          <span className="whitespace-pre-wrap" style={{ color: 'var(--md-on-surface-variant)' }}>
                            {thinkingSegments}
                          </span>
                        </div>
                      )}
                      {contentSegments && (
                        <div
                          className="border-l-2 pl-3"
                          style={{ borderColor: 'color-mix(in srgb, var(--md-tertiary) 50%, transparent)' }}
                        >
                          <span className="font-semibold block mb-1 text-xs" style={{ color: 'var(--md-tertiary)' }}>
                            {t('stream.content')}
                          </span>
                          <span className="whitespace-pre-wrap" style={{ color: 'var(--md-on-surface)' }}>
                            {contentSegments}
                          </span>
                        </div>
                      )}
                      {!thinkingSegments && !contentSegments && (
                        <div className="flex items-center gap-2 h-full justify-center" style={{ color: 'var(--md-on-surface-variant)' }}>
                          <Loader2 className="h-4 w-4 animate-spin" />
                          <span>{t('stream.waiting')}</span>
                        </div>
                      )}
                    </div>
                  </div>
                )
              })}
            </div>
          </section>
        )}

        {/* ─── Agent Status Table ─── */}
        <section
          className="rounded-xl overflow-hidden animate-slide-up"
          style={{
            backgroundColor: 'var(--md-surface-container-low)',
            boxShadow: 'var(--md-elevation-1)',
          }}
        >
          {/* Table header */}
          <div
            className="px-4 sm:px-6 py-4 border-b flex items-center justify-between flex-wrap gap-2"
            style={{ borderColor: 'var(--md-outline-variant)' }}
          >
            <h2 className="text-sm font-semibold flex items-center gap-2" style={{ color: 'var(--md-on-surface)' }}>
              <Database className="h-4 w-4" style={{ color: 'var(--md-primary)' }} />
              {t('table.title')}
            </h2>
            <span className="text-xs font-mono" style={{ color: 'var(--md-on-surface-variant)' }}>
              {t('table.totalRegistered', { count: isConnected ? agents.length : 0 })}
            </span>
          </div>

          {/* Table body */}
          <div className="table-scroll">
            {!isConnected ? (
              <div className="flex flex-col items-center justify-center py-16 gap-3">
                <Loader2 className="h-6 w-6 animate-spin" style={{ color: 'var(--md-on-surface-variant)' }} />
                <span className="text-sm" style={{ color: 'var(--md-on-surface-variant)' }}>
                  {t('metrics.connecting')}
                </span>
              </div>
            ) : agents.length === 0 ? (
              <EmptyState
                icon={<Bot className="h-6 w-6" />}
                title={t('table.emptyTitle')}
                description={t('table.emptyDesc')}
              />
            ) : (
              <table className="w-full text-left text-sm border-collapse">
                <thead>
                  <tr style={{ backgroundColor: 'var(--md-surface-container)' }}>
                    <th
                      className="px-4 sm:px-6 py-3 text-xs font-semibold uppercase tracking-wider whitespace-nowrap"
                      style={{ color: 'var(--md-on-surface-variant)' }}
                    >
                      {t('table.cols.name')}
                    </th>
                    <th
                      className="px-4 sm:px-6 py-3 text-xs font-semibold uppercase tracking-wider whitespace-nowrap"
                      style={{ color: 'var(--md-on-surface-variant)' }}
                    >
                      {t('table.cols.status')}
                    </th>
                    <th
                      className="px-4 sm:px-6 py-3 text-xs font-semibold uppercase tracking-wider whitespace-nowrap hidden sm:table-cell"
                      style={{ color: 'var(--md-on-surface-variant)' }}
                    >
                      {t('table.cols.group')}
                    </th>
                    <th
                      className="px-4 sm:px-6 py-3 text-xs font-semibold uppercase tracking-wider whitespace-nowrap hidden md:table-cell"
                      style={{ color: 'var(--md-on-surface-variant)' }}
                    >
                      {t('table.cols.model')}
                    </th>
                    <th
                      className="px-4 sm:px-6 py-3 text-xs font-semibold uppercase tracking-wider whitespace-nowrap hidden lg:table-cell"
                      style={{ color: 'var(--md-on-surface-variant)' }}
                    >
                      {t('table.cols.level')}
                    </th>
                    <th
                      className="px-4 sm:px-6 py-3 text-xs font-semibold uppercase tracking-wider whitespace-nowrap text-right"
                      style={{ color: 'var(--md-on-surface-variant)' }}
                    >
                      {t('table.cols.errors')}
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {agents.map((agent, idx) => (
                    <tr
                      key={agent.id}
                      className="transition-colors duration-150"
                      style={{
                        borderTop: '1px solid var(--md-outline-variant)',
                        animationDelay: `${idx * 30}ms`,
                      }}
                      onMouseEnter={(e) => {
                        e.currentTarget.style.backgroundColor = 'color-mix(in srgb, var(--md-on-surface) 5%, transparent)'
                      }}
                      onMouseLeave={(e) => {
                        e.currentTarget.style.backgroundColor = 'transparent'
                      }}
                    >
                      <td className="px-4 sm:px-6 py-4">
                        <div className="flex items-center gap-2">
                          <span className="font-semibold text-sm" style={{ color: 'var(--md-on-surface)' }}>
                            {agent.name}
                          </span>
                          {agent.is_leader && (
                            <span
                              className="px-1.5 py-0.5 rounded text-[10px] font-bold"
                              style={{
                                backgroundColor: 'color-mix(in srgb, var(--md-warning) 12%, transparent)',
                                color: 'var(--md-warning)',
                              }}
                            >
                              {t('table.badges.leader')}
                            </span>
                          )}
                        </div>
                      </td>
                      <td className="px-4 sm:px-6 py-4">
                        <AgentStateBadge state={agent.state} />
                      </td>
                      <td
                        className="px-4 sm:px-6 py-4 text-xs font-mono hidden sm:table-cell"
                        style={{ color: 'var(--md-on-surface-variant)' }}
                      >
                        {agent.group || 'Global'}
                      </td>
                      <td
                        className="px-4 sm:px-6 py-4 text-xs font-mono hidden md:table-cell max-w-[160px] truncate"
                        style={{ color: 'var(--md-on-surface-variant)' }}
                        title={agent.model_id}
                      >
                        {agent.model_id}
                      </td>
                      <td className="px-4 sm:px-6 py-4 hidden lg:table-cell">
                        <span
                          className="px-2 py-0.5 rounded text-xs font-mono"
                          style={{
                            backgroundColor: 'var(--md-surface-container-high)',
                            color: 'var(--md-on-surface)',
                          }}
                        >
                          {agent.task_level}
                        </span>
                      </td>
                      <td className="px-4 sm:px-6 py-4 text-right">
                        {agent.error_count > 0 ? (
                          <span
                            className="inline-flex items-center gap-1 text-xs font-semibold"
                            style={{ color: 'var(--md-error)' }}
                            title={agent.last_error || undefined}
                          >
                            <ShieldAlert className="h-3.5 w-3.5" />
                            {agent.error_count}
                          </span>
                        ) : (
                          <span
                            className="inline-flex items-center gap-1 text-xs"
                            style={{ color: 'var(--md-on-surface-variant)' }}
                          >
                            <CheckCircle2 className="h-3.5 w-3.5" style={{ color: 'var(--md-success)' }} />
                            0
                          </span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </section>

        {/* ─── Scheduled Tasks ─── */}
        {isConnected && cronTasks.length > 0 && (
          <section
            className="rounded-xl overflow-hidden animate-slide-up"
            style={{
              backgroundColor: 'var(--md-surface-container-low)',
              boxShadow: 'var(--md-elevation-1)',
            }}
          >
            {/* Section header */}
            <div
              className="px-4 sm:px-6 py-4 border-b flex items-center justify-between flex-wrap gap-2"
              style={{ borderColor: 'var(--md-outline-variant)' }}
            >
              <h2 className="text-sm font-semibold flex items-center gap-2" style={{ color: 'var(--md-on-surface)' }}>
                <RefreshCw className="h-4 w-4" style={{ color: 'var(--md-primary)' }} />
                {t('cron.title')}
              </h2>
              <span className="text-xs font-mono" style={{ color: 'var(--md-on-surface-variant)' }}>
                {cronTasks.length === 1 ? t('cron.tasksCount', { count: 1 }) : t('cron.tasksCountPlural', { count: cronTasks.length })}
              </span>
            </div>

            {/* Task cards */}
            <div className="divide-y" style={{ borderColor: 'var(--md-outline-variant)' }}>
              {cronTasks.map((task) => {
                const statusColors: Record<string, { bg: string; fg: string }> = {
                  active:    { bg: 'color-mix(in srgb, var(--md-success) 12%, transparent)', fg: 'var(--md-success)' },
                  paused:    { bg: 'color-mix(in srgb, var(--md-warning) 12%, transparent)', fg: 'var(--md-warning)' },
                  completed: { bg: 'color-mix(in srgb, var(--md-outline) 12%, transparent)', fg: 'var(--md-outline)' },
                  running:   { bg: 'color-mix(in srgb, var(--md-primary) 12%, transparent)', fg: 'var(--md-primary)' },
                  failed:    { bg: 'color-mix(in srgb, var(--md-error) 12%, transparent)', fg: 'var(--md-error)' },
                }
                const sc = statusColors[task.status] || statusColors.completed
                const isL1 = task.target_agent === 'L1'
                const agentColor = isL1
                  ? { bg: 'color-mix(in srgb, #3b82f6 12%, transparent)', fg: '#3b82f6' }
                  : { bg: 'color-mix(in srgb, #22c55e 12%, transparent)', fg: '#22c55e' }

                return (
                  <div
                    key={task.id}
                    className="px-4 sm:px-6 py-3 flex items-start gap-3"
                    style={{ animationDelay: `${cronTasks.indexOf(task) * 30}ms` }}
                  >
                    {/* Instruction */}
                    <div className="flex-1 min-w-0">
                      <p className="text-sm font-medium truncate" style={{ color: 'var(--md-on-surface)' }}>
                        {task.title}
                      </p>
                      <p className="text-xs truncate mt-0.5" style={{ color: 'var(--md-on-surface-variant)' }}>
                        {task.instruction}
                      </p>
                      <div className="flex items-center gap-2 mt-1.5 flex-wrap">
                        <span
                          className="inline-flex items-center px-2 py-0.5 rounded text-[10px] font-semibold"
                          style={{
                            backgroundColor: 'color-mix(in srgb, var(--md-primary) 12%, transparent)',
                            color: 'var(--md-primary)',
                          }}
                        >
                          {task.task_level}
                        </span>
                        {/* Target agent badge */}
                        <span
                          className="inline-flex items-center px-2 py-0.5 rounded text-[10px] font-semibold"
                          style={{
                            backgroundColor: agentColor.bg,
                            color: agentColor.fg,
                          }}
                        >
                          {task.target_agent}
                        </span>
                        {/* Status badge */}
                        <span
                          className="inline-flex items-center px-2 py-0.5 rounded text-[10px] font-semibold"
                          style={{
                            backgroundColor: sc.bg,
                            color: sc.fg,
                          }}
                        >
                          {task.status}
                        </span>
                        {/* Next run */}
                        <span className="text-[10px] font-mono" style={{ color: 'var(--md-outline)' }}>
                          {t('cron.nextRun', { time: task.next_run_at || '--' })}
                        </span>
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          </section>
        )}

        {/* ─── Footer Info ─── */}
        {isConnected && runtime && (
          <div
            className="flex flex-wrap items-center gap-x-4 gap-y-1 px-1 py-2 text-xs"
            style={{ color: 'var(--md-outline)' }}
          >
            <span className="flex items-center gap-1">
              <Layers className="h-3 w-3" />
              {t('footerInfo.phase', { phase: runtime.phase || 'Running' })}
            </span>
            <span className="flex items-center gap-1">
              <Database className="h-3 w-3" />
              {t('footerInfo.cache', { hit: formatTokenCount(runtime.cache_hit_tokens), miss: formatTokenCount(runtime.cache_miss_tokens) })}
            </span>
          </div>
        )}
      </main>

      {/* ═══ Footer ═══ */}
      <footer
        className="border-t px-6 py-4 text-center text-xs"
        style={{ borderColor: 'var(--md-outline-variant)', color: 'var(--md-outline)' }}
      >
        <div className="max-w-7xl mx-auto flex flex-col sm:flex-row items-center justify-between gap-1">
          <span>{t('footer.banner')}</span>
          <span>
            {isConnected ? t('footer.summary', { agents: totalAgents, tokens: formatTokenCount(totalTokens) }) : t('footer.disconnected')}
          </span>
        </div>
      </footer>
    </div>
  )
}
