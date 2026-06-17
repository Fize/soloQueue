import { useEffect, useState, useRef, useCallback } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { wsManager } from '@/lib/websocket'
import { SimulationGraph, type GraphEdgeInput } from './SimulationGraph'
import { AgentActivityPanel } from './AgentActivityPanel'
import { SimulationProgressPanel } from './SimulationProgressPanel'
import { SimulationMonitor } from './SimulationMonitor'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import {
  Play,
  Square,
  ArrowLeft,
  MessageSquare,
  Cpu,
  Send,
  Loader2,
  FileText,
  AlertCircle,
  Clock,
  Settings,
  Edit,
  Save,
  X,
} from 'lucide-react'
import type {
  SimulationState,
  SimulationMessage,
  SimulationEvent,
  SimulationProgress,
  SimulationPersona,
} from '@/types'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'

const MAX_MESSAGES = 500
const MAX_CHAT_HISTORY = 20

function capChatHistory(history: { q: string; a: string; loading?: boolean }[]): typeof history {
  if (history.length > MAX_CHAT_HISTORY) return history.slice(-MAX_CHAT_HISTORY)
  return history
}
const MAX_GRAPH_EDGES = 200

function capMessages<T>(msgs: T[]): T[] {
  if (msgs.length <= MAX_MESSAGES) return msgs
  return msgs.slice(msgs.length - MAX_MESSAGES)
}

