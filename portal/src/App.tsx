import { useEffect, useState, useRef, useCallback } from 'react'
import { Layers, Database } from 'lucide-react'
import { useTranslation } from './i18n'
import { Header } from './components/Header'
import { MetricsRow } from './components/MetricsRow'
import { AgentTable, type SupervisorInfo } from './components/AgentTable'
import { TokenStats } from './components/TokenStats'
import { CronSection } from './components/CronSection'
import { AgentModal } from './components/AgentModal'
import { NotificationToast } from './components/NotificationToast'
import type { NotificationPayload } from './components/NotificationToast'

// ─── Types ───
type ConnectionStatus = 'connected' | 'disconnected' | 'reconnecting'

interface AgentInfo {
  id: string
  instance_id: string
  name: string
  state: 'idle' | 'processing' | 'stopping' | 'stopped'
  model_id: string
  provider_id: string
  group: string
  is_leader: boolean
  task_type: string
  last_level?: string
  error_count: number
  last_error: string
  iteration?: number
}

interface Segment {
  type: 'thinking' | 'content' | 'tool_call'
  text?: string
  call_id?: string
  name?: string
  args?: string
  result?: string
  error?: string
  done?: boolean
  duration_ms?: number
}

interface AgentStreamState {
  agent_id: string
  processing: boolean
  segments: Segment[]
  iteration: number
  error?: string
}

interface CronTaskStatus {
  id: string
  title: string
  task_type: 'L0' | 'L1' | 'L2' | 'L3'
  expression: string
  instruction: string
  target_agent: string
  status: string
  last_run_at: string | null
  next_run_at: string
  is_one_time: boolean
}

// Architectural Decision: Websocket polling & reconnection strategy.
// Reconnect interval fixed at 2s to maintain low latency without overwhelming local Go server.
// Token fetch is graceful — if /api/auth/token fails (e.g., auth disabled in dev), fallback to unauthenticated WS.
const RECONNECT_DELAY = 2000
const MAX_NOTIFICATIONS = 3
const NOTIFICATION_TTL = 5000

export function formatTokenCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`
  return String(n)
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

// ════════════════════════════════════════════════════════════
//  App
// ════════════════════════════════════════════════════════════
export default function App() {
  const { t, language, setLanguage } = useTranslation()
  const [connStatus, setConnStatus] = useState<ConnectionStatus>('disconnected')
  const [runtime, setRuntime] = useState<RuntimeStatus | null>(null)
  const [agents, setAgents] = useState<AgentInfo[]>([])
  const [supervisors, setSupervisors] = useState<SupervisorInfo[] | null>(null)
  const [cronTasks, setCronTasks] = useState<CronTaskStatus[]>([])
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null)
  const [notifications, setNotifications] = useState<NotificationPayload[]>([])

  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const mountedRef = useRef(true)
  const notificationTimersRef = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map())

  // ── Add notification with auto-expire ──
  const addNotification = useCallback((payload: NotificationPayload) => {
    const id = `${payload.timestamp}-${Math.random()}`
    setNotifications(prev => {
      const next = [{ ...payload, timestamp: id }, ...prev].slice(0, MAX_NOTIFICATIONS)
      return next
    })
    // Auto-remove after TTL
    const timer = setTimeout(() => {
      setNotifications(prev => prev.filter(n => n.timestamp !== id))
      notificationTimersRef.current.delete(id)
    }, NOTIFICATION_TTL)
    notificationTimersRef.current.set(id, timer)
  }, [])

  // ── WebSocket connect ──
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
          if (msg.agents?.supervisors) setSupervisors(msg.agents.supervisors)
          if (msg.cron_tasks) setCronTasks(msg.cron_tasks)
        } else if (msg.type === 'notification') {
          addNotification({
            category: msg.category || 'system',
            level: msg.level || 'info',
            title: msg.title || '',
            body: msg.body || '',
            timestamp: msg.timestamp || new Date().toISOString(),
          })
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
      // Clear notification timers
      notificationTimersRef.current.forEach(t => clearTimeout(t))
      notificationTimersRef.current.clear()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // ── Derived state ──
  const isConnected = connStatus === 'connected'
  const promptTokens = runtime?.prompt_tokens ?? 0
  const outputTokens = runtime?.output_tokens ?? 0
  const totalTokens = promptTokens + outputTokens
  const contextPct = runtime?.context_pct ?? 0
  const totalErrors = runtime?.total_errors ?? 0
  const runningAgents = runtime?.running_agents ?? 0
  const totalAgents = runtime?.total_agents ?? 0
  const idleAgents = runtime?.idle_agents ?? 0

  const selectedAgent = agents.find(a => a.instance_id === selectedAgentId)
  const selectedStream = selectedAgentId ? runtime?.agent_streams?.[selectedAgentId] : undefined

  return (
    <div
      className="min-h-screen flex flex-col transition-colors duration-250"
      style={{ backgroundColor: 'var(--color-background)', color: 'var(--color-foreground)' }}
    >
      {/* ═══ Header ═══ */}
      <Header
        connStatus={connStatus}
        language={language}
        onToggleLanguage={() => setLanguage(language === 'en' ? 'zh' : 'en')}
      />

      {/* ═══ Main Content ═══ */}
      <main className="flex-1 w-full max-w-7xl mx-auto px-4 sm:px-6 py-6 space-y-6">
        {/* ─── Metric Cards ─── */}
        <MetricsRow
          isConnected={isConnected}
          runningAgents={runningAgents}
          totalAgents={totalAgents}
          idleAgents={idleAgents}
          totalTokens={totalTokens}
          promptTokens={promptTokens}
          outputTokens={outputTokens}
          contextPct={contextPct}
          totalErrors={totalErrors}
          phase={runtime?.phase}
        />

        {/* ─── Agent Status Table ─── */}
        <AgentTable
          agents={agents}
          supervisors={supervisors}
          isConnected={isConnected}
          onSelectAgent={(id) => setSelectedAgentId(id)}
          t={t}
        />

        {/* ─── Token Usage Statistics (collapsed by default) ─── */}
        {isConnected && <TokenStats />}

        {/* ─── Scheduled Tasks ─── */}
        {isConnected && (
          <CronSection tasks={cronTasks} t={t} />
        )}

        {/* ─── Footer Info ─── */}
        {isConnected && runtime && (
          <div
            className="flex flex-wrap items-center gap-x-4 gap-y-1 px-1 py-2 text-xs"
            style={{ color: 'var(--color-muted-foreground)' }}
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
        style={{ borderColor: 'var(--color-border)', color: 'var(--color-muted-foreground)' }}
      >
        <div className="max-w-7xl mx-auto flex flex-col sm:flex-row items-center justify-between gap-1">
          <span>{t('footer.banner')}</span>
          <span>
            {isConnected ? t('footer.summary', { agents: totalAgents, tokens: formatTokenCount(totalTokens) }) : t('footer.disconnected')}
          </span>
        </div>
      </footer>

      {/* ═══ Agent Detail Modal ═══ */}
      {selectedAgent && (
        <AgentModal
          agent={selectedAgent}
          stream={selectedStream}
          onClose={() => setSelectedAgentId(null)}
          t={t}
        />
      )}

      {/* ═══ Notification Toasts ═══ */}
      <NotificationToast notifications={notifications} />
    </div>
  )
}
