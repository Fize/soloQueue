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
  WifiOff 
} from 'lucide-react'

// 类型定义
type AgentState = 'idle' | 'processing' | 'stopping' | 'stopped'

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

export default function App() {
  const [status, setStatus] = useState<'connected' | 'disconnected' | 'reconnecting'>('disconnected')
  const [runtime, setRuntime] = useState<RuntimeStatus | null>(null)
  const [agents, setAgents] = useState<AgentInfo[]>([])
  
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // 连接 Go 服务器 WebSocket
  const connect = async () => {
    if (wsRef.current) return

    let token = ''
    try {
      const res = await fetch('/api/auth/token')
      if (res.ok) {
        const data = await res.json()
        token = data.token
      }
    } catch (e) {
      console.warn('获取认证令牌失败:', e)
    }

    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    let url = `${proto}//${window.location.host}/ws`
    if (token) {
      url += `?token=${encodeURIComponent(token)}`
    }

    const ws = new WebSocket(url)
    wsRef.current = ws

    ws.onopen = () => {
      setStatus('connected')
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
          if (msg.agents && msg.agents.agents) setAgents(msg.agents.agents)
        }
      } catch (err) {
        console.error('解析 WebSocket 消息失败:', err)
      }
    }

    ws.onclose = () => {
      wsRef.current = null
      setStatus('reconnecting')
      scheduleReconnect()
    }

    ws.onerror = () => {
      ws.close()
    }
  }

  const scheduleReconnect = () => {
    if (reconnectTimerRef.current) clearTimeout(reconnectTimerRef.current)
    reconnectTimerRef.current = setTimeout(() => {
      connect()
    }, 2000)
  }

  useEffect(() => {
    connect()
    return () => {
      if (wsRef.current) wsRef.current.close()
      if (reconnectTimerRef.current) clearTimeout(reconnectTimerRef.current)
    }
  }, [])

  return (
    <div className="min-h-screen bg-zinc-950 text-zinc-100 flex flex-col font-sans">
      {/* 顶部标题栏 */}
      <header className="border-b border-zinc-800 bg-zinc-900/50 backdrop-blur-md px-6 py-4 flex items-center justify-between sticky top-0 z-50">
        <div className="flex items-center gap-3">
          <div className="h-9 w-9 rounded-xl bg-indigo-600/10 border border-indigo-500/30 flex items-center justify-center text-indigo-400">
            <Activity className="h-5 w-5" />
          </div>
          <div>
            <h1 className="text-base font-bold tracking-tight">SoloQueue 状态中心</h1>
            <p className="text-xs text-zinc-400 font-mono">本地只读监控面板 · 127.0.0.1</p>
          </div>
        </div>

        <div className="flex items-center gap-2">
          {status === 'connected' ? (
            <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-semibold bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
              <Wifi className="h-3.5 w-3.5" /> 已连接
            </span>
          ) : status === 'reconnecting' ? (
            <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-semibold bg-amber-500/10 text-amber-400 border border-amber-500/20 animate-pulse">
              <Activity className="h-3.5 w-3.5" /> 重连中...
            </span>
          ) : (
            <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-semibold bg-zinc-500/10 text-zinc-400 border border-zinc-500/20">
              <WifiOff className="h-3.5 w-3.5" /> 未连接
            </span>
          )}
        </div>
      </header>

      {/* 主内容区 */}
      <main className="flex-1 max-w-7xl w-full mx-auto p-6 space-y-6">
        
        {/* 指标卡片 */}
        <section className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
          <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-5 flex flex-col justify-between">
            <div className="flex items-center justify-between text-zinc-400 mb-2">
              <span className="text-sm font-semibold">活跃智能体</span>
              <Bot className="h-5 w-5 text-indigo-400" />
            </div>
            <div>
              <div className="text-3xl font-extrabold font-mono text-zinc-50">
                {runtime?.running_agents ?? 0} <span className="text-sm text-zinc-500 font-normal">/ {runtime?.total_agents ?? 0}</span>
              </div>
              <p className="text-xs text-zinc-500 mt-1">正在工作区运行</p>
            </div>
          </div>

          <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-5 flex flex-col justify-between">
            <div className="flex items-center justify-between text-zinc-400 mb-2">
              <span className="text-sm font-semibold">Token 消耗</span>
              <Cpu className="h-5 w-5 text-sky-400" />
            </div>
            <div>
              <div className="text-3xl font-extrabold font-mono text-zinc-50">
                {(((runtime?.prompt_tokens ?? 0) + (runtime?.output_tokens ?? 0)) / 1000).toFixed(1)}k
              </div>
              <p className="text-xs text-zinc-500 mt-1">
                输入: {runtime?.prompt_tokens ?? 0} | 输出: {runtime?.output_tokens ?? 0}
              </p>
            </div>
          </div>

          <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-5 flex flex-col justify-between">
            <div className="flex items-center justify-between text-zinc-400 mb-2">
              <span className="text-sm font-semibold">上下文占用</span>
              <FileText className="h-5 w-5 text-emerald-400" />
            </div>
            <div>
              <div className="text-3xl font-extrabold font-mono text-zinc-50">
                {runtime?.context_pct ?? 0}%
              </div>
              <div className="w-full bg-zinc-800 h-1.5 rounded-full overflow-hidden mt-2">
                <div 
                  className={`h-full rounded-full transition-all duration-300 ${
                    (runtime?.context_pct ?? 0) > 85 ? 'bg-rose-500' : (runtime?.context_pct ?? 0) > 60 ? 'bg-amber-500' : 'bg-emerald-500'
                  }`}
                  style={{ width: `${runtime?.context_pct ?? 0}%` }}
                />
              </div>
            </div>
          </div>

          <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-5 flex flex-col justify-between">
            <div className="flex items-center justify-between text-zinc-400 mb-2">
              <span className="text-sm font-semibold">系统异常</span>
              <AlertCircle className={`h-5 w-5 ${(runtime?.total_errors ?? 0) > 0 ? 'text-rose-400 animate-pulse' : 'text-zinc-500'}`} />
            </div>
            <div>
              <div className={`text-3xl font-extrabold font-mono ${
                (runtime?.total_errors ?? 0) > 0 ? 'text-rose-400' : 'text-zinc-50'
              }`}>
                {runtime?.total_errors ?? 0}
              </div>
              <p className="text-xs text-zinc-500 mt-1">智能体执行异常次数</p>
            </div>
          </div>
        </section>

        {/* 实时推理流（显示智能体正在思考的内容） */}
        {runtime?.agent_streams && Object.keys(runtime.agent_streams).some(id => runtime.agent_streams[id].processing) && (
          <section className="space-y-3">
            <h2 className="text-sm font-bold text-zinc-400 tracking-wider uppercase flex items-center gap-2">
              <Terminal className="h-4 w-4 text-emerald-400" /> 实时推理流
            </h2>
            <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
              {Object.entries(runtime.agent_streams)
                .filter(([_, stream]) => stream.processing)
                .map(([id, stream]) => {
                  const agentName = agents.find(a => a.id === id)?.name || id
                  const lastThinking = stream.segments
                    ?.filter(seg => seg.type === 'thinking')
                    .map(seg => seg.text)
                    .join('') || ''
                  
                  const lastContent = stream.segments
                    ?.filter(seg => seg.type === 'content')
                    .map(seg => seg.text)
                    .join('') || ''

                  return (
                    <div key={id} className="bg-zinc-900 border border-zinc-800 rounded-xl overflow-hidden flex flex-col h-80">
                      <div className="bg-zinc-950 px-4 py-3 border-b border-zinc-800 flex justify-between items-center shrink-0">
                        <span className="font-semibold text-sm flex items-center gap-2">
                          <span className="w-2 h-2 rounded-full bg-emerald-500 animate-ping" />
                          {agentName}
                        </span>
                        <span className="text-xs text-zinc-500 font-mono">第 #{stream.iteration} 轮迭代</span>
                      </div>
                      
                      <div className="flex-1 p-4 overflow-y-auto font-mono text-xs leading-relaxed space-y-3">
                        {lastThinking && (
                          <div className="text-zinc-400 border-l-2 border-indigo-500/50 pl-3">
                            <span className="text-indigo-400 font-semibold block mb-1">思考过程：</span>
                            <span className="whitespace-pre-wrap">{lastThinking}</span>
                          </div>
                        )}
                        {lastContent && (
                          <div className="text-emerald-400 border-l-2 border-emerald-500/50 pl-3">
                            <span className="text-emerald-500 font-semibold block mb-1">生成内容：</span>
                            <span className="whitespace-pre-wrap">{lastContent}</span>
                          </div>
                        )}
                      </div>
                    </div>
                  )
                })}
            </div>
          </section>
        )}

        {/* 智能体状态列表 */}
        <section className="bg-zinc-900 border border-zinc-800 rounded-xl overflow-hidden">
          <div className="px-6 py-4 border-b border-zinc-800 flex items-center justify-between">
            <h2 className="text-sm font-bold text-zinc-300 uppercase tracking-wider">智能体状态总览</h2>
            <span className="text-xs text-zinc-500 font-mono">注册总数：{agents.length}</span>
          </div>

          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm border-collapse">
              <thead>
                <tr className="bg-zinc-950/50 border-b border-zinc-800 text-zinc-400 font-semibold">
                  <th className="px-6 py-3">智能体名称</th>
                  <th className="px-6 py-3">状态</th>
                  <th className="px-6 py-3">所属组</th>
                  <th className="px-6 py-3">模型</th>
                  <th className="px-6 py-3">任务级别</th>
                  <th className="px-6 py-3 text-right">异常次数</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-zinc-800/50">
                {agents.map((agent) => (
                  <tr key={agent.id} className="hover:bg-zinc-800/20 transition-colors">
                    <td className="px-6 py-4 font-semibold text-zinc-200">
                      <div className="flex items-center gap-2">
                        {agent.name}
                        {agent.is_leader && (
                          <span className="px-1.5 py-0.5 rounded text-[10px] font-bold bg-amber-500/10 text-amber-400 border border-amber-500/20">
                            队长
                          </span>
                        )}
                      </div>
                    </td>
                    <td className="px-6 py-4">
                      {agent.state === 'processing' ? (
                        <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-semibold bg-emerald-500/10 text-emerald-400">
                          <span className="w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse" />
                          运行中
                        </span>
                      ) : agent.state === 'idle' ? (
                        <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-semibold bg-zinc-500/10 text-zinc-400">
                          空闲
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-semibold bg-rose-500/10 text-rose-400">
                          已停止
                        </span>
                      )}
                    </td>
                    <td className="px-6 py-4 text-zinc-400 font-mono text-xs">{agent.group || '全局'}</td>
                    <td className="px-6 py-4 text-zinc-400 font-mono text-xs">{agent.model_id}</td>
                    <td className="px-6 py-4 font-mono text-xs">
                      <span className="px-2 py-0.5 rounded bg-zinc-800 text-zinc-300 border border-zinc-700">
                        {agent.task_level}
                      </span>
                    </td>
                    <td className="px-6 py-4 text-right">
                      {agent.error_count > 0 ? (
                        <span className="inline-flex items-center gap-1 text-rose-400 font-semibold" title={agent.last_error}>
                          <ShieldAlert className="h-4 w-4" /> {agent.error_count}
                        </span>
                      ) : (
                        <span className="text-zinc-500 flex items-center justify-end gap-1">
                          <CheckCircle2 className="h-4 w-4 text-emerald-500/70" /> 0
                        </span>
                      )}
                    </td>
                  </tr>
                ))}
                {agents.length === 0 && (
                  <tr>
                    <td colSpan={6} className="px-6 py-10 text-center text-zinc-500 italic">
                      暂无已注册的智能体
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </section>
      </main>

      <footer className="border-t border-zinc-800 px-6 py-3 text-center text-xs text-zinc-600">
        SoloQueue 状态中心 · 仅限本地访问 · 只读模式
      </footer>
    </div>
  )
}