export function SimulationDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()

  const [state, setState] = useState<SimulationState | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [controlLoading, setControlLoading] = useState(false)

  // Configuration Edit States
  const [isEditing, setIsEditing] = useState(false)
  const [editTopic, setEditTopic] = useState('')
  const [editMaxWallClockMin, setEditMaxWallClockMin] = useState(18)
  const [editSimHours, setEditSimHours] = useState(168)
  const [editTimeScale, setEditTimeScale] = useState(600)
  const [editEnableReflection, setEditEnableReflection] = useState(true)
  const [editPersonas, setEditPersonas] = useState<any[]>([])
  const [savingConfig, setSavingConfig] = useState(false)

  const [providers, setProviders] = useState<{ id: string; name: string }[]>([])
  const [models, setModels] = useState<{ id: string; name: string; providerId: string }[]>([])

  // Filtering & Interaction
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null)
  const [viewingPersona, setViewingPersona] = useState<SimulationPersona | null>(null)
  const [chatAgentId, setChatAgentId] = useState<string | null>(null)
  const [chatQuestion, setChatQuestion] = useState('')
  const [chatHistory, setChatHistory] = useState<
    Record<string, { q: string; a: string; loading?: boolean }[]>
  >({})

  // Progress display state
  const [progress, setProgress] = useState<SimulationProgress | null>(null)
  const [graphEdges, setGraphEdges] = useState<GraphEdgeInput[]>([])
  const [progressSidebarTab, setProgressSidebarTab] = useState<'progress' | 'monitor' | 'activity'>(
    'progress'
  )
  // Use ref for pulse nodes to avoid render storms (#5). The graph reads via ref,
  // triggered by a lightweight counter state (avoids Set recreation).
  const pulseNodesRef = useRef<Set<string>>(new Set())
  const [pulseVersion, setPulseVersion] = useState(0)

  const messagesEndRef = useRef<HTMLDivElement | null>(null)
  const pulseTimersRef = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map())

  const pulseAgent = (agentId: string) => {
    pulseNodesRef.current.add(agentId)
    setPulseVersion((v) => v + 1)
    const existing = pulseTimersRef.current.get(agentId)
    if (existing) clearTimeout(existing)
    pulseTimersRef.current.set(
      agentId,
      setTimeout(() => {
        pulseNodesRef.current.delete(agentId)
        setPulseVersion((v) => v + 1)
      }, 2500)
    )
  }

  useEffect(() => {
    const abortController = new AbortController()
    const fetchConfigOptions = async () => {
      try {
        const provRes = await fetch('/api/config/providers', { signal: abortController.signal })
        if (provRes.ok) {
          const provData = await provRes.json()
          setProviders(provData || [])
        }
        const modelRes = await fetch('/api/config/models', { signal: abortController.signal })
        if (modelRes.ok) {
          const modelData = await modelRes.json()
          setModels(modelData || [])
        }
      } catch (err: any) {
        if (err.name !== 'AbortError') {
          console.error('Failed to load LLM configs', err)
        }
      }
    }
    fetchConfigOptions()
    return () => abortController.abort()
  }, [])

  useEffect(() => {
    if (state && !isEditing) {
      setEditTopic(state.config.topic)
      setEditMaxWallClockMin(
        state.config.max_wall_clock_ms ? Math.round(state.config.max_wall_clock_ms / 60000) : 18
      )
      setEditSimHours(state.config.simulated_hours || 168)
      setEditTimeScale(state.config.time_scale || 600)
      setEditEnableReflection(
        state.config.enable_reflection !== undefined ? state.config.enable_reflection : true
      )
      setEditPersonas(state.config.personas || [])
    }
  }, [state, isEditing])

  const handleSaveConfig = async () => {
    if (!id || !state) return
    try {
      setSavingConfig(true)
      const res = await fetch(`/api/simulations/${id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          ...state.config,
          topic: editTopic,
          max_wall_clock_ms: editMaxWallClockMin * 60 * 1000,
          simulated_hours: editSimHours,
          time_scale: editTimeScale,
          enable_reflection: editEnableReflection,
          personas: editPersonas,
        }),
      })

      if (!res.ok) {
        const errData = await res.json()
        throw new Error(errData.error || 'Failed to update configuration')
      }

      const data = await res.json()
      const mappedState: SimulationState = {
        ...data,
        id: data.config?.id || data.run_id || id,
        round: data.current_round || 0,
        messages: capMessages(
          (data.rounds || []).flatMap((r: any) =>
            (r.messages || []).map((m: any) => ({
              agent_id: m.agent_id,
              agent_name: m.agent_name,
              content: m.content,
              reasoning: m.reasoning,
              to: m.to,
              type: m.type,
              round: m.round,
              seq_num: m.seq_num,
            }))
          )
        ),
      }
      setState(mappedState)
      setIsEditing(false)
    } catch (err: any) {
      alert(err.message || 'Failed to save configuration')
    } finally {
      setSavingConfig(false)
    }
  }

  const handleUpdatePersonaOverride = (
    idx: number,
    field: 'model_id' | 'provider_id',
    value: string
  ) => {
    setEditPersonas((prev) => {
      const copy = [...prev]
      copy[idx] = {
        ...copy[idx],
        [field]: value || undefined,
      }
      return copy
    })
  }

  const fetchState = useCallback(async () => {
    try {
      setLoading(true)
      const res = await fetch(`/api/simulations/${id}`)
      if (!res.ok) {
        throw new Error('Simulation not found')
      }
      const data = await res.json()
      const mappedState: SimulationState = {
        ...data,
        id: data.config?.id || data.run_id || id,
        round: data.current_round || 0,
        messages: capMessages(
          (data.rounds || []).flatMap((r: any) =>
            (r.messages || []).map((m: any) => ({
              agent_id: m.agent_id,
              agent_name: m.agent_name,
              content: m.content,
              reasoning: m.reasoning,
              to: m.to,
              type: m.type,
              round: m.round,
              seq_num: m.seq_num,
            }))
          )
        ),
      }
      setState(mappedState)
      setError(null)
    } catch (err: any) {
      setError(err.message || 'Failed to fetch details')
    } finally {
      setLoading(false)
    }
  }, [id])

  useEffect(() => {
    if (!id) return
    fetchState()

    // Subscribe to real-time events
    const unsubEvent = wsManager.subscribe('simulation_event', (ev: SimulationEvent) => {
      if (ev.simulation_id !== id) return

      if (ev.type === 'agent_message' && ev.data) {
        const newMsg = ev.data as SimulationMessage
        pulseAgent(newMsg.agent_id)
        setState((prev) => {
          if (!prev) return null
          if (prev.messages.some((m) => m.seq_num === newMsg.seq_num)) return prev
          return {
            ...prev,
            round: ev.round,
            messages: capMessages([...prev.messages, newMsg]),
          }
        })
      } else if (ev.type === 'agent_move' && ev.data) {
        const moveData = ev.data as { agent_id: string; to_zone: string }
        if (moveData.agent_id) pulseAgent(moveData.agent_id)
      } else if (ev.type === 'agent_reflection' && ev.data) {
        const refData = ev.data as { agent_id: string }
        if (refData.agent_id) pulseAgent(refData.agent_id)
      } else if (ev.type === 'round_start') {
        setState((prev) => (prev ? { ...prev, round: ev.round } : null))
      } else if (ev.type === 'simulation_end') {
        setProgress((prev) =>
          prev
            ? {
                ...prev,
                phase: 'completed',
                progress_percent: 100,
              }
            : null
        )
      } else if (ev.type === 'agent_death' && ev.data) {
        const deathData = ev.data as { agent_id: string; agent_name: string }
        if (deathData.agent_id) pulseAgent(deathData.agent_id)
        // Refetch state to pick up IsActive changes and updated graph
        setTimeout(() => fetchState(), 1000)
      } else if (ev.type === 'agent_spawn' && ev.data) {
        // A new agent was spawned — refetch to update personas and graph
        fetchState()
      } else if (ev.type === 'error') {
        setProgress((prev) => (prev ? { ...prev, phase: 'failed', progress_percent: 100 } : null))
        fetchState()
      } else if (ev.type === 'finished') {
        setGraphEdges([])
        fetchState()
      }
    })

    // Subscribe to real-time progress updates
    const unsubProgress = wsManager.subscribe('simulation_progress', (p: SimulationProgress) => {
      if (p.simulation_id !== id) return
      setProgress(p)

      if (p.graph_edges && p.graph_edges.length > 0) {
        setGraphEdges((prev) => {
          const merged = [...prev]
          let changed = false
          for (const newEdge of p.graph_edges) {
            const idx = merged.findIndex(
              (e) =>
                e.source === newEdge.source &&
                e.target === newEdge.target &&
                e.type === newEdge.type
            )
            if (idx >= 0) {
              if (merged[idx].weight !== newEdge.weight) {
                merged[idx] = { ...merged[idx], weight: newEdge.weight, type: newEdge.type }
                changed = true
              }
            } else {
              merged.push(newEdge)
              changed = true
            }
          }
          if (!changed) return prev
          if (merged.length > MAX_GRAPH_EDGES) return merged.slice(merged.length - MAX_GRAPH_EDGES)
          return merged
        })
      }
    })

    return () => {
      unsubEvent()
      unsubProgress()
      // Clear all pulse timers (#3)
      pulseTimersRef.current.forEach((t) => clearTimeout(t))
      pulseTimersRef.current.clear()
    }
  }, [id, fetchState])

  // Scroll to bottom of message list on new messages
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [state?.messages])

  const handleStart = async () => {
    if (!id) return
    try {
      setControlLoading(true)
      const res = await fetch(`/api/simulations/${id}/start`, { method: 'POST' })
      if (!res.ok) {
        const errData = await res.json()
        throw new Error(errData.error || 'Failed to start simulation')
      }
      // Instantly update local status
      setState((prev) => (prev ? { ...prev, status: 'running' } : null))
    } catch (err: any) {
      alert(err.message)
    } finally {
      setControlLoading(false)
    }
  }

  const handleStop = async () => {
    if (!id) return
    try {
      setControlLoading(true)
      const res = await fetch(`/api/simulations/${id}/stop`, { method: 'POST' })
      if (!res.ok) {
        const errData = await res.json()
        throw new Error(errData.error || 'Failed to stop simulation')
      }
      setState((prev) => (prev ? { ...prev, status: 'completed' } : null))
    } catch (err: any) {
      alert(err.message)
    } finally {
      setControlLoading(false)
    }
  }

  const handleAskAgent = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!id || !chatAgentId || !chatQuestion.trim()) return

    const question = chatQuestion.trim()
    setChatQuestion('')

    // Append question immediately as loading state
    setChatHistory((prev) => ({
      ...prev,
      [chatAgentId]: capChatHistory([
        ...(prev[chatAgentId] || []),
        { q: question, a: '', loading: true },
      ]),
    }))

    try {
      const res = await fetch(`/api/simulations/${id}/agents/${chatAgentId}/ask`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ question }),
      })

      if (!res.ok) {
        throw new Error('Failed to query agent')
      }
      const data = await res.json()

      setChatHistory((prev) => {
        const history = [...(prev[chatAgentId] || [])]
        const lastIndex = history.findIndex((h) => h.q === question && h.loading)
        if (lastIndex !== -1) {
          history[lastIndex] = { q: question, a: data.answer || 'No answer received.' }
        }
        return { ...prev, [chatAgentId]: capChatHistory(history) }
      })
    } catch (err: any) {
      setChatHistory((prev) => {
        const history = [...(prev[chatAgentId] || [])]
        const lastIndex = history.findIndex((h) => h.q === question && h.loading)
        if (lastIndex !== -1) {
          history[lastIndex] = {
            q: question,
            a: `Error: ${err.message || 'Failed to query agent.'}`,
          }
        }
        return { ...prev, [chatAgentId]: capChatHistory(history) }
      })
    }
  }

  if (loading && !state) {
    return (
      <div className="flex h-screen items-center justify-center bg-background">
        <Loader2 className="h-8 w-8 animate-spin text-primary" />
      </div>
    )
  }

  if (error || !state) {
    return (
      <div className="flex h-screen flex-col items-center justify-center bg-background p-6 text-foreground">
        <AlertCircle className="h-10 w-10 text-rose-500 mb-4" />
        <p className="text-lg font-semibold">{error || 'Simulation not found'}</p>
        <button
          onClick={() => navigate('/simulations')}
          className="mt-4 flex items-center gap-2 rounded-lg bg-muted hover:bg-muted/85 px-4 py-2 text-sm text-foreground transition-colors"
        >
          <ArrowLeft className="h-4 w-4" /> Back to Simulations
        </button>
      </div>
    )
  }

  // Filter messages based on active filters
  const filteredMessages = selectedAgentId
    ? state.messages.filter((m) => m.agent_id === selectedAgentId)
    : state.messages

  const getStatusBadgeClass = (status: string) => {
    switch (status) {
      case 'running':
        return 'bg-emerald-500/10 text-emerald-500 dark:text-emerald-400 border border-emerald-500/25'
      case 'completed':
        return 'bg-primary/10 text-primary border border-primary/25'
      case 'failed':
        return 'bg-rose-500/10 text-rose-500 dark:text-rose-400 border border-rose-500/25'
      default:
        return 'bg-muted-foreground/10 text-muted-foreground border border-muted-foreground/25'
    }
  }

  return (
    <div className="flex h-full flex-col bg-background text-foreground overflow-hidden">
      {/* Top Header Controls */}
      <header className="flex shrink-0 items-center justify-between border-b border-border bg-card/30 px-6 py-4">
        <div className="flex items-center gap-4 min-w-0">
          <button
            onClick={() => navigate('/simulations')}
            className="rounded-lg p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
          >
            <ArrowLeft className="h-5 w-5" />
          </button>
          <div className="min-w-0">
            <h1 className="text-lg font-semibold text-foreground truncate">{state.config.topic}</h1>
            <div className="mt-0.5 flex items-center gap-2 text-[10px] text-muted-foreground font-mono">
              <span>ID: {state.id.slice(0, 8)}...</span>
              <span>•</span>
              <span
                className={`px-1.5 py-0.2 rounded text-[9px] font-bold uppercase ${getStatusBadgeClass(state.status)}`}
              >
                {state.status}
              </span>
              {state.status === 'running' && (
                <>
                  <span>•</span>
                  <span className="text-primary animate-pulse font-bold">
                    Round {state.current_round}
                  </span>
                </>
              )}
            </div>
          </div>
        </div>

        {/* Start / Stop Controls */}
        <div className="flex items-center gap-3">
          {(state.status === 'idle' || state.status === 'pending') && (
            <button
              onClick={handleStart}
              disabled={controlLoading || isEditing}
              title={isEditing ? 'Save configuration before starting' : ''}
              className="flex items-center gap-2 rounded-lg bg-success hover:bg-success/90 disabled:bg-success/50 px-4 py-2 text-sm font-semibold text-success-foreground transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <Play className="h-4 w-4" /> Start Simulation
            </button>
          )}
          {state.status === 'running' && (
            <button
              onClick={handleStop}
              disabled={controlLoading}
              className="flex items-center gap-2 rounded-lg bg-destructive hover:bg-destructive/90 disabled:bg-destructive/50 px-4 py-2 text-sm font-semibold text-destructive-foreground transition-colors cursor-pointer"
            >
              <Square className="h-4 w-4" /> Stop Simulation
            </button>
          )}
          {controlLoading && <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />}
        </div>
      </header>

      {/* Main Workspace (Grid layout) */}
      <div className="flex flex-1 overflow-hidden min-h-0">
        {/* Left Side: Graph + Agent Profiles */}
        <div className="flex-[3] flex flex-col p-6 gap-6 overflow-y-auto border-r border-border min-w-[320px]">
          {/* Graph Visualization */}
          <div className="flex-1 min-h-[350px]">
            <SimulationGraph
              personas={state.config.personas}
              edges={
                graphEdges.length > 0
                  ? graphEdges
                  : (state.graph?.edges || []).map((e) => ({
                      source: e.source,
                      target: e.target,
                      type: e.type,
                      weight: e.weight,
                    }))
              }
              onSelectAgent={(agentId) => {
                setSelectedAgentId((prev) => (prev === agentId ? null : agentId))
                if (agentId) setProgressSidebarTab('activity')
              }}
              selectedAgentId={selectedAgentId}
              pulseNodes={pulseNodesRef.current}
              pulseVersion={pulseVersion}
            />
          </div>

          {/* Agent Stances & Profiles */}
          <div className="shrink-0 space-y-4">
            <h3 className="text-xs font-bold text-muted-foreground uppercase tracking-wider font-mono">
              Agents
            </h3>
            <div className="grid gap-3 sm:grid-cols-2">
              {state.config.personas.map((persona) => {
                const isSelected = selectedAgentId === persona.id
                return (
                  <div
                    key={persona.id}
                    onClick={() => setSelectedAgentId(isSelected ? null : persona.id)}
                    className={`rounded-xl border p-4 cursor-pointer transition-all ${
                      isSelected
                        ? 'border-primary/50 bg-primary/5'
                        : 'border-border bg-card/25 hover:border-border/80'
                    }`}
                  >
                    <div className="flex items-start justify-between gap-2">
                      <div className="flex-1 min-w-0">
                        <h4 className="font-semibold text-foreground text-sm truncate">
                          {persona.name}
                        </h4>
                        <p className="text-xs text-muted-foreground mt-0.5 line-clamp-1">
                          {persona.role}
                        </p>
                      </div>
                      <div className="flex items-center gap-1">
                        <button
                          onClick={(e) => {
                            e.stopPropagation()
                            setViewingPersona(persona)
                          }}
                          className="rounded-lg p-1 text-muted-foreground hover:bg-muted hover:text-primary transition-colors"
                          title="View Agent Prompt"
                        >
                          <FileText className="h-4 w-4" />
                        </button>
                        {state.status === 'completed' && (
                          <button
                            onClick={(e) => {
                              e.stopPropagation()
                              setChatAgentId(persona.id)
                            }}
                            className="rounded-lg p-1 text-muted-foreground hover:bg-muted hover:text-primary transition-colors"
                            title="Ask question post-simulation"
                          >
                            <MessageSquare className="h-4 w-4" />
                          </button>
                        )}
                      </div>
                    </div>
                    {/* Traits */}
                    <div className="mt-2.5 flex flex-wrap gap-1">
                      {Object.entries(persona.traits || {}).map(([k, v]) => (
                        <span
                          key={k}
                          className="rounded bg-muted px-1.5 py-0.5 text-[9px] font-mono text-muted-foreground"
                          title={`${k}: ${v}`}
                        >
                          {k}
                        </span>
                      ))}
                    </div>
                  </div>
                )
              })}

              {/* Report Analyst Node Card */}
              {state.status === 'completed' && state.report && (
                <div
                  onClick={() => setChatAgentId('report')}
                  className={`rounded-xl border p-4 cursor-pointer transition-all border-primary/30 bg-primary/5 hover:border-primary/50 sm:col-span-2`}
                >
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2.5">
                      <Cpu className="h-5 w-5 text-primary animate-pulse" />
                      <div>
                        <h4 className="font-semibold text-foreground text-sm">Report Analyst</h4>
                        <p className="text-xs text-muted-foreground">
                          Interview the analyst regarding simulation findings.
                        </p>
                      </div>
                    </div>
                    <span className="rounded bg-primary/20 px-2 py-0.5 text-[9px] font-bold text-primary font-mono">
                      ASK REPORT
                    </span>
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>

        {/* Right Side: Message Stream / Report Markdown / Configuration */}
        <div className="flex-[2] flex flex-col min-w-[280px] bg-muted/10 border-l border-border/45">
          {/* Tabs */}
          <div className="flex border-b border-border">
            {state.status === 'running' || state.status === 'failed' ? (
              <>
                <button
                  onClick={() => setProgressSidebarTab('progress')}
                  className={`flex-1 py-3 text-center text-xs font-semibold font-mono transition-colors ${
                    progressSidebarTab === 'progress'
                      ? 'border-b-2 border-primary text-primary bg-card/20'
                      : 'text-muted-foreground border-b-2 border-transparent hover:text-foreground'
                  }`}
                >
                  PROGRESS
                </button>
                <button
                  onClick={() => setProgressSidebarTab('monitor')}
                  className={`flex-1 py-3 text-center text-xs font-semibold font-mono transition-colors ${
                    progressSidebarTab === 'monitor'
                      ? 'border-b-2 border-primary text-primary bg-card/20'
                      : 'text-muted-foreground border-b-2 border-transparent hover:text-foreground'
                  }`}
                >
                  MONITOR
                </button>
                {selectedAgentId && (
                  <button
                    onClick={() => setProgressSidebarTab('activity')}
                    className={`flex-1 py-3 text-center text-xs font-semibold font-mono transition-colors ${
                      progressSidebarTab === 'activity'
                        ? 'border-b-2 border-primary text-primary bg-card/20'
                        : 'text-muted-foreground border-b-2 border-transparent hover:text-foreground'
                    }`}
                  >
                    AGENT
                  </button>
                )}
              </>
            ) : (
              <button className="flex-1 py-3 text-center text-xs font-semibold border-b-2 border-primary text-primary bg-card/20 font-mono">
                {state.status === 'pending' ? 'CONFIGURATION' : 'DISCUSSION LOG'}
              </button>
            )}
          </div>

          <div className="flex-1 overflow-y-auto p-4 space-y-4">
            {state.status === 'running' || state.status === 'failed' ? (
              progress ? (
                progressSidebarTab === 'progress' ? (
                  <SimulationProgressPanel
                    progress={progress}
                    messages={state.messages}
                    selectedAgentId={selectedAgentId}
                    onSelectAgent={setSelectedAgentId}
                  />
                ) : progressSidebarTab === 'monitor' ? (
                  <SimulationMonitor logs={progress.recent_logs || []} />
                ) : selectedAgentId ? (
                  <AgentActivityPanel
                    agentId={selectedAgentId}
                    agentName={
                      state.config.personas.find((p) => p.id === selectedAgentId)?.name ||
                      selectedAgentId
                    }
                    agentRole={
                      state.config.personas.find((p) => p.id === selectedAgentId)?.role || ''
                    }
                    messages={state.messages}
                    progress={progress}
                  />
                ) : (
                  <SimulationMonitor logs={progress.recent_logs || []} />
                )
              ) : (
                <div className="flex h-32 items-center justify-center text-center text-muted-foreground font-mono text-xs">
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  Starting simulation...
                </div>
              )
            ) : state.status === 'pending' ? (
              isEditing ? (
                // Edit Configuration Form
                <div className="space-y-5 rounded-xl border border-border bg-card/30 p-5 backdrop-blur-md">
                  <div className="flex items-center justify-between border-b border-border/60 pb-3">
                    <div className="flex items-center gap-2">
                      <Settings className="h-4.5 w-4.5 text-primary" />
                      <h3 className="font-semibold text-foreground text-sm">Edit Parameters</h3>
                    </div>
                    <button
                      type="button"
                      onClick={() => setIsEditing(false)}
                      className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground transition-all cursor-pointer"
                    >
                      <X className="h-4 w-4" />
                    </button>
                  </div>

                  {/* Topic */}
                  <div className="space-y-1.5">
                    <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono">
                      Simulation Topic
                    </label>
                    <input
                      type="text"
                      value={editTopic}
                      onChange={(e) => setEditTopic(e.target.value)}
                      className="w-full rounded-lg border border-border bg-background px-3 py-2 text-xs text-foreground focus:border-primary focus:ring-1 focus:ring-primary/20 focus:outline-none transition-all"
                    />
                  </div>

                  {/* Wall Clock & Simulated Hours */}
                  <div className="grid grid-cols-2 gap-3">
                    <div className="space-y-1.5">
                      <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono">
                        Max Time: {editMaxWallClockMin}m
                      </label>
                      <input
                        type="range"
                        min={1}
                        max={30}
                        value={editMaxWallClockMin}
                        onChange={(e) => setEditMaxWallClockMin(parseInt(e.target.value) || 5)}
                        className="w-full h-1.5 bg-muted rounded-lg appearance-none cursor-pointer accent-primary"
                      />
                    </div>

                    <div className="space-y-1.5">
                      <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono">
                        Sim Hours: {editSimHours}h
                      </label>
                      <input
                        type="range"
                        min={6}
                        max={168}
                        step={6}
                        value={editSimHours}
                        onChange={(e) => {
                          const val = parseInt(e.target.value) || 48
                          setEditSimHours(val)
                          setEditMaxWallClockMin(Math.round((val * 5) / 48))
                        }}
                        className="w-full h-1.5 bg-muted rounded-lg appearance-none cursor-pointer accent-primary"
                      />
                    </div>
                  </div>

                  {/* Time Scale & Reflection */}
                  <div className="grid grid-cols-2 gap-3">
                    <div className="space-y-1.5">
                      <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono">
                        Time Scale
                      </label>
                      <select
                        value={editTimeScale}
                        onChange={(e) => setEditTimeScale(parseInt(e.target.value))}
                        className="w-full rounded-lg border border-border bg-background px-2.5 py-1.5 text-xs text-foreground focus:border-primary focus:outline-none transition-all cursor-pointer"
                      >
                        <option value={60}>1s = 1min</option>
                        <option value={300}>1s = 5min</option>
                        <option value={600}>1s = 10min</option>
                        <option value={1800}>1s = 30min</option>
                        <option value={3600}>1s = 1h</option>
                      </select>
                    </div>

                    <div className="space-y-1.5">
                      <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono">
                        Reflection
                      </label>
                      <div className="flex items-center gap-2 pt-1">
                        <button
                          type="button"
                          onClick={() => setEditEnableReflection(!editEnableReflection)}
                          className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors ${
                            editEnableReflection ? 'bg-primary' : 'bg-muted'
                          }`}
                        >
                          <span
                            className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white transition-transform ${
                              editEnableReflection ? 'translate-x-[18px]' : 'translate-x-[3px]'
                            }`}
                          />
                        </button>
                        <span className="text-[10px] text-muted-foreground">
                          {editEnableReflection ? 'On' : 'Off'}
                        </span>
                      </div>
                    </div>
                  </div>

                  {/* Agent Custom Overrides */}
                  <div className="space-y-3 pt-2">
                    <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono border-t border-border/40 pt-3">
                      Agent Specific Models
                    </label>
                    <div className="space-y-2.5">
                      {editPersonas.map((persona, idx) => (
                        <div
                          key={persona.id || idx}
                          className="rounded-lg border border-border bg-background/55 p-3 space-y-2"
                        >
                          <div className="flex items-center justify-between">
                            <span className="text-xs font-semibold text-foreground">
                              {persona.name}
                            </span>
                            <span className="text-[9px] text-muted-foreground font-mono">
                              {persona.role}
                            </span>
                          </div>

                          <div className="grid grid-cols-2 gap-2">
                            <div>
                              <label className="block text-[8px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-1">
                                Provider
                              </label>
                              <select
                                value={persona.provider_id || ''}
                                onChange={(e) => {
                                  handleUpdatePersonaOverride(idx, 'provider_id', e.target.value)
                                  handleUpdatePersonaOverride(idx, 'model_id', '')
                                }}
                                className="w-full rounded border border-border bg-background px-1.5 py-1 text-[11px] text-foreground focus:outline-none transition-all cursor-pointer"
                              >
                                <option value="">(Default Fast Provider)</option>
                                {providers.map((p) => (
                                  <option key={p.id} value={p.id}>
                                    {p.name}
                                  </option>
                                ))}
                              </select>
                            </div>

                            <div>
                              <label className="block text-[8px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-1">
                                Model
                              </label>
                              <select
                                value={persona.model_id || ''}
                                onChange={(e) =>
                                  handleUpdatePersonaOverride(idx, 'model_id', e.target.value)
                                }
                                className="w-full rounded border border-border bg-background px-1.5 py-1 text-[11px] text-foreground focus:outline-none transition-all cursor-pointer"
                              >
                                <option value="">(Default Fast Model)</option>
                                {models
                                  .filter(
                                    (m) =>
                                      !persona.provider_id || m.providerId === persona.provider_id
                                  )
                                  .map((m) => (
                                    <option key={m.id} value={m.id}>
                                      {m.name}
                                    </option>
                                  ))}
                              </select>
                            </div>
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>

                  {/* Actions */}
                  <div className="flex gap-2.5 pt-2">
                    <button
                      type="button"
                      onClick={handleSaveConfig}
                      disabled={savingConfig}
                      className="flex-1 flex items-center justify-center gap-1.5 rounded-lg bg-primary hover:bg-primary/95 disabled:bg-primary/50 py-2 text-xs font-semibold text-primary-foreground transition-all cursor-pointer shadow-md shadow-primary/5 disabled:cursor-not-allowed"
                    >
                      {savingConfig ? (
                        <>
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                          Saving...
                        </>
                      ) : (
                        <>
                          <Save className="h-3.5 w-3.5" />
                          Save Config
                        </>
                      )}
                    </button>
                    <button
                      type="button"
                      onClick={() => setIsEditing(false)}
                      disabled={savingConfig}
                      className="flex-1 rounded-lg bg-muted hover:bg-muted/80 py-2 text-xs font-semibold text-foreground transition-colors cursor-pointer"
                    >
                      Cancel
                    </button>
                  </div>
                </div>
              ) : (
                // View Configuration Summary
                <div className="space-y-5 rounded-xl border border-border bg-card/30 p-5 backdrop-blur-md">
                  <div className="flex items-center justify-between border-b border-border/60 pb-3">
                    <div className="flex items-center gap-2">
                      <Settings className="h-4.5 w-4.5 text-primary" />
                      <h3 className="font-semibold text-foreground text-sm">Parameters Summary</h3>
                    </div>
                    <button
                      type="button"
                      onClick={() => setIsEditing(true)}
                      className="flex items-center gap-1 rounded bg-primary/10 hover:bg-primary/20 border border-primary/20 px-2 py-1 text-[10px] font-semibold text-primary transition-colors cursor-pointer"
                    >
                      <Edit className="h-3 w-3" /> Edit Config
                    </button>
                  </div>

                  {/* Details Grid */}
                  <div className="grid grid-cols-2 gap-3 text-xs">
                    <div className="rounded-lg bg-background/45 border border-border/40 p-2.5">
                      <span className="block text-[8px] font-bold text-muted-foreground uppercase font-mono tracking-wider">
                        Simulated Hours
                      </span>
                      <span className="font-semibold text-foreground mt-0.5 block">
                        {state.config.simulated_hours || 48}h
                      </span>
                    </div>

                    <div className="rounded-lg bg-background/45 border border-border/40 p-2.5">
                      <span className="block text-[8px] font-bold text-muted-foreground uppercase font-mono tracking-wider">
                        Max Clock
                      </span>
                      <span className="font-semibold text-foreground mt-0.5 block">
                        {state.config.max_wall_clock_ms
                          ? Math.round(state.config.max_wall_clock_ms / 60000)
                          : 5}{' '}
                        minutes
                      </span>
                    </div>

                    <div className="rounded-lg bg-background/45 border border-border/40 p-2.5">
                      <span className="block text-[8px] font-bold text-muted-foreground uppercase font-mono tracking-wider">
                        Time Scale
                      </span>
                      <span className="font-semibold text-foreground mt-0.5 block">
                        1s = {Math.round((state.config.time_scale || 600) / 60)}min
                      </span>
                    </div>

                    <div className="rounded-lg bg-background/45 border border-border/40 p-2.5">
                      <span className="block text-[8px] font-bold text-muted-foreground uppercase font-mono tracking-wider">
                        Reflection
                      </span>
                      <span className="font-semibold text-foreground mt-0.5 block">
                        {state.config.enable_reflection ? 'Enabled' : 'Disabled'}
                      </span>
                    </div>
                  </div>

                  {/* Topic Box */}
                  <div className="rounded-lg bg-background/45 border border-border/40 p-3 text-xs">
                    <span className="block text-[8px] font-bold text-muted-foreground uppercase font-mono tracking-wider mb-1">
                      Topic
                    </span>
                    <span className="text-foreground leading-relaxed font-medium">
                      {state.config.topic}
                    </span>
                  </div>

                  {/* Agent Configs */}
                  <div className="space-y-2">
                    <span className="block text-[9px] font-bold text-muted-foreground uppercase font-mono tracking-wider border-t border-border/40 pt-3 mb-1">
                      Agent Assignment
                    </span>
                    <div className="space-y-2 max-h-[220px] overflow-y-auto pr-1">
                      {state.config.personas.map((persona) => (
                        <div
                          key={persona.id}
                          className="flex flex-col gap-1 rounded bg-background/25 border border-border/40 p-2.5 text-xs"
                        >
                          <div className="flex justify-between items-center font-semibold">
                            <span className="text-foreground">{persona.name}</span>
                            <div className="flex items-center gap-1.5">
                              <span className="text-[10px] text-muted-foreground font-mono font-normal">
                                {persona.role}
                              </span>
                              <button
                                type="button"
                                onClick={() => setViewingPersona(persona)}
                                className="rounded p-0.5 text-muted-foreground hover:bg-muted hover:text-primary transition-colors"
                                title="View Agent Prompt"
                              >
                                <FileText className="h-3 w-3" />
                              </button>
                            </div>
                          </div>
                          <div className="flex gap-2 text-[10px] text-muted-foreground/80 mt-1 font-mono">
                            <span>
                              Provider:{' '}
                              <span className="text-primary/90">
                                {persona.provider_id || 'default'}
                              </span>
                            </span>
                            <span>•</span>
                            <span>
                              Model:{' '}
                              <span className="text-primary/90">
                                {persona.model_id || 'default'}
                              </span>
                            </span>
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>
                </div>
              )
            ) : (
              // Discussion Log
              <>
                {state.report && !selectedAgentId && (
                  <div className="rounded-xl border border-primary/20 bg-primary/5 p-5 mb-4 backdrop-blur-sm">
                    <div className="mb-3 flex items-center gap-2 border-b border-primary/10 pb-2">
                      <FileText className="h-4 w-4 text-primary" />
                      <h3 className="font-bold text-primary text-xs tracking-wider uppercase font-mono">
                        Simulation Final Report
                      </h3>
                    </div>
                    <div className="prose prose-sm dark:prose-invert max-w-none text-foreground/90">
                      <ReactMarkdown remarkPlugins={[remarkGfm]}>{state.report}</ReactMarkdown>
                    </div>
                  </div>
                )}

                {filteredMessages.length === 0 ? (
                  <div className="flex h-32 flex-col items-center justify-center text-center text-muted-foreground font-mono text-xs">
                    <Clock className="mb-2 h-5 w-5 text-muted-foreground/60 animate-pulse" />
                    <span>Waiting for discussion to begin...</span>
                  </div>
                ) : (
                  filteredMessages.map((msg, idx) => (
                    <div
                      key={idx}
                      className="flex flex-col gap-1 rounded-xl bg-card/30 border border-border/80 p-4"
                    >
                      <div className="flex items-center justify-between border-b border-border/40 pb-1.5">
                        <span className="font-semibold text-primary text-xs">{msg.agent_name}</span>
                        <span className="rounded bg-muted px-1.5 py-0.5 text-[8px] font-mono text-muted-foreground">
                          R{msg.round} • {msg.type}
                        </span>
                      </div>
                      <p className="text-xs text-foreground/95 leading-relaxed mt-1 whitespace-pre-wrap font-sans">
                        {msg.content}
                      </p>
                      {msg.reasoning && (
                        <details className="mt-2.5">
                          <summary className="text-[10px] text-muted-foreground cursor-pointer select-none hover:text-foreground">
                            View Agent Reasoning
                          </summary>
                          <p className="mt-1 text-[10px] text-muted-foreground/80 italic bg-background/50 p-2.5 rounded border border-border/40 leading-normal">
                            {msg.reasoning}
                          </p>
                        </details>
                      )}
                    </div>
                  ))
                )}
              </>
            )}
            <div ref={messagesEndRef} />
          </div>
        </div>
      </div>

      {/* Post-Simulation Chat Dialogue (Drawer style at bottom of screen) */}
      {chatAgentId && (
        <div className="fixed inset-y-0 right-0 z-50 w-full max-w-md bg-card border-l border-border shadow-2xl flex flex-col animate-in slide-in-from-right duration-200">
          <header className="flex shrink-0 items-center justify-between border-b border-border px-4 py-3 bg-background">
            <div>
              <h3 className="font-semibold text-foreground text-sm">
                Chat with{' '}
                {chatAgentId === 'report'
                  ? 'Report Analyst'
                  : state.config.personas.find((p) => p.id === chatAgentId)?.name}
              </h3>
              <p className="text-[10px] text-muted-foreground">
                Interview agent in-character regarding the simulation.
              </p>
            </div>
            <button
              onClick={() => {
                setChatAgentId(null)
                setChatQuestion('')
              }}
              className="text-muted-foreground hover:text-foreground text-xs font-mono"
            >
              Close
            </button>
          </header>

          <div className="flex-1 overflow-y-auto p-4 space-y-4">
            <div className="rounded-lg bg-background/40 border border-border p-3 text-[10px] text-muted-foreground leading-relaxed">
              You can query the agent about their stance during the debate, their interactions, or
              the resulting report details.
            </div>

            {/* Local chat thread history */}
            {(chatHistory[chatAgentId] || []).map((chat, idx) => (
              <div key={idx} className="space-y-3">
                {/* Question */}
                <div className="flex justify-end">
                  <div className="rounded-lg bg-primary px-3 py-2 text-xs text-primary-foreground max-w-[85%] font-medium">
                    {chat.q}
                  </div>
                </div>
                {/* Answer */}
                <div className="flex justify-start">
                  <div className="rounded-lg bg-muted/70 border border-border px-3 py-2 text-xs text-foreground max-w-[85%]">
                    {chat.loading ? (
                      <div className="flex items-center gap-2 text-muted-foreground">
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        Thinking...
                      </div>
                    ) : (
                      chat.a
                    )}
                  </div>
                </div>
              </div>
            ))}
          </div>

          <form
            onSubmit={handleAskAgent}
            className="shrink-0 border-t border-border p-3 bg-background flex gap-2"
          >
            <input
              type="text"
              required
              placeholder="Ask a question..."
              value={chatQuestion}
              onChange={(e) => setChatQuestion(e.target.value)}
              className="flex-1 rounded-lg border border-border bg-card px-3 py-2 text-xs text-foreground placeholder:text-muted-foreground/50 focus:border-primary focus:ring-1 focus:ring-primary/20 focus:outline-none transition-all"
            />
            <button
              type="submit"
              className="rounded-lg bg-primary hover:bg-primary/90 p-2 text-primary-foreground transition-colors cursor-pointer"
            >
              <Send className="h-4 w-4" />
            </button>
          </form>
        </div>
      )}

      {/* View Agent Prompt Dialog */}
      <Dialog open={!!viewingPersona} onOpenChange={(v) => !v && setViewingPersona(null)}>
        <DialogContent className="max-w-2xl max-h-[85vh] flex flex-col p-6 overflow-hidden">
          <DialogHeader className="shrink-0">
            <DialogTitle className="flex items-baseline gap-2">
              <span className="text-lg font-bold text-foreground">{viewingPersona?.name}</span>
              <span className="text-xs text-muted-foreground font-mono font-normal">
                ({viewingPersona?.role})
              </span>
            </DialogTitle>
          </DialogHeader>

          <div className="flex-1 overflow-y-auto mt-4 pr-1 space-y-4">
            <div>
              <h5 className="text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-2">
                System Prompt
              </h5>
              <div className="rounded-xl border border-border bg-muted/30 p-4 font-mono text-xs whitespace-pre-wrap leading-relaxed text-foreground select-text overflow-x-auto max-h-[40vh]">
                {viewingPersona?.system_prompt || 'No system prompt configured.'}
              </div>
            </div>

            {viewingPersona?.bio && (
              <div>
                <h5 className="text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-2">
                  Biography
                </h5>
                <p className="text-xs text-foreground/90 leading-relaxed bg-muted/10 p-3 rounded-lg border border-border/40">
                  {viewingPersona.bio}
                </p>
              </div>
            )}

            {viewingPersona?.goals && viewingPersona.goals.length > 0 && (
              <div>
                <h5 className="text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-2">
                  Goals
                </h5>
                <ul className="list-disc list-inside space-y-1 text-xs text-foreground/90 leading-relaxed bg-muted/10 p-3 rounded-lg border border-border/40">
                  {viewingPersona.goals.map((goal, idx) => (
                    <li key={idx}>{goal}</li>
                  ))}
                </ul>
              </div>
            )}

            {viewingPersona?.traits && Object.keys(viewingPersona.traits).length > 0 && (
              <div>
                <h5 className="text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-2">
                  Traits
                </h5>
                <div className="grid grid-cols-2 gap-2 bg-muted/10 p-3 rounded-lg border border-border/40">
                  {Object.entries(viewingPersona.traits).map(([k, v]) => (
                    <div key={k} className="text-xs">
                      <span className="font-mono text-muted-foreground mr-1.5">{k}:</span>
                      <span className="text-foreground">{v}</span>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>

          <DialogFooter className="mt-4 shrink-0" showCloseButton />
        </DialogContent>
      </Dialog>
    </div>
  )
}
