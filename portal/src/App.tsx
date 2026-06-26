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
              <span className="text-base font-normal opacity-60">等待连接...</span>
            </span>
          ) : (
            mainValue
          )}
        </span>
        <span className="text-xs" style={{ color: 'var(--md-on-surface-variant)' }}>
          {isEmpty ? '启动后自动回连' : subValue}
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
        已连接
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
        重连中...
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
      未连接
    </span>
  )
}

// ─── Agent State Badge ───
function AgentStateBadge({ state }: { state: AgentState }) {
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
        运行中
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
        空闲
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
      已停止
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
  const [connStatus, setConnStatus] = useState<ConnectionStatus>('disconnected')
  const [runtime, setRuntime] = useState<RuntimeStatus | null>(null)
  const [agents, setAgents] = useState<AgentInfo[]>([])

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
              SoloQueue 状态中心
            </h1>
            <p className="text-xs font-mono truncate" style={{ color: 'var(--md-on-surface-variant)' }}>
              本地只读监控面板
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2 shrink-0">
          <ThemeToggle />
          <ConnectionBadge status={connStatus} />
        </div>
      </header>

      {/* ═══ Main Content ═══ */}
      <main className="flex-1 w-full max-w-7xl mx-auto px-4 sm:px-6 py-6 space-y-6">
        {/* ─── Metric Cards ─── */}
        <section className="metric-grid">
          <MetricCard
            title="活跃智能体"
            icon={<Bot className="h-5 w-5" />}
            iconColor="color: var(--md-primary)"
            mainValue={
              isConnected
                ? `${runningAgents}`
                : undefined
            }
            subValue={isConnected ? `共 ${totalAgents} 个注册智能体` : undefined}
            detail={isConnected ? `运行中: ${runningAgents} · 空闲: ${runtime?.idle_agents ?? 0}` : undefined}
            isEmpty={!isConnected}
          />

          <MetricCard
            title="Token 消耗"
            icon={<Cpu className="h-5 w-5" />}
            iconColor="color: var(--md-tertiary)"
            mainValue={isConnected ? formatTokenCount(totalTokens) : undefined}
            subValue={isConnected ? '累计 Token 总量' : undefined}
            detail={isConnected ? `输入: ${formatTokenCount(promptTokens)} · 输出: ${formatTokenCount(outputTokens)}` : undefined}
            isEmpty={!isConnected}
          />

          <MetricCard
            title="上下文占用"
            icon={<FileText className="h-5 w-5" />}
            iconColor="color: var(--md-secondary)"
            mainValue={isConnected ? `${contextPct}%` : undefined}
            subValue={isConnected ? '当前上下文窗口使用率' : undefined}
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
            title="系统异常"
            icon={<AlertCircle className="h-5 w-5" />}
            iconColor={totalErrors > 0 ? 'color: var(--md-error)' : 'color: var(--md-outline)'}
            mainValue={isConnected ? totalErrors : undefined}
            subValue={isConnected ? '智能体执行异常次数' : undefined}
            detail={
              isConnected && totalErrors > 0 && runtime?.phase
                ? `当前阶段: ${runtime.phase}`
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
              实时推理流
              <span
                className="text-xs font-normal normal-case"
                style={{ color: 'var(--md-outline)' }}
              >
                · {activeStreams.length} 个活跃智能体
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
                        第 #{stream.iteration} 轮
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
                            ── 思考过程 ──
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
                            ── 生成内容 ──
                          </span>
                          <span className="whitespace-pre-wrap" style={{ color: 'var(--md-on-surface)' }}>
                            {contentSegments}
                          </span>
                        </div>
                      )}
                      {!thinkingSegments && !contentSegments && (
                        <div className="flex items-center gap-2 h-full justify-center" style={{ color: 'var(--md-on-surface-variant)' }}>
                          <Loader2 className="h-4 w-4 animate-spin" />
                          <span>等待推理输出...</span>
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
              智能体状态总览
            </h2>
            <span className="text-xs font-mono" style={{ color: 'var(--md-on-surface-variant)' }}>
              注册总数：{isConnected ? agents.length : '--'}
            </span>
          </div>

          {/* Table body */}
          <div className="table-scroll">
            {!isConnected ? (
              <div className="flex flex-col items-center justify-center py-16 gap-3">
                <Loader2 className="h-6 w-6 animate-spin" style={{ color: 'var(--md-on-surface-variant)' }} />
                <span className="text-sm" style={{ color: 'var(--md-on-surface-variant)' }}>
                  正在连接服务器...
                </span>
              </div>
            ) : agents.length === 0 ? (
              <EmptyState
                icon={<Bot className="h-6 w-6" />}
                title="暂无已注册的智能体"
                description="启动 SoloQueue 服务器后，已注册的智能体将自动显示在此处。"
              />
            ) : (
              <table className="w-full text-left text-sm border-collapse">
                <thead>
                  <tr style={{ backgroundColor: 'var(--md-surface-container)' }}>
                    <th
                      className="px-4 sm:px-6 py-3 text-xs font-semibold uppercase tracking-wider whitespace-nowrap"
                      style={{ color: 'var(--md-on-surface-variant)' }}
                    >
                      智能体名称
                    </th>
                    <th
                      className="px-4 sm:px-6 py-3 text-xs font-semibold uppercase tracking-wider whitespace-nowrap"
                      style={{ color: 'var(--md-on-surface-variant)' }}
                    >
                      状态
                    </th>
                    <th
                      className="px-4 sm:px-6 py-3 text-xs font-semibold uppercase tracking-wider whitespace-nowrap hidden sm:table-cell"
                      style={{ color: 'var(--md-on-surface-variant)' }}
                    >
                      所属组
                    </th>
                    <th
                      className="px-4 sm:px-6 py-3 text-xs font-semibold uppercase tracking-wider whitespace-nowrap hidden md:table-cell"
                      style={{ color: 'var(--md-on-surface-variant)' }}
                    >
                      模型
                    </th>
                    <th
                      className="px-4 sm:px-6 py-3 text-xs font-semibold uppercase tracking-wider whitespace-nowrap hidden lg:table-cell"
                      style={{ color: 'var(--md-on-surface-variant)' }}
                    >
                      任务级别
                    </th>
                    <th
                      className="px-4 sm:px-6 py-3 text-xs font-semibold uppercase tracking-wider whitespace-nowrap text-right"
                      style={{ color: 'var(--md-on-surface-variant)' }}
                    >
                      异常
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
                              队长
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
                        {agent.group || '全局'}
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

        {/* ─── Footer Info ─── */}
        {isConnected && runtime && (
          <div
            className="flex flex-wrap items-center gap-x-4 gap-y-1 px-1 py-2 text-xs"
            style={{ color: 'var(--md-outline)' }}
          >
            <span className="flex items-center gap-1">
              <Layers className="h-3 w-3" />
              阶段: {runtime.phase || '运行中'}
            </span>
            <span className="flex items-center gap-1">
              <Database className="h-3 w-3" />
              Cache: 命中 {formatTokenCount(runtime.cache_hit_tokens)} / 未命中 {formatTokenCount(runtime.cache_miss_tokens)}
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
          <span>SoloQueue 状态中心 · 仅限本地访问 · 只读模式</span>
          <span>
            智能体 {isConnected ? totalAgents : '--'} ·{' '}
            {isConnected ? `Token ${formatTokenCount(totalTokens)}` : '未连接'}
          </span>
        </div>
      </footer>
    </div>
  )
}
