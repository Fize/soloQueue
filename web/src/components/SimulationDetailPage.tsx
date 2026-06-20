import { useEffect, useState, useRef, useCallback } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { wsManager } from '@/lib/websocket'
import { SimulationGraph, type GraphEdgeInput } from './SimulationGraph'
import { AgentActivityPanel } from './AgentActivityPanel'
import { AgentDetailPanel } from './AgentDetailPanel'
import { SimulationProgressPanel } from './SimulationProgressPanel'
import { SimulationMonitor } from './SimulationMonitor'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { useIsMobile } from '@/hooks/useMediaQuery'
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
  Share2,
  ChevronDown,
  ChevronRight,
  Users,
} from 'lucide-react'
import type {
  SimulationState,
  SimulationMessage,
  SimulationEvent,
  SimulationProgress,
  SimulationPersona,
  RelationshipDTO,
} from '@/types'
import { toast } from 'sonner'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
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
  const [stopConfirmOpen, setStopConfirmOpen] = useState(false)
  const [editTopic, setEditTopic] = useState('')
  const [editMaxWallClockMin, setEditMaxWallClockMin] = useState(18)
  const [editSimHours, setEditSimHours] = useState(168)
  const [editTimeScale, setEditTimeScale] = useState(600)
  const [editEnableReflection, setEditEnableReflection] = useState(true)
  const [editPersonas, setEditPersonas] = useState<any[]>([])
  const [savingConfig, setSavingConfig] = useState(false)
  const [graphCollapsed, setGraphCollapsed] = useState(false)
  const [relationships, setRelationships] = useState<RelationshipDTO[]>([])
  const [graphLayer, setGraphLayer] = useState<'interaction' | 'relationship' | 'both'>('both')
  const isMobile = useIsMobile()

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
          toast.error('Failed to load LLM configs')
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
      toast.error(err.message || 'Failed to save configuration')
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

      // Populate relationships: prefer runtime snapshot, fallback to seed extraction for pre-simulation
      const hasStarted =
        data.started_at ||
        data.status === 'running' ||
        data.status === 'completed' ||
        data.status === 'failed'
      if (hasStarted && data.relationships) {
        setRelationships(data.relationships)
      } else if (data.config?.initial_relationships?.length > 0) {
        const nameToId = new Map<string, string>()
        for (const p of data.config.personas || []) {
          if (p.name && p.id) nameToId.set(p.name, p.id)
        }
        const dtos: RelationshipDTO[] = data.config.initial_relationships
          .map((rel: any) => {
            const subjectId = nameToId.get(rel.subject_name)
            const targetId = nameToId.get(rel.target_name)
            if (!subjectId || !targetId) return null
            return {
              subject_id: subjectId,
              subject_name: rel.subject_name,
              target_id: targetId,
              target_name: rel.target_name,
              kind: rel.kind || 'stranger',
              familiarity: rel.familiarity ?? 0.5,
              affinity: rel.affinity ?? 0,
              tags: rel.tags || [],
            }
          })
          .filter(Boolean) as RelationshipDTO[]
        setRelationships(dtos)
      }

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
      } else if (ev.type === 'relationship_update' && ev.data) {
        const data = ev.data as any
        setRelationships((prev) => {
          const idx = prev.findIndex(
            (r) => r.subject_id === data.subject_id && r.target_id === data.target_id
          )
          const newRel: RelationshipDTO = {
            subject_id: data.subject_id,
            subject_name: data.subject_name || '',
            target_id: data.target_id,
            target_name: data.target_name || '',
            kind: data.kind || 'stranger',
            familiarity: data.familiarity ?? 0.5,
            affinity: data.affinity ?? 0,
            tags: data.tags || [],
          }
          if (idx >= 0) {
            const next = [...prev]
            next[idx] = newRel
            return next
          }
          return [...prev, newRel]
        })
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

      // Sync relationship edges from progress
      const progRels = p.relationship_edges
      if (progRels && progRels.length > 0) {
        setRelationships((prev) => {
          const updated = [...prev]
          let changed = false
          for (const re of progRels) {
            const idx = updated.findIndex(
              (r) => r.subject_id === re.subject_id && r.target_id === re.target_id
            )
            if (idx >= 0) {
              if (
                updated[idx].familiarity !== re.familiarity ||
                updated[idx].affinity !== re.affinity
              ) {
                updated[idx] = re
                changed = true
              }
            } else {
              updated.push(re)
              changed = true
            }
          }
          return changed ? updated : prev
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
      toast.error(err.message)
    } finally {
      setControlLoading(false)
    }
  }

  const handleStopClick = () => {
    setStopConfirmOpen(true)
  }

  const confirmStop = async () => {
    if (!id) return
    setControlLoading(true)
    setStopConfirmOpen(false)
    try {
      const res = await fetch(`/api/simulations/${id}/stop`, { method: 'POST' })
      if (!res.ok) {
        const errData = await res.json()
        throw new Error(errData.error || 'Failed to stop simulation')
      }
      setState((prev) => (prev ? { ...prev, status: 'completed' } : null))
      toast.success('Simulation stopped')
    } catch (err: any) {
      toast.error(err.message)
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

  const handleAgentInterview = async (agentId: string, question: string): Promise<string> => {
    if (!id) return 'Error: no simulation ID'
    const res = await fetch(`/api/simulations/${id}/agents/${agentId}/ask`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ question }),
    })
    if (!res.ok) {
      const errData = await res.json().catch(() => ({}))
      throw new Error(errData.error || 'Failed to query agent')
    }
    const data = await res.json()
    return data.answer || 'No answer received.'
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
            <>
              <button
                onClick={() => setIsEditing(true)}
                className="flex items-center gap-1.5 rounded-lg border border-border/80 bg-muted/40 px-3 py-2 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-muted/60 transition-colors cursor-pointer"
              >
                <Edit className="h-3.5 w-3.5" />
                <span className="hidden sm:inline">Edit Config</span>
              </button>
              <button
                onClick={handleStart}
                disabled={controlLoading}
                className="flex items-center gap-2 rounded-lg bg-success hover:bg-success/90 disabled:bg-success/50 px-4 py-2 text-sm font-semibold text-success-foreground transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
              >
                <Play className="h-4 w-4" /> Start Simulation
              </button>
            </>
          )}
          {state.status === 'running' && (
            <button
              onClick={handleStopClick}
              disabled={controlLoading}
              className="flex items-center gap-2 rounded-lg bg-destructive hover:bg-destructive/90 disabled:bg-destructive/50 px-4 py-2 text-sm font-semibold text-destructive-foreground transition-colors cursor-pointer"
            >
              <Square className="h-4 w-4" /> Stop Simulation
            </button>
          )}
          {controlLoading && <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />}
        </div>
      </header>

      {/* Main Workspace (Grid layout) — stable 2-column layout */}
      <div className="flex flex-1 overflow-hidden min-h-0">
        {/* Left Side: Messages + Graph + Agents (always visible) */}
        <div className="flex-[3] flex flex-col overflow-y-auto p-4 md:p-6 gap-4 border-r border-border min-w-[320px]">
          {/* Compact Graph — collapsible on mobile */}
          {(!isMobile || !graphCollapsed) && (
            <div
              className={`shrink-0 rounded-xl border border-border/50 bg-card/20 overflow-hidden ${isMobile ? '' : 'h-[200px]'}`}
            >
              {/* Graph Layer Toggle */}
              <div className="flex items-center gap-1 px-3 pt-2 pb-1 border-b border-border/30">
                <button
                  onClick={() => setGraphLayer('interaction')}
                  className={`text-[10px] font-mono px-2 py-0.5 rounded transition-colors ${
                    graphLayer === 'interaction' || graphLayer === 'both'
                      ? 'bg-primary/15 text-primary font-semibold'
                      : 'text-muted-foreground hover:text-foreground'
                  }`}
                >
                  Interactions
                </button>
                <button
                  onClick={() => setGraphLayer('relationship')}
                  className={`text-[10px] font-mono px-2 py-0.5 rounded transition-colors ${
                    graphLayer === 'relationship' || graphLayer === 'both'
                      ? 'bg-primary/15 text-primary font-semibold'
                      : 'text-muted-foreground hover:text-foreground'
                  }`}
                >
                  Relationships
                </button>
                <button
                  onClick={() => setGraphLayer(graphLayer === 'both' ? 'interaction' : 'both')}
                  className={`text-[10px] font-mono px-2 py-0.5 rounded transition-colors ml-auto ${
                    graphLayer === 'both'
                      ? 'bg-primary/15 text-primary font-semibold'
                      : 'text-muted-foreground hover:text-foreground'
                  }`}
                  title="Toggle overlay"
                >
                  <Share2 className="h-3 w-3 inline mr-1" />
                  Overlay
                </button>
              </div>
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
                relationships={relationships}
                graphLayer={graphLayer}
                onSelectAgent={(agentId) => {
                  setSelectedAgentId((prev) => (prev === agentId ? null : agentId))
                  if (agentId) setProgressSidebarTab('activity')
                }}
                selectedAgentId={selectedAgentId}
                pulseNodes={pulseNodesRef.current}
                pulseVersion={pulseVersion}
              />
            </div>
          )}
          {isMobile && (
            <button
              onClick={() => setGraphCollapsed(!graphCollapsed)}
              className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
            >
              {graphCollapsed ? (
                <ChevronRight className="h-3.5 w-3.5" />
              ) : (
                <ChevronDown className="h-3.5 w-3.5" />
              )}
              {graphCollapsed ? 'Show Agent Graph' : 'Hide Agent Graph'}
            </button>
          )}

          {/* Agent pills (compact) */}
          <div className="shrink-0 flex flex-wrap gap-2">
            {state.config.personas.map((persona) => {
              const isSelected = selectedAgentId === persona.id
              return (
                <button
                  key={persona.id}
                  onClick={() => setSelectedAgentId(isSelected ? null : persona.id)}
                  className={`inline-flex items-center gap-1.5 rounded-lg border px-2.5 py-1.5 text-xs transition-all cursor-pointer ${
                    isSelected
                      ? 'border-primary/50 bg-primary/5 text-foreground'
                      : 'border-border bg-card/25 text-muted-foreground hover:text-foreground hover:border-border/80'
                  }`}
                >
                  <span className="font-semibold">{persona.name}</span>
                  <span className="text-[10px] text-muted-foreground/60 hidden sm:inline">
                    {persona.role}
                  </span>
                </button>
              )
            })}
            {state.status === 'completed' && state.report && (
              <button
                onClick={() => setChatAgentId('report')}
                className="inline-flex items-center gap-1.5 rounded-lg border border-primary/30 bg-primary/5 px-2.5 py-1.5 text-xs text-primary font-semibold cursor-pointer hover:border-primary/50 transition-all"
              >
                <Cpu className="h-3.5 w-3.5" />
                Ask Report Analyst
              </button>
            )}
          </div>

          {/* Messages (always visible) */}
          <div className="flex-1 min-h-0 space-y-4">
            {/* Report banner */}
            {state.report && !selectedAgentId && (
              <div className="rounded-xl border border-primary/20 bg-primary/5 p-4 backdrop-blur-sm">
                <div className="mb-2 flex items-center gap-2 border-b border-primary/10 pb-2">
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

            {/* Message list */}
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
            <div ref={messagesEndRef} />
          </div>
        </div>

        {/* Right Side: Agent Detail (when selected) or Monitor / Activity (when running) */}
        <div className="flex-[2] flex flex-col min-w-[280px] bg-muted/10 border-l border-border/45">
          {(() => {
            const selectedPersona = selectedAgentId
              ? state.config.personas.find((p) => p.id === selectedAgentId)
              : null

            if (selectedPersona) {
              return (
                <AgentDetailPanel
                  persona={selectedPersona}
                  messages={state.messages}
                  progress={progress}
                  relationships={relationships}
                  onClose={() => setSelectedAgentId(null)}
                  onInterview={(question) => handleAgentInterview(selectedPersona.id, question)}
                  status={state.status}
                />
              )
            }

            if (state.status === 'running' || state.status === 'failed') {
              return (
                <Tabs
                  value={progressSidebarTab}
                  onValueChange={(val) => setProgressSidebarTab(val as 'progress' | 'activity')}
                >
                  <TabsList className="flex border-b border-border w-full bg-transparent">
                    <TabsTrigger
                      value="progress"
                      className="flex-1 py-3 text-center text-xs font-semibold font-mono transition-colors border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-card/20"
                    >
                      MONITOR
                    </TabsTrigger>
                    <TabsTrigger
                      value="activity"
                      className="flex-1 py-3 text-center text-xs font-semibold font-mono transition-colors border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-card/20"
                    >
                      ACTIVITY
                    </TabsTrigger>
                  </TabsList>
                  <TabsContent
                    value={progressSidebarTab}
                    className="flex-1 overflow-y-auto p-4 space-y-4"
                  >
                    {progress ? (
                      progressSidebarTab === 'progress' ? (
                        <SimulationProgressPanel
                          progress={progress}
                          messages={state.messages}
                          selectedAgentId={selectedAgentId}
                          onSelectAgent={setSelectedAgentId}
                        />
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
                    )}
                  </TabsContent>
                </Tabs>
              )
            }

            // Pre/post-simulation placeholder — no tabs, clean & focused
            return (
              <div className="flex-1 flex flex-col items-center justify-center p-6 text-center">
                <Users className="h-8 w-8 text-muted-foreground/30 mb-3" />
                <p className="text-sm font-medium text-foreground/70 mb-1">
                  {state.status === 'pending'
                    ? 'Ready to start'
                    : state.status === 'completed'
                      ? 'Simulation complete'
                      : 'Waiting to start'}
                </p>
                <p className="text-xs text-muted-foreground/60 max-w-[240px] leading-relaxed">
                  {state.status === 'pending'
                    ? 'Configure parameters and click Start Simulation to begin.'
                    : state.status === 'completed'
                      ? 'Select an agent in the graph or list above to review their details, relationships, and activity.'
                      : 'Click Start Simulation to begin. Select an agent to view their profile and relationships.'}
                </p>
                {state.status === 'completed' && state.report && (
                  <p className="mt-3 text-[10px] text-muted-foreground/40 max-w-[240px]">
                    The final report is available in the main panel.
                  </p>
                )}
              </div>
            )
          })()}
        </div>
      </div>

      {/* Post-Simulation Chat Dialogue (Dialog) */}
      <Dialog
        open={!!chatAgentId}
        onOpenChange={(v) => {
          if (!v) {
            setChatAgentId(null)
            setChatQuestion('')
          }
        }}
      >
        <DialogContent className="max-w-lg max-h-[80vh] flex flex-col p-0 overflow-hidden gap-0">
          <DialogHeader className="shrink-0 px-5 py-4 border-b border-border/50">
            <DialogTitle className="flex items-center gap-2">
              <MessageSquare className="h-4 w-4 text-primary" />
              Chat with{' '}
              {chatAgentId === 'report'
                ? 'Report Analyst'
                : chatAgentId
                  ? state.config.personas.find((p) => p.id === chatAgentId)?.name
                  : ''}
            </DialogTitle>
            <p className="text-[10px] text-muted-foreground font-normal">
              Interview agent in-character regarding the simulation.
            </p>
          </DialogHeader>

          <div className="flex-1 overflow-y-auto p-5 space-y-4 min-h-0">
            <div className="rounded-lg bg-background/40 border border-border p-3 text-[10px] text-muted-foreground leading-relaxed">
              You can query the agent about their stance during the debate, their interactions, or
              the resulting report details.
            </div>

            {/* Local chat thread history */}
            {chatAgentId &&
              (chatHistory[chatAgentId] || []).map((chat, idx) => (
                <div key={idx} className="space-y-3">
                  <div className="flex justify-end">
                    <div className="rounded-lg bg-primary px-3 py-2 text-xs text-primary-foreground max-w-[85%] font-medium">
                      {chat.q}
                    </div>
                  </div>
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
            className="shrink-0 border-t border-border/50 p-4 bg-card/30 flex gap-2"
          >
            <input
              type="text"
              required
              placeholder="Ask a question..."
              value={chatQuestion}
              onChange={(e) => setChatQuestion(e.target.value)}
              className="flex-1 rounded-lg border border-border bg-background px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 focus:border-primary focus:ring-1 focus:ring-primary/20 focus:outline-none transition-all"
            />
            <button
              type="submit"
              className="rounded-lg bg-primary hover:bg-primary/90 p-2.5 text-primary-foreground transition-colors cursor-pointer shrink-0"
            >
              <Send className="h-4 w-4" />
            </button>
          </form>
        </DialogContent>
      </Dialog>

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

      {/* Config Edit Dialog */}
      <Dialog open={isEditing} onOpenChange={setIsEditing}>
        <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Settings className="h-4.5 w-4.5 text-primary" />
              Edit Simulation Parameters
            </DialogTitle>
          </DialogHeader>

          <div className="space-y-5 py-2">
            {/* Topic */}
            <div className="space-y-1.5">
              <Input
                label="Simulation Topic"
                value={editTopic}
                onChange={(e) => setEditTopic(e.target.value)}
                className="text-xs"
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
                <Select
                  label="Time Scale"
                  value={String(editTimeScale)}
                  onChange={(v) => setEditTimeScale(parseInt(v))}
                  options={[
                    { value: '60', label: '1s = 1min' },
                    { value: '300', label: '1s = 5min' },
                    { value: '600', label: '1s = 10min' },
                    { value: '1800', label: '1s = 30min' },
                    { value: '3600', label: '1s = 1h' },
                  ]}
                />
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

            {/* Agent Specific Models */}
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
                      <span className="text-xs font-semibold text-foreground">{persona.name}</span>
                      <span className="text-[9px] text-muted-foreground font-mono">
                        {persona.role}
                      </span>
                    </div>
                    <div className="grid grid-cols-2 gap-2">
                      <div>
                        <Select
                          label="Provider"
                          value={persona.provider_id || ''}
                          onChange={(v) => {
                            handleUpdatePersonaOverride(idx, 'provider_id', v)
                            handleUpdatePersonaOverride(idx, 'model_id', '')
                          }}
                          placeholder="(Default Fast Provider)"
                          options={[
                            { value: '', label: '(Default Fast Provider)' },
                            ...providers.map((p) => ({ value: p.id, label: p.name })),
                          ]}
                        />
                      </div>
                      <div>
                        <Select
                          label="Model"
                          value={persona.model_id || ''}
                          onChange={(v) => handleUpdatePersonaOverride(idx, 'model_id', v)}
                          placeholder="(Default Fast Model)"
                          options={[
                            { value: '', label: '(Default Fast Model)' },
                            ...models
                              .filter(
                                (m) => !persona.provider_id || m.providerId === persona.provider_id
                              )
                              .map((m) => ({ value: m.id, label: m.name })),
                          ]}
                        />
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </div>

          <DialogFooter showCloseButton={false}>
            <button
              type="button"
              onClick={() => setIsEditing(false)}
              disabled={savingConfig}
              className="rounded-lg bg-muted hover:bg-muted/80 px-4 py-2 text-xs font-semibold text-foreground transition-colors cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={handleSaveConfig}
              disabled={savingConfig}
              className="flex items-center justify-center gap-1.5 rounded-lg bg-primary hover:bg-primary/95 disabled:bg-primary/50 px-4 py-2 text-xs font-semibold text-primary-foreground transition-all cursor-pointer shadow-md shadow-primary/5 disabled:cursor-not-allowed"
            >
              {savingConfig ? (
                <>
                  <Loader2 className="h-3.5 w-3.5 animate-spin" /> Saving...
                </>
              ) : (
                <>
                  <Save className="h-3.5 w-3.5" /> Save Config
                </>
              )}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={stopConfirmOpen}
        onOpenChange={setStopConfirmOpen}
        title="Stop Simulation"
        message="Are you sure you want to stop this simulation? The current state will be saved, but any in-progress agent actions will be interrupted."
        destructive
        onConfirm={confirmStop}
        confirmLabel="Stop Simulation"
        loading={controlLoading}
      />
    </div>
  )
}
