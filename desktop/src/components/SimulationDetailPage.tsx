import { useEffect, useState, useRef, useCallback } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { wsManager } from '@/lib/websocket'
import { cn } from '@/lib/utils'
import { useRuntimeStore } from '@/stores/runtimeStore'
import { SimulationGraph, type GraphEdgeInput } from './SimulationGraph'
import { AgentDetailPanel } from './AgentDetailPanel'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import {
  Play,
  Square,
  ArrowLeft,
  MessageSquare,
  Send,
  Loader2,
  FileText,
  AlertCircle,
  Clock,
  Settings,
  Edit,
  Save,
  Pause,
  GitFork,
  Trash2,
  SkipForward,
  X,
  MessageCircle,
  MapPin,
  Lightbulb,
  Lock,
  AlertTriangle,
  LogOut,
  Skull,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  Zap,
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
import { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from '@/components/ui/tooltip'

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

const WORLD_STATE_KEYS_ZH: Record<string, string> = {
  _seed_locations: 'Seed Locations',
  _seed_topic: 'Seed Topic',
  conflict: 'Core Conflict',
  era: 'Era Setting',
  faction: 'Major Factions',
  factions: 'Faction Camps',
  location: 'Main Location',
  time: 'Time Period',
  world: 'World View / Background',
}

function getStatusLabel(status: string) {
  const map: Record<string, string> = {
    idle: 'Idle',
    pending: 'Pending',
    running: 'Running',
    paused: 'Paused',
    completed: 'Completed',
    failed: 'Failed',
    cancelled: 'Cancelled',
  }
  return map[status] ?? status
}

export function SimulationDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const sidebarCollapsed = useRuntimeStore((s) => s.sidebarCollapsed)

  const [state, setState] = useState<SimulationState | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [controlLoading, setControlLoading] = useState(false)

  // Redesign states
  const [activeTab, setActiveTab] = useState<'stream' | 'world' | 'report' | 'agent'>('stream')
  const [rightPanelWidth, setRightPanelWidth] = useState(420)
  const [rightPanelCollapsed, setRightPanelCollapsed] = useState(false)
  const [isResizing, setIsResizing] = useState(false)
  const [filterAgentId, setFilterAgentId] = useState<string | null>(null)
  const [expandedMessageSeqs, setExpandedMessageSeqs] = useState<Set<number>>(new Set())

  // Delete action confirm state
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false)

  // Fork action parameters states
  const [forkDialogOpen, setForkDialogOpen] = useState(false)
  const [forkTopic, setForkTopic] = useState('')
  const [forkMaxWallClockMin, setForkMaxWallClockMin] = useState(18)
  const [forking, setForking] = useState(false)

  // World state variables snapshot
  const [worldState, setWorldState] = useState<Record<string, any> | null>(null)
  const [worldSearch, setWorldSearch] = useState('')

  const fetchEnvironment = useCallback(async () => {
    if (!id) return
    try {
      const res = await fetch(`/api/simulations/${id}/environment`)
      if (res.ok) {
        const data = await res.json()
        setWorldState(data.world_state || null)
      }
    } catch (err) {
      console.error('Failed to fetch environment state', err)
    }
  }, [id])

  const startResize = useCallback((e: React.MouseEvent) => {
    e.preventDefault()
    setIsResizing(true)
  }, [])

  useEffect(() => {
    const handleMouseMove = (e: MouseEvent) => {
      if (!isResizing) return
      const newWidth = window.innerWidth - e.clientX
      if (newWidth >= 320 && newWidth <= 580) {
        setRightPanelWidth(newWidth)
      }
    }
    const handleMouseUp = () => {
      setIsResizing(false)
    }
    if (isResizing) {
      window.addEventListener('mousemove', handleMouseMove)
      window.addEventListener('mouseup', handleMouseUp)
    }
    return () => {
      window.removeEventListener('mousemove', handleMouseMove)
      window.removeEventListener('mouseup', handleMouseUp)
    }
  }, [isResizing])

  // Configuration Edit States
  const [isEditing, setIsEditing] = useState(false)
  const [stopConfirmOpen, setStopConfirmOpen] = useState(false)
  const [editTopic, setEditTopic] = useState('')
  const [editMaxWallClockMin, setEditMaxWallClockMin] = useState(18)
  const [editSimHours, setEditSimHours] = useState(168)
  const [editTimeScale, setEditTimeScale] = useState(300)
  const [editEnableReflection, setEditEnableReflection] = useState(true)
  const [editPersonas, setEditPersonas] = useState<any[]>([])
  const [editLanguage, setEditLanguage] = useState('zh')
  const [savingConfig, setSavingConfig] = useState(false)
  const [relationships, setRelationships] = useState<RelationshipDTO[]>([])
  const [graphLayer, setGraphLayer] = useState<'interaction' | 'relationship' | 'both'>('both')

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
  const [isReportModalOpen, setIsReportModalOpen] = useState(false)
  const [reportQuestion, setReportQuestion] = useState('')
  const [reportInterviewing, setReportInterviewing] = useState(false)

  // Progress display state
  const [progress, setProgress] = useState<SimulationProgress | null>(null)
  const [graphEdges, setGraphEdges] = useState<GraphEdgeInput[]>([])
  // Use ref for pulse nodes to avoid render storms (#5). The graph reads via ref,
  // triggered by a lightweight counter state (avoids Set recreation).
  const pulseNodesRef = useRef<Set<string>>(new Set())
  const [pulseVersion, setPulseVersion] = useState(0)

  const messagesEndRef = useRef<HTMLDivElement | null>(null)
  const pulseTimersRef = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map())
  const completionPollRef = useRef<ReturnType<typeof setInterval> | null>(null)

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
          console.error('Failed to load LLM config', err)
          toast.error('Failed to load LLM config')
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
      setEditLanguage(state.config.language || 'zh')
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
          language: editLanguage,
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
      fetchEnvironment()
    } catch (err: any) {
      setError(err.message || 'Failed to fetch details')
    } finally {
      setLoading(false)
    }
  }, [id, fetchEnvironment])

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
        fetchEnvironment()
      } else if (ev.type === 'paused') {
        setState((prev) => (prev ? { ...prev, status: 'paused' } : null))
        fetchEnvironment()
      } else if (ev.type === 'resumed') {
        setState((prev) => (prev ? { ...prev, status: 'running' } : null))
      } else if (ev.type === 'simulation_end') {
        setState((prev) => (prev ? { ...prev, status: 'completed' } : null))
        setProgress((prev) =>
          prev
            ? {
                ...prev,
                phase: 'completed',
                progress_percent: 100,
              }
            : null
        )
        fetchEnvironment()
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
        // Immediate fallback: set status before fetchState() completes
        setState((prev) => (prev ? { ...prev, status: 'completed' } : null))
        if (completionPollRef.current) {
          clearInterval(completionPollRef.current)
          completionPollRef.current = null
        }
        fetchState()
      }
    })

    // Subscribe to real-time progress updates
    const unsubProgress = wsManager.subscribe('simulation_progress', (p: SimulationProgress) => {
      if (p.simulation_id !== id) return
      setProgress(p)

      // When the server reports completion or failure via progress, also
      // update the local status so the UI stops showing "Stop Simulation".
      if (p.phase === 'completed' || p.phase === 'failed') {
        setState((prev) =>
          prev ? { ...prev, status: p.phase === 'completed' ? 'completed' : 'failed' } : null
        )
        fetchEnvironment()
        // Stop polling if active
        if (completionPollRef.current) {
          clearInterval(completionPollRef.current)
          completionPollRef.current = null
        }
      } else if (p.phase === 'paused') {
        setState((prev) => (prev ? { ...prev, status: 'paused' } : null))
        fetchEnvironment()
      } else if (p.phase === 'running') {
        setState((prev) => (prev ? { ...prev, status: 'running' } : null))
      } else if (p.phase === 'generating_report') {
        // Report generation takes time (LLM calls). If the WebSocket drops
        // during this period, the 'completed'/'finished' events will be lost.
        // Poll the REST API as a fallback.
        if (!completionPollRef.current) {
          completionPollRef.current = setInterval(async () => {
            if (!id) return
            try {
              const res = await fetch(`/api/simulations/${id}`)
              if (!res.ok) return
              const data = await res.json()
              if (data.status === 'completed' || data.status === 'failed') {
                setState((prev) =>
                  prev ? { ...prev, status: data.status, report: data.report || prev.report } : null
                )
                if (completionPollRef.current) {
                  clearInterval(completionPollRef.current)
                  completionPollRef.current = null
                }
              }
            } catch {
              // Ignore polling errors — will retry on next interval
            }
          }, 3000)
        }
      }

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
      // Clear completion polling
      if (completionPollRef.current) {
        clearInterval(completionPollRef.current)
        completionPollRef.current = null
      }
    }
  }, [id, fetchState, fetchEnvironment])

  // Scroll to bottom of message list on new messages
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [state?.messages])

  const handlePause = async () => {
    if (!id) return
    try {
      setControlLoading(true)
      const res = await fetch(`/api/simulations/${id}/pause`, { method: 'POST' })
      if (!res.ok) {
        const errData = await res.json()
        throw new Error(errData.error || 'Failed to pause simulation')
      }
      setState((prev) => (prev ? { ...prev, status: 'paused' } : null))
      toast.success('Simulation paused')
    } catch (err: any) {
      toast.error(err.message)
    } finally {
      setControlLoading(false)
    }
  }

  const handleResume = async () => {
    if (!id) return
    try {
      setControlLoading(true)
      const res = await fetch(`/api/simulations/${id}/resume`, { method: 'POST' })
      if (!res.ok) {
        const errData = await res.json()
        throw new Error(errData.error || 'Failed to resume simulation')
      }
      setState((prev) => (prev ? { ...prev, status: 'running' } : null))
      toast.success('Simulation resumed')
    } catch (err: any) {
      toast.error(err.message)
    } finally {
      setControlLoading(false)
    }
  }

  const handleStep = async () => {
    if (!id) return
    try {
      setControlLoading(true)
      const res = await fetch(`/api/simulations/${id}/step`, { method: 'POST' })
      if (!res.ok) {
        const errData = await res.json()
        throw new Error(errData.error || 'Failed to step simulation')
      }
      toast.success('Simulation stepped forward one round')
    } catch (err: any) {
      toast.error(err.message)
    } finally {
      setControlLoading(false)
    }
  }

  const handleDelete = async () => {
    if (!id) return
    try {
      setControlLoading(true)
      const res = await fetch(`/api/simulations/${id}`, { method: 'DELETE' })
      if (!res.ok) {
        const errData = await res.json()
        throw new Error(errData.error || 'Failed to delete simulation')
      }
      toast.success('Simulation deleted')
      navigate('/simulations')
    } catch (err: any) {
      toast.error(err.message)
    } finally {
      setControlLoading(false)
    }
  }

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

  const handleReportAsk = async (question: string) => {
    if (!id || !question.trim() || reportInterviewing) return

    setReportInterviewing(true)
    setChatHistory((prev) => ({
      ...prev,
      report: capChatHistory([...(prev['report'] || []), { q: question, a: '', loading: true }]),
    }))

    try {
      const res = await fetch(`/api/simulations/${id}/agents/report/ask`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ question }),
      })
      if (!res.ok) throw new Error('Failed to query report expert')
      const data = await res.json()
      setChatHistory((prev) => {
        const history = [...(prev['report'] || [])]
        const idx = history.findIndex((h) => h.q === question && h.loading)
        if (idx !== -1) history[idx] = { q: question, a: data.answer || 'No answer received.' }
        return { ...prev, report: capChatHistory(history) }
      })
    } catch (err: any) {
      setChatHistory((prev) => {
        const history = [...(prev['report'] || [])]
        const idx = history.findIndex((h) => h.q === question && h.loading)
        if (idx !== -1) history[idx] = { q: question, a: `Error: ${err.message || 'Request failed'}` }
        return { ...prev, report: capChatHistory(history) }
      })
    } finally {
      setReportInterviewing(false)
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

  // ── Message type visual configuration ──────────────────────────────────
  const MESSAGE_TYPE_CONFIG: Record<string, {
    icon: React.ElementType
    borderColor: string   // left border class
    badgeBg: string
    badgeText: string
    label: string
  }> = {
    speak: {
      icon: MessageCircle,
      borderColor: 'border-l-blue-500/50',
      badgeBg: 'bg-blue-500/10',
      badgeText: 'text-blue-600 dark:text-blue-400',
      label: 'Dialogue',
    },
    private_speak: {
      icon: Lock,
      borderColor: 'border-l-violet-500/50',
      badgeBg: 'bg-violet-500/10',
      badgeText: 'text-violet-600 dark:text-violet-400',
      label: 'Whisper',
    },
    agent_move: {
      icon: MapPin,
      borderColor: 'border-l-amber-500/50',
      badgeBg: 'bg-amber-500/10',
      badgeText: 'text-amber-600 dark:text-amber-400',
      label: 'Movement',
    },
    reflection: {
      icon: Lightbulb,
      borderColor: 'border-l-emerald-500/50',
      badgeBg: 'bg-emerald-500/10',
      badgeText: 'text-emerald-600 dark:text-emerald-400',
      label: 'Reflection',
    },
    conflict: {
      icon: AlertTriangle,
      borderColor: 'border-l-rose-500/50',
      badgeBg: 'bg-rose-500/10',
      badgeText: 'text-rose-600 dark:text-rose-400',
      label: 'Conflict',
    },
    rebuttal: {
      icon: AlertCircle,
      borderColor: 'border-l-rose-400/50',
      badgeBg: 'bg-rose-400/10',
      badgeText: 'text-rose-500 dark:text-rose-400',
      label: 'Rebuttal',
    },
    question: {
      icon: MessageCircle,
      borderColor: 'border-l-cyan-500/50',
      badgeBg: 'bg-cyan-500/10',
      badgeText: 'text-cyan-600 dark:text-cyan-400',
      label: 'Question',
    },
    auto_pass: {
      icon: SkipForward,
      borderColor: 'border-l-gray-400/30 border-dashed',
      badgeBg: 'bg-gray-400/10',
      badgeText: 'text-gray-500 dark:text-gray-400',
      label: 'Routine',
    },
    agent_exit: {
      icon: LogOut,
      borderColor: 'border-l-gray-500/40',
      badgeBg: 'bg-gray-500/10',
      badgeText: 'text-gray-600 dark:text-gray-400',
      label: 'Exit',
    },
    agent_death_announcement: {
      icon: Skull,
      border