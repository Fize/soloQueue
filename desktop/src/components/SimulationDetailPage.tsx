import { useEffect, useState, useRef, useCallback } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { wsManager } from '@/lib/websocket'
import { cn } from '@/lib/utils'
import { useRuntimeStore } from '@/stores/runtimeStore'
import { SimulationGraph, type GraphEdgeInput } from './SimulationGraph'
import { AgentDetailPanel } from './AgentDetailPanel'
import { SimulationReportModal } from './SimulationReportModal'
import { SimulationConfigEditor } from './SimulationConfigEditor'
import { SimulationForkDialog } from './SimulationForkDialog'
import { MarkdownPreview as ReactMarkdown } from '@/components/ui/markdown-preview'
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
  Edit,
  Pause,
  GitFork,
  Trash2,
  SkipForward,
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
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from '@/components/ui/tooltip'
import { useTranslation } from '@/lib/i18n'

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
  const { t } = useTranslation()

  const WORLD_STATE_KEYS_ZH: Record<string, string> = {
    _seed_locations: t('simulation.seedLocations'),
    _seed_topic: t('simulation.seedTopic'),
    conflict: t('simulation.conflict'),
    era: t('simulation.era'),
    faction: t('simulation.faction'),
    factions: t('simulation.factions'),
    location: t('simulation.location'),
    time: t('simulation.time'),
    world: t('simulation.world'),
  }

  function getStatusLabel(status: string) {
    const map: Record<string, string> = {
      idle: t('common.idle'),
      pending: t('common.pending'),
      running: t('common.running'),
      paused: t('common.paused'),
      completed: t('common.completed'),
      failed: t('common.failed'),
      cancelled: t('common.cancelled'),
    }
    return map[status] ?? status
  }
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
  const [forkInitialTopic, setForkInitialTopic] = useState('')
  const [forkInitialMaxWallClockMin, setForkInitialMaxWallClockMin] = useState(18)

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
    const handleMouseUp = () => {
      setIsResizing(false)
    }
    const handleMouseMove = (e: MouseEvent) => {
      if (e.buttons === 0) {
        handleMouseUp()
        return
      }
      if (!isResizing) return
      const newWidth = window.innerWidth - e.clientX
      if (newWidth >= 320 && newWidth <= 580) {
        setRightPanelWidth(newWidth)
      }
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
          console.error(t('simulation.loadLLMConfigFailed'), err)
          toast.error(t('simulation.loadLLMConfigFailed'))
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
        throw new Error(errData.error || t('simulation.updateConfigFailed'))
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
      toast.error(err.message || t('simulation.saveConfigFailed'))
    } finally {
      setSavingConfig(false)
    }
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
        throw new Error(errData.error || t('simulation.pauseSimFailed'))
      }
      setState((prev) => (prev ? { ...prev, status: 'paused' } : null))
      toast.success(t('simulation.simPaused'))
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
        throw new Error(errData.error || t('simulation.resumeSimFailed'))
      }
      setState((prev) => (prev ? { ...prev, status: 'running' } : null))
      toast.success(t('simulation.simResumed'))
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
        throw new Error(errData.error || t('simulation.stepFailed'))
      }
      toast.success(t('simulation.stepSuccess'))
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
        throw new Error(errData.error || t('simulation.deleteSimFailed'))
      }
      toast.success(t('simulation.simDeleted'))
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
        throw new Error(errData.error || t('simulation.startSimFailed'))
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
        throw new Error(errData.error || t('simulation.stopSimFailed'))
      }
      setState((prev) => (prev ? { ...prev, status: 'completed' } : null))
      toast.success(t('simulation.simStopped'))
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
        if (idx !== -1) history[idx] = { q: question, a: `Error: ${err.message || t('common.error')}` }
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
        <Loader2 className="h-8 w-8 animate-spin text-signal" />
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
        return 'bg-success/10 text-success border border-success/25'
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
      label: t('simulation.eventTalk'),
    },
    private_speak: {
      icon: Lock,
      borderColor: 'border-l-primary/50',
      badgeBg: 'bg-primary/10',
      badgeText: 'text-primary',
      label: t('simulation.eventWhisper'),
    },
    agent_move: {
      icon: MapPin,
      borderColor: 'border-l-amber-500/50',
      badgeBg: 'bg-amber-500/10',
      badgeText: 'text-amber-600 dark:text-amber-400',
      label: t('simulation.eventMove'),
    },
    reflection: {
      icon: Lightbulb,
      borderColor: 'border-l-success/50',
      badgeBg: 'bg-success/10',
      badgeText: 'text-success',
      label: t('simulation.eventReflect'),
    },
    conflict: {
      icon: AlertTriangle,
      borderColor: 'border-l-rose-500/50',
      badgeBg: 'bg-rose-500/10',
      badgeText: 'text-rose-600 dark:text-rose-400',
      label: t('simulation.eventConflict'),
    },
    rebuttal: {
      icon: AlertCircle,
      borderColor: 'border-l-rose-400/50',
      badgeBg: 'bg-rose-400/10',
      badgeText: 'text-rose-500 dark:text-rose-400',
      label: t('simulation.eventRefute'),
    },
    question: {
      icon: MessageCircle,
      borderColor: 'border-l-cyan-500/50',
      badgeBg: 'bg-cyan-500/10',
      badgeText: 'text-cyan-600 dark:text-cyan-400',
      label: t('simulation.eventAsk'),
    },
    auto_pass: {
      icon: SkipForward,
      borderColor: 'border-l-gray-400/30 border-dashed',
      badgeBg: 'bg-muted',
      badgeText: 'text-muted-foreground',
      label: t('simulation.eventRoutine'),
    },
    agent_exit: {
      icon: LogOut,
      borderColor: 'border-l-gray-500/40',
      badgeBg: 'bg-muted',
      badgeText: 'text-muted-foreground',
      label: t('simulation.eventExit'),
    },
    agent_death_announcement: {
      icon: Skull,
      borderColor: 'border-l-red-600/50',
      badgeBg: 'bg-red-600/10',
      badgeText: 'text-red-600 dark:text-red-400',
      label: t('simulation.eventDeath'),
    },
  }

  function getTypeConfig(type: string) {
    return MESSAGE_TYPE_CONFIG[type] || {
      icon: MessageSquare,
      borderColor: 'border-l-muted-foreground/30',
      badgeBg: 'bg-muted',
      badgeText: 'text-muted-foreground',
      label: type,
    }
  }

  const renderWorldStateValue = (key: string, val: any) => {
    if (val === null || val === undefined)
      return <span className="text-muted-foreground/60">{t('common.none')}</span>

    let parsedVal = val
    if (typeof val === 'string') {
      const trimmed = val.trim()
      if (
        (trimmed.startsWith('[') && trimmed.endsWith(']')) ||
        (trimmed.startsWith('{') && trimmed.endsWith('}'))
      ) {
        try {
          parsedVal = JSON.parse(trimmed)
        } catch {
          // Not a valid JSON, keep as is
        }
      }
    }

    // 针对地点或种子地点进行精美格式化
    if (key === '_seed_locations' || key === 'locations') {
      if (Array.isArray(parsedVal)) {
        return (
          <div className="flex flex-wrap gap-2.5 py-1.5 select-text">
            {parsedVal.map((loc: any, idx: number) => {
              if (typeof loc === 'string') {
                return (
                  <span
                    key={idx}
                    className="inline-flex items-center gap-1 px-2.5 py-1 rounded-lg bg-primary/10 border border-primary/20 text-primary font-semibold text-[11px] hover:bg-primary/15 transition-all shadow-sm"
                  >
                    📍 {loc}
                  </span>
                )
              } else if (typeof loc === 'object' && loc !== null) {
                const name = loc.name || loc.Name || t('simulation.locationIndex', { index: idx + 1 })
                const desc = loc.desc || loc.desc || loc.description || loc.Description || ''
                return (
                  <div
                    key={idx}
                    className="flex flex-col gap-0.5 px-3 py-1.5 rounded-lg bg-card border border-border/60 hover:border-primary/40 shadow-sm transition-all min-w-[125px] max-w-[200px]"
                  >
                    <div className="flex items-center gap-1 font-bold text-foreground text-xs">
                      <span className="text-primary text-[11px]">📍</span>
                      <span>{name}</span>
                    </div>
                    {desc && desc !== name && (
                      <div
                        className="text-[10px] text-muted-foreground leading-normal truncate"
                        title={desc}
                      >
                        {desc}
                      </div>
                    )}
                  </div>
                )
              }
              return <div key={idx}>{JSON.stringify(loc)}</div>
            })}
          </div>
        )
      } else if (typeof parsedVal === 'object' && parsedVal !== null) {
        return (
          <div className="flex flex-wrap gap-2.5 py-1.5 select-text">
            {Object.entries(parsedVal).map(([name, desc], idx) => {
              const description = typeof desc === 'string' ? desc : JSON.stringify(desc)
              return (
                <div
                  key={idx}
                  className="flex flex-col gap-0.5 px-3 py-1.5 rounded-lg bg-card border border-border/60 hover:border-primary/40 shadow-sm transition-all min-w-[125px] max-w-[200px]"
                >
                  <div className="flex items-center gap-1 font-bold text-foreground text-xs">
                    <span className="text-primary text-[11px]">📍</span>
                    <span>{name}</span>
                  </div>
                  {description && description !== name && (
                    <div
                      className="text-[10px] text-muted-foreground leading-normal truncate"
                      title={description}
                    >
                      {description}
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        )
      } else if (typeof parsedVal === 'string') {
        const parts = parsedVal
          .split(/[,，;\s]+/)
          .map((s: string) => s.trim())
          .filter(Boolean)
        if (parts.length > 0) {
          return (
            <div className="flex flex-wrap gap-1.5 py-1">
              {parts.map((loc, idx) => (
                <span
                  key={idx}
                  className="inline-flex items-center gap-1 px-2.5 py-1 rounded-lg bg-primary/10 border border-primary/20 text-primary font-semibold text-[11px] hover:bg-primary/15 transition-all shadow-sm"
                >
                  📍 {loc}
                </span>
              ))}
            </div>
          )
        }
      }
    }

    if (Array.isArray(parsedVal)) {
      if (parsedVal.every((item) => typeof item === 'string' || typeof item === 'number')) {
        return (
          <div className="flex flex-wrap gap-1.5 py-1">
            {parsedVal.map((item, idx) => (
              <span
                key={idx}
                className="px-1.5 py-0.5 rounded bg-muted text-foreground/80 border border-border/30 text-[10px] font-mono"
              >
                {String(item)}
              </span>
            ))}
          </div>
        )
      }
      return (
        <pre className="text-[10px] bg-muted/10 p-2 rounded border border-border/40 max-h-48 overflow-y-auto font-mono whitespace-pre select-text">
          {JSON.stringify(parsedVal, null, 2)}
        </pre>
      )
    }

    if (typeof parsedVal === 'object' && parsedVal !== null) {
      const entries = Object.entries(parsedVal)
      if (entries.every(([_, v]) => typeof v !== 'object' || v === null)) {
        return (
          <div className="grid grid-cols-1 gap-1 py-1 text-[10px] select-text">
            {entries.map(([k, v]) => (
              <div key={k} className="flex gap-2">
                <span className="text-muted-foreground font-medium shrink-0">{k}:</span>
                <span className="text-foreground/90 font-mono break-all">{String(v)}</span>
              </div>
            ))}
          </div>
        )
      }
      return (
        <pre className="text-[10px] bg-muted/10 p-2 rounded border border-border/40 max-h-48 overflow-y-auto font-mono whitespace-pre select-text">
          {JSON.stringify(parsedVal, null, 2)}
        </pre>
      )
    }

    return <span className="font-mono">{String(parsedVal)}</span>
  }

  const renderWorldState = () => {
    if (!worldState || Object.keys(worldState).length === 0) {
      return (
        <div className="flex h-32 flex-col items-center justify-center text-center text-muted-foreground font-mono text-xs p-6">
          <AlertCircle className="mb-2 h-5 w-5 text-muted-foreground/60" />
          <span>{t('simulation.noEnvVars')}</span>
        </div>
      )
    }

    const filteredKeys = Object.keys(worldState)
      .filter((k) => k.toLowerCase().includes(worldSearch.toLowerCase()))
      .sort()

    return (
      <div className="flex-1 flex flex-col min-h-0 overflow-hidden p-4 space-y-3">
        <input
          type="text"
          placeholder={t('simulation.filterVars')}
          value={worldSearch}
          onChange={(e) => setWorldSearch(e.target.value)}
          className="w-full shrink-0 rounded-lg border border-border bg-background px-3 py-1.5 text-xs text-foreground placeholder:text-muted-foreground/50 focus:border-primary focus:outline-none transition-all"
        />
        <div className="flex-1 overflow-y-auto min-h-0 border border-border/50 rounded-lg bg-card/10">
          {filteredKeys.length === 0 ? (
            <div className="text-center text-xs font-mono text-muted-foreground py-6">
              {t('simulation.noMatchingVars')}
            </div>
          ) : (
            <table className="w-full text-xs font-sans border-collapse select-text">
              <thead>
                <tr className="border-b border-border/80 bg-muted/40 text-left text-muted-foreground">
                  <th className="p-3 py-2 font-semibold">{t('simulation.varName')}</th>
                  <th className="p-3 py-2 font-semibold">{t('simulation.varValue')}</th>
                </tr>
              </thead>
              <tbody>
                {filteredKeys.map((key) => {
                  const val = worldState[key]
                  const displayName = WORLD_STATE_KEYS_ZH[key] || key
                  const hasAlias = !!WORLD_STATE_KEYS_ZH[key]
                  return (
                    <tr
                      key={key}
                      className="border-b border-border/40 hover:bg-muted/10 transition-colors"
                    >
                      <td className="p-3 py-2.5 align-top max-w-[150px] shrink-0">
                        <div className="text-primary font-semibold text-xs leading-normal">
                          {displayName}
                        </div>
                        {hasAlias && (
                          <div className="text-[10px] text-muted-foreground/60 font-mono font-normal mt-0.5">
                            {key}
                          </div>
                        )}
                      </td>
                      <td className="p-3 py-2.5 text-foreground/90 break-all whitespace-pre-wrap align-top font-sans leading-normal">
                        {renderWorldStateValue(key, val)}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          )}
        </div>
      </div>
    )
  }

  const activeAgentIds = progress?.agent_states
    ? new Set(Object.keys(progress.agent_states))
    : undefined

  const filteredMessages = filterAgentId
    ? state.messages.filter((m) => m.agent_id === filterAgentId)
    : state.messages

  const formatTime = (seconds: number) => {
    const m = Math.floor(seconds / 60)
    const s = Math.floor(seconds % 60)
    return `${m}m ${s}s`
  }

  const phaseSteps = [
    { key: 'initializing', label: t('simulation.phaseInitializing') },
    { key: 'generating_plans', label: t('simulation.phaseGeneratingPlans') },
    { key: 'building_prompts', label: t('simulation.phaseBuildingPrompts') },
    { key: 'running', label: t('simulation.phaseRunning') },
    { key: 'generating_report', label: t('simulation.phaseGeneratingReport') },
    { key: 'completed', label: t('simulation.phaseCompleted') },
  ]

  const currentPhaseIdx = progress ? phaseSteps.findIndex(s => s.key === progress.phase) : -1

  const handleSelectAgent = (agentId: string | null) => {
    setSelectedAgentId(agentId)
    if (agentId) {
      setActiveTab('agent')
    }
  }

  // Render routine pass indicator inline or expanded
  const renderMessageItem = (msg: SimulationMessage, idx: number) => {
    const cfg = getTypeConfig(msg.type)
    const Icon = cfg.icon
    const isRoutine = msg.type === 'auto_pass'
    const expanded = expandedMessageSeqs.has(msg.seq_num || 0)

    if (isRoutine && !expanded) {
      return (
        <div key={`${msg.seq_num}-${idx}`} className="group rounded-lg border border-dashed border-border/45 bg-muted/5 p-2 text-xs flex items-center justify-between transition-all hover:bg-muted/10 animate-in fade-in duration-200">
          <span className="text-muted-foreground truncate flex items-center gap-1.5">
            <SkipForward className="h-3 w-3 shrink-0" />
            <span className="font-semibold text-foreground/80">{msg.agent_name}</span>
            <span>{t('simulation.routineActionPassed')}</span>
          </span>
          <button
            type="button"
            onClick={() => {
              setExpandedMessageSeqs((prev) => {
                const next = new Set(prev)
                next.add(msg.seq_num || 0)
                return next
              })
            }}
            className="text-[10px] text-primary hover:underline font-mono cursor-pointer shrink-0"
          >
            {t('simulation.expand')}
          </button>
        </div>
      )
    }

    return (
      <div
        key={`${msg.seq_num}-${idx}`}
        className={cn(
          "group flex flex-col gap-1.5 rounded-xl bg-card/45 border border-border/60 pl-3.5 pr-3 py-3 transition-all hover:bg-card/75 relative animate-in fade-in slide-in-from-right-4 duration-300",
          cfg.borderColor,
          "border-l-[3.5px]"
        )}
      >
        {/* Ask overlay button on hover */}
        <div className="absolute right-3 top-3 opacity-0 group-hover:opacity-100 transition-opacity z-10">
          <button
            type="button"
            onClick={() => setChatAgentId(msg.agent_id)}
            className="p-1 rounded-md bg-primary/15 border border-primary/25 hover:bg-primary/25 text-primary hover:text-primary-foreground transition-all cursor-pointer shadow-sm flex items-center justify-center"
            title={t('simulation.askAgent', { name: msg.agent_name })}
          >
            <MessageSquare className="h-3.5 w-3.5" />
          </button>
        </div>

        {/* Header Info */}
        <div className="flex items-center justify-between gap-2 mr-6">
          <div className="flex items-center gap-2 min-w-0">
            <div className="h-5 w-5 rounded-full bg-primary/10 text-primary font-bold text-[9px] flex items-center justify-center shrink-0 border border-primary/20">
              {msg.agent_name?.charAt(0)?.toUpperCase()}
            </div>
            <span className="font-semibold text-foreground text-xs truncate">
              {msg.agent_name}
            </span>
          </div>

          <div className="flex items-center gap-1.5 shrink-0">
            <span
              className={cn(
                "inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-[9px] font-semibold font-mono leading-none",
                cfg.badgeBg,
                cfg.badgeText
              )}
            >
              <Icon className="h-2.5 w-2.5" />
              {cfg.label}
            </span>
          </div>
        </div>

        {/* Content Text */}
        <div className="text-xs text-foreground/90 leading-relaxed font-sans prose prose-sm dark:prose-invert max-w-none select-text">
          <ReactMarkdown>{msg.content}</ReactMarkdown>
        </div>

        {/* Collapsible toggle if routine */}
        {isRoutine && (
          <button
            type="button"
            onClick={() => {
              setExpandedMessageSeqs((prev) => {
                const next = new Set(prev)
                next.delete(msg.seq_num || 0)
                return next
              })
            }}
            className="text-[10px] text-muted-foreground/60 hover:text-foreground font-mono self-start cursor-pointer hover:underline"
          >
            {t('simulation.collapseRoutine')}
          </button>
        )}

        {/* Reasoning (collapsible details) */}
        {msg.reasoning && (
          <details className="mt-1 group">
            <summary className="text-[9px] text-muted-foreground/50 cursor-pointer select-none hover:text-foreground font-mono tracking-wide flex items-center gap-1">
              <span className="inline-block w-0 h-0 border-l-4 border-l-transparent border-t-4 border-t-current border-r-4 border-r-transparent group-open:rotate-90 transition-transform" />
              {t('simulation.reasoningProcess')}
            </summary>
            <div className="mt-1 text-[9px] text-muted-foreground/80 bg-background/55 p-3 rounded-lg border border-border/30 leading-relaxed whitespace-pre-wrap">
              {msg.reasoning}
            </div>
          </details>
        )}
      </div>
    )
  }

  // Render grouped messages round by round
  const renderGroupedMessages = () => {
    if (filteredMessages.length === 0) {
      return (
        <div className="flex h-full min-h-[200px] flex-col items-center justify-center text-center text-muted-foreground font-mono text-xs gap-3">
          <Clock className="h-5 w-5 text-muted-foreground/60 animate-pulse" />
          <span>{t('simulation.waitingForStart')}</span>
        </div>
      )
    }

    // Group by round
    const groups: Record<number, SimulationMessage[]> = {}
    filteredMessages.forEach((m) => {
      const r = m.round || 0
      if (!groups[r]) groups[r] = []
      groups[r].push(m)
    })

    const rounds = Object.keys(groups).map(Number).sort((a, b) => a - b)

    return (
      <div className="space-y-6">
        {rounds.map((r) => (
          <div key={r} className="space-y-3">
            {/* Round Header Label */}
            <div className="flex items-center gap-2 select-none">
              <div className="h-px flex-1 bg-border/40" />
              <span className="text-[10px] font-bold text-muted-foreground font-mono uppercase bg-muted/40 px-2 py-0.5 rounded border border-border/20">
                {r === 0 ? t('simulation.phaseInitialStage') : t('simulation.roundStatus', { round: r })}
              </span>
              <div className="h-px flex-1 bg-border/40" />
            </div>

            {/* Messages of this round */}
            <div className="space-y-3">
              {groups[r].map((msg, idx) => renderMessageItem(msg, idx))}
            </div>
          </div>
        ))}
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col bg-background text-foreground overflow-hidden font-sans">
      {/* Top Header Controls */}
      <header className={cn(
        "flex shrink-0 items-center justify-between border-b border-border bg-card/30 px-6 py-2.5",
        sidebarCollapsed && "pl-[115px]"
      )}>
        <div className="flex items-center gap-4 min-w-0 electron-no-drag">
          <button
            onClick={() => navigate('/simulations')}
            className="rounded-lg p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground transition-colors cursor-pointer"
          >
            <ArrowLeft className="h-4 w-4" />
          </button>
          <div className="min-w-0">
            <h1 className="text-sm font-bold text-foreground truncate leading-snug">{state.config.topic}</h1>
            <div className="mt-0.5 flex items-center gap-1.5 text-[10px] text-muted-foreground font-mono">
              <span>ID: {state.id.slice(0, 8)}</span>
              <span>•</span>
              <span className={cn(
                "px-1.5 py-0.2 rounded text-[8px] font-bold uppercase border",
                getStatusBadgeClass(state.status)
              )}>
                {getStatusLabel(state.status)}
              </span>
              {state.status === 'running' && (
                <>
                  <span>•</span>
                  <span className="text-primary animate-pulse font-bold">
                    {state.current_round === 0 ? t('simulation.initializing') : t('simulation.roundNumber', { count: state.current_round })}
                  </span>
                </>
              )}
            </div>
          </div>
        </div>

        {/* Start / Stop / Pause / Resume / Step / Fork / Delete Controls */}
        <div className="flex items-center gap-2 electron-no-drag">
          <TooltipProvider>
            {(state.status === 'idle' || state.status === 'pending') && (
              <>
                <Tooltip>
                  <TooltipTrigger>
                    <button
                      onClick={() => setIsEditing(true)}
                      className="flex items-center justify-center h-8 w-8 rounded-lg border border-border/80 bg-muted/40 text-muted-foreground hover:text-foreground hover:bg-muted/60 transition-colors cursor-pointer"
                    >
                      <Edit className="h-4 w-4" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>{t('simulation.editConfig')}</TooltipContent>
                </Tooltip>
                <Tooltip>
                  <TooltipTrigger>
                    <button
                      onClick={handleStart}
                      disabled={controlLoading}
                      className="flex items-center justify-center h-8 px-3 rounded-lg bg-success hover:bg-success/90 disabled:bg-success/50 text-xs font-semibold text-success-foreground transition-colors cursor-pointer disabled:opacity-50"
                    >
                      <Play className="h-3.5 w-3.5 mr-1" /> {t('simulation.startSim')}
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>{t('simulation.startDeduction')}</TooltipContent>
                </Tooltip>
              </>
            )}

            {state.status === 'running' && (
              <>
                <Tooltip>
                  <TooltipTrigger>
                    <button
                      onClick={handlePause}
                      disabled={controlLoading}
                      className="flex items-center justify-center h-8 w-8 rounded-lg bg-amber-600 hover:bg-amber-700 text-white transition-colors cursor-pointer"
                    >
                      <Pause className="h-4 w-4" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>{t('simulation.pauseSim')}</TooltipContent>
                </Tooltip>
                <Tooltip>
                  <TooltipTrigger>
                    <button
                      onClick={handleStopClick}
                      disabled={controlLoading}
                      className="flex items-center justify-center h-8 px-3 rounded-lg bg-destructive hover:bg-destructive/90 disabled:bg-destructive/50 text-xs font-semibold text-destructive-foreground transition-colors cursor-pointer"
                    >
                      <Square className="h-3.5 w-3.5 mr-1" /> {t('simulation.stopSim')}
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>{t('simulation.terminateSim')}</TooltipContent>
                </Tooltip>
              </>
            )}

            {state.status === 'paused' && (
              <>
                <Tooltip>
                  <TooltipTrigger>
                    <button
                      onClick={handleResume}
                      disabled={controlLoading}
                      className="flex items-center justify-center h-8 w-8 rounded-lg bg-success hover:bg-success/90 text-success-foreground transition-colors cursor-pointer"
                    >
                      <Play className="h-4 w-4" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>{t('simulation.resumeSim')}</TooltipContent>
                </Tooltip>
                <Tooltip>
                  <TooltipTrigger>
                    <button
                      onClick={handleStep}
                      disabled={controlLoading}
                      className="flex items-center justify-center h-8 w-8 rounded-lg bg-primary hover:bg-primary/95 text-primary-foreground transition-colors cursor-pointer"
                    >
                      <SkipForward className="h-4 w-4" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>{t('simulation.stepSim')}</TooltipContent>
                </Tooltip>
                <Tooltip>
                  <TooltipTrigger>
                    <button
                      onClick={handleStopClick}
                      disabled={controlLoading}
                      className="flex items-center justify-center h-8 px-3 rounded-lg bg-destructive hover:bg-destructive/90 text-xs font-semibold text-destructive-foreground transition-colors cursor-pointer"
                    >
                      <Square className="h-3.5 w-3.5 mr-1" /> {t('simulation.stopSim')}
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>{t('simulation.terminateSim')}</TooltipContent>
                </Tooltip>
              </>
            )}

            {(state.status === 'completed' ||
              state.status === 'failed' ||
              state.status === 'cancelled') && (
              <Tooltip>
                <TooltipTrigger>
                  <button
                    onClick={() => {
                      setForkInitialTopic(state.config.topic + ' (Forked)')
                      setForkInitialMaxWallClockMin(
                        state.config.max_wall_clock_ms
                          ? Math.round(state.config.max_wall_clock_ms / 60000)
                          : 18
                      )
                      setForkDialogOpen(true)
                    }}
                    disabled={controlLoading}
                    className="flex items-center justify-center h-8 px-3 rounded-lg bg-indigo-600 hover:bg-indigo-700 text-xs font-semibold text-white transition-colors cursor-pointer"
                  >
                    <GitFork className="h-3.5 w-3.5 mr-1" /> {t('simulation.forkSim')}
                  </button>
                </TooltipTrigger>
                <TooltipContent>{t('simulation.forkDesc')}</TooltipContent>
              </Tooltip>
            )}

            <Tooltip>
              <TooltipTrigger>
                <button
                  onClick={() => setDeleteConfirmOpen(true)}
                  disabled={controlLoading}
                  className="flex items-center justify-center h-8 w-8 rounded-lg border border-rose-500/25 bg-rose-500/5 text-rose-500 hover:bg-rose-500/10 transition-colors cursor-pointer"
                >
                  <Trash2 className="h-4 w-4" />
                </button>
              </TooltipTrigger>
              <TooltipContent>{t('common.delete')}</TooltipContent>
            </Tooltip>
          </TooltipProvider>

          {controlLoading && <Loader2 className="h-4 w-4 animate-spin text-muted-foreground ml-1" />}
        </div>
      </header>

      {/* Main Workspace (Grid layout) — stable 2-column layout */}
      <div className="flex flex-1 overflow-hidden min-h-0 relative">
        {/* Left Side: Simulation Graph area (completely unscrollable, fits page height) */}
        <div className="flex-1 flex flex-col relative overflow-hidden bg-background">
          {/* Graph Layer Toggle */}
          <div className="absolute top-4 right-4 z-10 flex gap-2 select-none">
            <div className="flex items-center gap-1 bg-card/85 backdrop-blur-md p-1 rounded-lg border border-border/60 shadow-sm">
              <button
                onClick={() => setGraphLayer('interaction')}
                className={`text-[9px] font-mono px-2 py-1 rounded transition-colors cursor-pointer ${
                  graphLayer === 'interaction'
                    ? 'bg-primary/20 text-primary font-bold'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                {t('simulation.tabDynamicInteract')}
              </button>
              <button
                onClick={() => setGraphLayer('relationship')}
                className={`text-[9px] font-mono px-2 py-1 rounded transition-colors cursor-pointer ${
                  graphLayer === 'relationship'
                    ? 'bg-primary/20 text-primary font-bold'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                {t('simulation.tabSocialRelations')}
              </button>
              <button
                onClick={() => setGraphLayer('both')}
                className={`text-[9px] font-mono px-2 py-1 rounded transition-colors cursor-pointer ${
                  graphLayer === 'both'
                    ? 'bg-primary/20 text-primary font-bold'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                {t('simulation.tabDualDisplay')}
              </button>
            </div>
          </div>

          {/* D3 Graph itself */}
          <div className="flex-1 w-full h-full relative">
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
              onSelectAgent={handleSelectAgent}
              selectedAgentId={selectedAgentId}
              pulseNodes={pulseNodesRef.current}
              pulseVersion={pulseVersion}
              activeAgentIds={activeAgentIds}
              onOpenDetails={(agentId) => {
                setSelectedAgentId(agentId)
                setActiveTab('agent')
              }}
            />
          </div>
        </div>

        {/* Right Side: Resizable Telemetry Sidebar */}
        <div
          style={{ width: rightPanelCollapsed ? 0 : rightPanelWidth }}
          className={cn(
            "shrink-0 h-full border-l border-border bg-card/20 flex flex-col overflow-hidden relative",
            !isResizing && "transition-all duration-300 ease-in-out",
            rightPanelCollapsed && "border-l-0"
          )}
        >
          {/* Resize Handle (only visible when not collapsed) */}
          {!rightPanelCollapsed && (
            <div
              className="absolute top-0 bottom-0 left-0 w-1 cursor-col-resize hover:bg-primary/50 transition-colors z-30"
              onMouseDown={startResize}
            />
          )}

          {/* Collapse/Expand Toggle Button */}
          <button
            onClick={() => setRightPanelCollapsed(!rightPanelCollapsed)}
            className="absolute top-1/2 -left-3.5 z-40 flex h-7 w-3.5 -translate-y-1/2 items-center justify-center rounded-l-md border border-r-0 border-border bg-card hover:bg-muted text-muted-foreground hover:text-foreground transition-all shadow-sm cursor-pointer"
          >
            {rightPanelCollapsed ? (
              <ChevronLeft className="h-3 w-3" />
            ) : (
              <ChevronRight className="h-3 w-3" />
            )}
          </button>

          {/* Sidebar content */}
          {!rightPanelCollapsed && (
            <div className="flex flex-col h-full overflow-hidden">
              {/* Telemetry header & phase progress */}
              <div className="shrink-0 space-y-3 p-4 pb-3 border-b border-border/40 bg-muted/5">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-bold text-foreground font-mono tracking-wide">{t('simulation.sandboxMonitoring')}</span>
                  {state.status === 'completed' && state.report && (
                    <button
                      onClick={() => setIsReportModalOpen(true)}
                      className="inline-flex items-center gap-1 rounded bg-primary/10 text-primary border border-primary/20 px-2 py-0.5 text-[9px] font-semibold cursor-pointer hover:bg-primary/20 transition-all font-mono"
                    >
                      <FileText className="h-2.5 w-2.5" />
                      {t('simulation.fullScreenReport')}
                    </button>
                  )}
                </div>

                <div className="flex items-center gap-3">
                  <div className="flex-1 space-y-1">
                    <div className="flex items-center justify-between text-[10px] text-muted-foreground font-mono">
                      <span>{progress?.current_actions || 0} actions</span>
                      <span className="font-semibold text-foreground">
                        {(progress?.progress_percent || 0).toFixed(1)}%
                      </span>
                    </div>
                    <div className="relative h-1.5 w-full overflow-hidden rounded-full bg-muted">
                      <div
                        className="h-full rounded-full bg-primary transition-all duration-500 ease-out"
                        style={{ width: `${Math.min(progress?.progress_percent || 0, 100)}%` }}
                      />
                    </div>
                  </div>

                  {progress && (
                    <div className="flex items-center gap-2.5 text-[9px] text-muted-foreground font-mono whitespace-nowrap">
                      <div className="flex items-center gap-1">
                        <Clock className="h-3 w-3 text-muted-foreground/60" />
                        <span>{formatTime(progress.elapsed_seconds)}</span>
                      </div>
                      {progress.estimated_remaining_seconds > 0 && (
                        <div className="flex items-center gap-1">
                          <Zap className="h-3 w-3 text-primary/70" />
                          <span>ETA {formatTime(progress.estimated_remaining_seconds)}</span>
                        </div>
                      )}
                    </div>
                  )}
                </div>

                {/* Step indicators */}
                {progress && (
                  <div className="flex items-center gap-1 pt-0.5 overflow-x-auto scrollbar-none select-none">
                    {phaseSteps.map((step, idx) => (
                      <div key={step.key} className="flex items-center gap-1 flex-1">
                        <div className="flex items-center gap-1 min-w-0">
                          {idx < currentPhaseIdx ? (
                            <CheckCircle2 className="h-3 w-3 shrink-0 text-success" />
                          ) : idx === currentPhaseIdx ? (
                            <Loader2 className="h-3 w-3 shrink-0 animate-spin text-signal" />
                          ) : (
                            <div className="h-3 w-3 shrink-0 rounded-full border border-muted-foreground/30" />
                          )}
                          <span
                            className={cn(
                              "text-[9px] font-mono truncate",
                              idx === currentPhaseIdx
                                ? 'text-primary font-semibold'
                                : idx < currentPhaseIdx
                                  ? 'text-muted-foreground'
                                  : 'text-muted-foreground/30'
                            )}
                          >
                            {step.label}
                          </span>
                        </div>
                        {idx < phaseSteps.length - 1 && (
                          <div
                            className={cn(
                              "flex-1 h-px mx-0.5",
                              idx < currentPhaseIdx ? 'bg-success/50' : 'bg-muted-foreground/20'
                            )}
                          />
                        )}
                      </div>
                    ))}
                  </div>
                )}
              </div>

              {/* Sidebar Tabs control */}
              <Tabs value={activeTab} onValueChange={(v) => setActiveTab(v as any)} className="flex-1 flex flex-col min-h-0">
                <TabsList className="grid grid-cols-4 border-b border-border bg-transparent shrink-0">
                  <TabsTrigger
                    value="stream"
                    className="py-2.5 text-center text-xs font-semibold border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-card/25"
                  >
                    {t('simulation.tabStream')}
                  </TabsTrigger>
                  <TabsTrigger
                    value="world"
                    className="py-2.5 text-center text-xs font-semibold border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-card/25"
                  >
                    {t('simulation.tabWorld')}
                  </TabsTrigger>
                  <TabsTrigger
                    value="report"
                    className="py-2.5 text-center text-xs font-semibold border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-card/25"
                  >
                    {t('simulation.tabReport')}
                  </TabsTrigger>
                  <TabsTrigger
                    value="agent"
                    className="py-2.5 text-center text-xs font-semibold border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-card/25"
                  >
                    {t('simulation.tabAgent')}
                  </TabsTrigger>
                </TabsList>

                {/* Tab content 1: Stream (Telemetry messages) */}
                <TabsContent value="stream" className="flex-1 flex flex-col min-h-0 overflow-hidden outline-none">
                  {/* Agent Chips filter bar */}
                  <div className="shrink-0 px-4 py-2 bg-muted/10 border-b border-border/40 overflow-x-auto flex gap-2 scrollbar-none select-none">
                    <button
                      onClick={() => setFilterAgentId(null)}
                      className={cn(
                        "px-2.5 py-1 rounded-full text-[10px] font-mono border transition-all cursor-pointer whitespace-nowrap",
                        filterAgentId === null
                          ? "bg-primary/15 border-primary/30 text-primary font-bold"
                          : "border-border hover:bg-muted/40 text-muted-foreground"
                      )}
                    >
                      {t('simulation.all')}
                    </button>
                    {state.config.personas.map((p) => {
                      const isSelected = filterAgentId === p.id
                      const agentState = progress?.agent_states?.[p.id]
                      const isThinking = agentState?.status === 'thinking'
                      const isActive = activeAgentIds ? activeAgentIds.has(p.id) : true

                      return (
                        <button
                          key={p.id}
                          onClick={() => setFilterAgentId(isSelected ? null : p.id)}
                          className={cn(
                            "px-2.5 py-1 rounded-full text-[10px] font-mono border transition-all cursor-pointer flex items-center gap-1 whitespace-nowrap",
                            !isActive && "opacity-50 line-through bg-muted/5 border-border text-muted-foreground",
                            isActive && isSelected && "bg-primary/15 border-primary/30 text-primary font-bold",
                            isActive && !isSelected && isThinking && "border-signal/30 bg-signal/5 text-signal hover:border-signal/50 animate-pulse",
                            isActive && !isSelected && !isThinking && "border-border hover:bg-muted/40 text-muted-foreground"
                          )}
                        >
                          {isActive && isThinking ? (
                            <span className="relative flex h-1.5 w-1.5 shrink-0">
                              <span className="absolute inset-0 rounded-full bg-signal animate-ping opacity-60" />
                              <span className="absolute inset-0.5 rounded-full bg-signal" />
                            </span>
                          ) : isActive && agentState?.status === 'spoke' ? (
                            <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-success" />
                          ) : isActive ? (
                            <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-muted-foreground/30" />
                          ) : (
                            <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-rose-500/40" />
                          )}
                          <span>{p.name}</span>
                          {agentState?.message_count !== undefined && (
                            <span className="text-[9px] opacity-70 font-mono">({agentState.message_count})</span>
                          )}
                        </button>
                      )
                    })}
                  </div>

                  <div className="flex-1 overflow-y-auto p-4 space-y-1.5 min-h-0 scroll-container">
                    {renderGroupedMessages()}
                  </div>
                </TabsContent>

                {/* Tab content 2: World State */}
                <TabsContent value="world" className="flex-1 flex flex-col min-h-0 overflow-hidden outline-none">
                  {renderWorldState()}
                </TabsContent>

                {/* Tab content 3: Final Report & QA */}
                <TabsContent value="report" className="flex-1 flex flex-col min-h-0 overflow-hidden outline-none">
                  {!state.report ? (
                    <div className="flex flex-col items-center justify-center p-6 text-muted-foreground gap-2 h-full text-center">
                      <FileText className="h-8 w-8 opacity-20 animate-pulse" />
                      <span className="text-xs">{t('simulation.reportNotGenerated')}</span>
                      <p className="text-[10px] text-muted-foreground/60 max-w-[240px]">{t('simulation.reportGenDesc')}</p>
                    </div>
                  ) : (
                    <Tabs defaultValue="doc" className="flex-1 flex flex-col min-h-0">
                      <TabsList className="flex border-b border-border bg-transparent shrink-0">
                        <TabsTrigger
                          value="doc"
                          className="flex-1 py-2 text-center text-xs font-semibold border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-card/20"
                        >
                          {t('simulation.reportContent')}
                        </TabsTrigger>
                        <TabsTrigger
                          value="qa"
                          className="flex-1 py-2 text-center text-xs font-semibold border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-card/20"
                        >
                          {t('simulation.reportQA')}
                        </TabsTrigger>
                      </TabsList>
                      
                      <TabsContent value="doc" className="flex-1 overflow-y-auto p-5 min-h-0 outline-none">
                        <div className="prose prose-sm dark:prose-invert max-w-none text-xs text-foreground/90 leading-relaxed select-text font-sans">
                          <ReactMarkdown>{state.report}</ReactMarkdown>
                        </div>
                      </TabsContent>
                      
                      <TabsContent value="qa" className="flex-1 flex flex-col min-h-0 outline-none">
                        {/* Report QA Chat Feed */}
                        <div className="flex-1 overflow-y-auto p-4 space-y-3 min-h-0 scroll-container">
                          <div className="rounded-lg bg-primary/5 border border-primary/20 p-3 text-[10px] text-muted-foreground leading-relaxed">
                            {t('simulation.expertQADesc')}
                          </div>
                          {(chatHistory['report'] || []).map((chat, idx) => (
                            <div key={idx} className="space-y-2">
                              <div className="flex justify-end">
                                <div className="rounded-xl bg-primary px-3 py-1.5 text-xs text-primary-foreground max-w-[85%] font-medium">
                                  {chat.q}
                                </div>
                              </div>
                              <div className="flex justify-start">
                                <div className="rounded-xl bg-muted/80 border border-border px-3 py-1.5 text-xs text-foreground max-w-[85%] select-text">
                                  {chat.loading ? (
                                    <div className="flex items-center gap-1.5 text-muted-foreground">
                                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                                      {t('simulation.analyzing')}
                                    </div>
                                  ) : (
                                    <div className="prose prose-sm dark:prose-invert max-w-none text-xs leading-relaxed">
                                      <ReactMarkdown>{chat.a}</ReactMarkdown>
                                    </div>
                                  )}
                                </div>
                              </div>
                            </div>
                          ))}
                        </div>
                        
                        {/* Chat Input */}
                        <form
                          onSubmit={(e) => {
                            e.preventDefault()
                            handleReportAsk(reportQuestion)
                            setReportQuestion('')
                          }}
                          className="shrink-0 border-t border-border/50 p-3 bg-card/30 flex gap-2"
                        >
                          <input
                            type="text"
                            required
                            placeholder={t('simulation.askExpertPlaceholder')}
                            value={reportQuestion}
                            onChange={(e) => setReportQuestion(e.target.value)}
                            className="flex-1 rounded-lg border border-border bg-background px-3 py-1.5 text-xs text-foreground placeholder:text-muted-foreground/50 focus:border-primary focus:outline-none transition-all"
                          />
                          <button
                            type="submit"
                            disabled={reportInterviewing || !reportQuestion.trim()}
                            className="rounded-lg bg-primary hover:bg-primary/90 disabled:bg-primary/40 p-2 text-primary-foreground transition-colors cursor-pointer shrink-0 disabled:cursor-not-allowed"
                          >
                            <Send className="h-3.5 w-3.5" />
                          </button>
                        </form>
                      </TabsContent>
                    </Tabs>
                  )}
                </TabsContent>

                {/* Tab content 4: Agent profile detail & list */}
                <TabsContent value="agent" className="flex-1 flex flex-col min-h-0 overflow-hidden outline-none">
                  {selectedAgentId ? (
                    <div className="flex-1 flex flex-col min-h-0">
                      <div className="shrink-0 p-3 bg-muted/10 border-b border-border/40 flex justify-between items-center select-none">
                        <span className="text-[10px] font-bold text-muted-foreground font-mono">{t('simulation.agentProfileDetails')}</span>
                        <button
                          onClick={() => handleSelectAgent(null)}
                          className="text-[10px] text-primary hover:underline font-mono flex items-center gap-1 cursor-pointer"
                        >
                          <ArrowLeft className="h-3 w-3" /> {t('simulation.backToAgentList')}
                        </button>
                      </div>
                      <div className="flex-1 overflow-y-auto min-h-0">
                        {(() => {
                          const persona = state.config.personas.find((p) => p.id === selectedAgentId)
                          return persona ? (
                            <AgentDetailPanel
                              persona={persona}
                              messages={state.messages}
                              progress={progress}
                              relationships={relationships}
                              onClose={() => handleSelectAgent(null)}
                              onInterview={(question) => handleAgentInterview(selectedAgentId, question)}
                              status={state.status}
                            />
                          ) : (
                            <div className="p-4 text-xs text-muted-foreground">{t('simulation.agentNotFound')}</div>
                          )
                        })()}
                      </div>
                    </div>
                  ) : (
                    <div className="flex-1 overflow-y-auto p-4 space-y-3 min-h-0">
                      <div className="flex flex-col gap-1 mb-2 select-none">
                        <h3 className="text-xs font-bold text-muted-foreground uppercase tracking-wider font-mono">{t('simulation.allParticipants')}</h3>
                        <p className="text-[10px] text-muted-foreground">{t('simulation.participantDesc')}</p>
                      </div>
                      <div className="grid grid-cols-1 gap-2.5">
                        {state.config.personas.map((p) => {
                          const agentState = progress?.agent_states?.[p.id]
                          const isActive = activeAgentIds ? activeAgentIds.has(p.id) : true

                          return (
                            <div
                              key={p.id}
                              onClick={() => handleSelectAgent(p.id)}
                              className={cn(
                                "group rounded-xl border p-3 bg-card/35 hover:bg-card/75 hover:border-primary/45 transition-all cursor-pointer relative",
                                !isActive && "opacity-60 bg-muted/5 border-dashed"
                              )}
                            >
                              <div className="flex items-center justify-between gap-2 mb-1.5">
                                <div className="flex items-center gap-2 min-w-0">
                                  <div className="h-6 w-6 rounded-full bg-primary/10 text-primary font-bold text-xs flex items-center justify-center shrink-0 border border-primary/20">
                                    {p.name.charAt(0).toUpperCase()}
                                  </div>
                                  <div className="min-w-0">
                                    <div className="font-semibold text-foreground text-xs truncate flex items-center gap-1.5">
                                      <span>{p.name}</span>
                                      {!isActive && (
                                        <span className="text-[8px] bg-rose-500/10 text-rose-500 border border-rose-500/25 px-1 py-0.2 rounded font-mono">DEATH</span>
                                      )}
                                    </div>
                                    <div className="text-[9px] text-muted-foreground font-mono leading-none mt-0.5">{p.role}</div>
                                  </div>
                                </div>
                                {agentState?.status === 'thinking' && (
                                  <span className="text-[8px] text-signal/85 font-mono animate-pulse border border-signal/20 bg-signal/5 rounded px-1 py-0.2 shrink-0">THINKING</span>
                                )}
                              </div>
                              {p.bio && (
                                <p className="text-[10px] text-muted-foreground leading-normal line-clamp-2 italic mb-2 select-text">
                                  {p.bio}
                                </p>
                              )}
                              {p.goals && p.goals.length > 0 && (
                                <div className="flex flex-wrap gap-1">
                                  {p.goals.slice(0, 2).map((g, idx) => (
                                    <span key={idx} className="px-1.5 py-0.5 rounded bg-muted/55 text-foreground/80 border border-border/40 text-[8px] font-sans truncate max-w-[120px]">
                                      🎯 {g}
                                    </span>
                                  ))}
                                </div>
                              )}
                            </div>
                          )
                        })}
                      </div>
                    </div>
                  )}
                </TabsContent>
              </Tabs>
            </div>
          )}
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
        <DialogContent className="max-w-lg max-h-[80vh] flex flex-col p-0 overflow-hidden gap-0 bg-card border border-border rounded-xl">
          <DialogHeader className="shrink-0 px-5 py-4 border-b border-border/50">
            <DialogTitle className="flex items-center gap-2 text-sm font-bold text-foreground">
              <MessageSquare className="h-4 w-4 text-primary" />
              {chatAgentId === 'report'
                ? t('simulation.chatWithExpert')
                : `${t('simulation.chatWithLabel')} ${chatAgentId
                    ? state.config.personas.find((p) => p.id === chatAgentId)?.name ?? ''
                    : ''}`}{' '}
              {t('simulation.interrogation')}
            </DialogTitle>
            <p className="text-[10px] text-muted-foreground font-normal">
              {t('simulation.interrogateDesc')}
            </p>
          </DialogHeader>

          <div className="flex-1 overflow-y-auto p-5 space-y-4 min-h-0 scroll-container">
            {chatAgentId &&
              (chatHistory[chatAgentId] || []).map((chat, idx) => (
                <div key={idx} className="space-y-3">
                  <div className="flex justify-end">
                    <div className="rounded-lg bg-primary px-3 py-2 text-xs text-primary-foreground max-w-[85%] font-medium">
                      {chat.q}
                    </div>
                  </div>
                  <div className="flex justify-start">
                    <div className="rounded-lg bg-muted/80 border border-border px-3 py-2 text-xs text-foreground max-w-[85%] select-text">
                      {chat.loading ? (
                        <div className="flex items-center gap-2 text-muted-foreground">
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                          {t('simulation.thinking')}
                        </div>
                      ) : (
                        <div className="prose prose-sm dark:prose-invert max-w-none text-xs leading-relaxed">
                          <ReactMarkdown>{chat.a}</ReactMarkdown>
                        </div>
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
              placeholder={t('simulation.askAgentPlaceholder')}
              value={chatQuestion}
              onChange={(e) => setChatQuestion(e.target.value)}
              className="flex-1 rounded-lg border border-border bg-background px-3 py-2 text-xs text-foreground placeholder:text-muted-foreground/50 focus:border-primary focus:outline-none transition-all"
            />
            <button
              type="submit"
              className="rounded-lg bg-primary hover:bg-primary/90 p-2.5 text-primary-foreground transition-colors cursor-pointer shrink-0"
            >
              <Send className="h-3.5 w-3.5" />
            </button>
          </form>
        </DialogContent>
      </Dialog>

      {/* Report Modal — expanded reading view */}
      <SimulationReportModal
        open={isReportModalOpen}
        onOpenChange={(open) => {
          setIsReportModalOpen(open)
          if (!open) setReportQuestion('')
        }}
        report={state?.report}
        topic={state?.config?.topic}
      />

      {/* View Agent Prompt Dialog */}
      <Dialog open={!!viewingPersona} onOpenChange={(v) => !v && setViewingPersona(null)}>
        <DialogContent className="max-w-2xl max-h-[85vh] flex flex-col p-6 overflow-hidden bg-card border border-border rounded-xl">
          <DialogHeader className="shrink-0">
            <DialogTitle className="flex items-baseline gap-2">
              <span className="text-sm font-bold text-foreground">{viewingPersona?.name}</span>
              <span className="text-xs text-muted-foreground font-mono font-normal">
                ({viewingPersona?.role})
              </span>
            </DialogTitle>
          </DialogHeader>

          <div className="flex-1 overflow-y-auto mt-4 pr-1 space-y-4 scroll-container">
            <div>
              <h5 className="text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-2">
                {t('simulation.systemPromptLabel')}
              </h5>
              <div className="rounded-xl border border-border bg-muted/30 p-4 font-mono text-xs whitespace-pre-wrap leading-relaxed text-foreground select-text overflow-x-auto max-h-[40vh]">
                {viewingPersona?.system_prompt || t('simulation.noSystemPrompt')}
              </div>
            </div>

            {viewingPersona?.bio && (
              <div>
                <h5 className="text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-2">
                  {t('simulation.personaBackground')}
                </h5>
                <p className="text-xs text-foreground/90 leading-relaxed bg-muted/10 p-3 rounded-lg border border-border/40">
                  {viewingPersona.bio}
                </p>
              </div>
            )}

            {viewingPersona?.goals && viewingPersona.goals.length > 0 && (
              <div>
                <h5 className="text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-2">
                  {t('simulation.agentGoals')}
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
                  {t('simulation.agentTraits')}
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
      <SimulationConfigEditor
        open={isEditing}
        onOpenChange={setIsEditing}
        editTopic={editTopic}
        onEditTopicChange={setEditTopic}
        editMaxWallClockMin={editMaxWallClockMin}
        onEditMaxWallClockMinChange={setEditMaxWallClockMin}
        editSimHours={editSimHours}
        onEditSimHoursChange={setEditSimHours}
        editTimeScale={editTimeScale}
        onEditTimeScaleChange={setEditTimeScale}
        editEnableReflection={editEnableReflection}
        onEditEnableReflectionChange={setEditEnableReflection}
        editPersonas={editPersonas}
        onEditPersonasChange={setEditPersonas}
        editLanguage={editLanguage}
        onEditLanguageChange={setEditLanguage}
        savingConfig={savingConfig}
        onSave={handleSaveConfig}
        providers={providers}
        models={models}
      />

      <ConfirmDialog
        open={stopConfirmOpen}
        onOpenChange={setStopConfirmOpen}
        title={t('simulation.confirmStopTitle')}
        message={t('simulation.confirmStopMsg')}
        destructive
        onConfirm={confirmStop}
        confirmLabel={t('simulation.stopSim')}
        loading={controlLoading}
      />

      {/* Fork Simulation Dialog */}
      <SimulationForkDialog
        open={forkDialogOpen}
        onOpenChange={setForkDialogOpen}
        simulationId={id!}
        initialTopic={forkInitialTopic}
        initialMaxWallClockMin={forkInitialMaxWallClockMin}
      />

      <ConfirmDialog
        open={deleteConfirmOpen}
        onOpenChange={setDeleteConfirmOpen}
        title={t('simulation.confirmDeleteTitle')}
        message={t('simulation.confirmDeleteMsg')}
        destructive
        onConfirm={handleDelete}
        confirmLabel={t('simulation.permanentlyDelete')}
        loading={controlLoading}
      />
    </div>
  )
}
