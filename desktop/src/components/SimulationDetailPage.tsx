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
  _seed_locations: '种子地点',
  _seed_topic: '种子主题',
  conflict: '核心冲突',
  era: '时代背景',
  faction: '主要势力',
  factions: '势力阵营',
  location: '主要地点',
  time: '时间阶段',
  world: '世界观/背景',
}

function getStatusLabel(status: string) {
  const map: Record<string, string> = {
    idle: '空闲',
    pending: '等待中',
    running: '运行中',
    paused: '已暂停',
    completed: '已完成',
    failed: '已失败',
    cancelled: '已取消',
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
          console.error('加载 LLM 配置失败', err)
          toast.error('加载 LLM 配置失败')
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
        throw new Error(errData.error || '更新配置失败')
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
      toast.error(err.message || '保存配置失败')
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
        throw new Error(errData.error || '暂停仿真失败')
      }
      setState((prev) => (prev ? { ...prev, status: 'paused' } : null))
      toast.success('仿真已暂停')
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
        throw new Error(errData.error || '恢复仿真失败')
      }
      setState((prev) => (prev ? { ...prev, status: 'running' } : null))
      toast.success('仿真已恢复')
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
        throw new Error(errData.error || '单步运行失败')
      }
      toast.success('仿真单步运行了一轮')
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
        throw new Error(errData.error || '删除仿真失败')
      }
      toast.success('仿真已删除')
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
        throw new Error(errData.error || '启动仿真失败')
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
        throw new Error(errData.error || '停止仿真失败')
      }
      setState((prev) => (prev ? { ...prev, status: 'completed' } : null))
      toast.success('仿真已停止')
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
        if (idx !== -1) history[idx] = { q: question, a: `Error: ${err.message || '请求失败'}` }
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
      label: '对话',
    },
    private_speak: {
      icon: Lock,
      borderColor: 'border-l-violet-500/50',
      badgeBg: 'bg-violet-500/10',
      badgeText: 'text-violet-600 dark:text-violet-400',
      label: '私语',
    },
    agent_move: {
      icon: MapPin,
      borderColor: 'border-l-amber-500/50',
      badgeBg: 'bg-amber-500/10',
      badgeText: 'text-amber-600 dark:text-amber-400',
      label: '移动',
    },
    reflection: {
      icon: Lightbulb,
      borderColor: 'border-l-emerald-500/50',
      badgeBg: 'bg-emerald-500/10',
      badgeText: 'text-emerald-600 dark:text-emerald-400',
      label: '反思',
    },
    conflict: {
      icon: AlertTriangle,
      borderColor: 'border-l-rose-500/50',
      badgeBg: 'bg-rose-500/10',
      badgeText: 'text-rose-600 dark:text-rose-400',
      label: '冲突',
    },
    rebuttal: {
      icon: AlertCircle,
      borderColor: 'border-l-rose-400/50',
      badgeBg: 'bg-rose-400/10',
      badgeText: 'text-rose-500 dark:text-rose-400',
      label: '反驳',
    },
    question: {
      icon: MessageCircle,
      borderColor: 'border-l-cyan-500/50',
      badgeBg: 'bg-cyan-500/10',
      badgeText: 'text-cyan-600 dark:text-cyan-400',
      label: '提问',
    },
    auto_pass: {
      icon: SkipForward,
      borderColor: 'border-l-gray-400/30 border-dashed',
      badgeBg: 'bg-gray-400/10',
      badgeText: 'text-gray-500 dark:text-gray-400',
      label: '例行',
    },
    agent_exit: {
      icon: LogOut,
      borderColor: 'border-l-gray-500/40',
      badgeBg: 'bg-gray-500/10',
      badgeText: 'text-gray-600 dark:text-gray-400',
      label: '退场',
    },
    agent_death_announcement: {
      icon: Skull,
      borderColor: 'border-l-red-600/50',
      badgeBg: 'bg-red-600/10',
      badgeText: 'text-red-600 dark:text-red-400',
      label: '死亡',
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
      return <span className="text-muted-foreground/60">无</span>

    let parsedVal = val
    if (typeof val === 'string') {
      const trimmed = val.trim()
      if (
        (trimmed.startsWith('[') && trimmed.endsWith(']')) ||
        (trimmed.startsWith('{') && trimmed.endsWith('}'))
      ) {
        try {
          parsedVal = JSON.parse(trimmed)
        } catch (e) {
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
                const name = loc.name || loc.Name || `地点 ${idx + 1}`
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
          <span>未发现环境状态变量。</span>
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
          placeholder="过滤变量..."
          value={worldSearch}
          onChange={(e) => setWorldSearch(e.target.value)}
          className="w-full shrink-0 rounded-lg border border-border bg-background px-3 py-1.5 text-xs text-foreground placeholder:text-muted-foreground/50 focus:border-primary focus:outline-none transition-all"
        />
        <div className="flex-1 overflow-y-auto min-h-0 border border-border/50 rounded-lg bg-card/10">
          {filteredKeys.length === 0 ? (
            <div className="text-center text-xs font-mono text-muted-foreground py-6">
              没有匹配当前搜索的变量。
            </div>
          ) : (
            <table className="w-full text-xs font-sans border-collapse select-text">
              <thead>
                <tr className="border-b border-border/80 bg-muted/40 text-left text-muted-foreground">
                  <th className="p-3 py-2 font-semibold">变量名</th>
                  <th className="p-3 py-2 font-semibold">变量值</th>
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
    { key: 'initializing', label: '环境准备' },
    { key: 'generating_plans', label: '生成计划' },
    { key: 'building_prompts', label: '构建提示' },
    { key: 'running', label: '运行中' },
    { key: 'generating_report', label: '生成报告' },
    { key: 'completed', label: '完成' },
  ] as const

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
            <span>例行通过动作</span>
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
            展开
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
            title={`向 ${msg.agent_name} 提问`}
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
          <ReactMarkdown remarkPlugins={[remarkGfm]}>{msg.content}</ReactMarkdown>
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
            收起例行信息
          </button>
        )}

        {/* Reasoning (collapsible details) */}
        {msg.reasoning && (
          <details className="mt-1 group">
            <summary className="text-[9px] text-muted-foreground/50 cursor-pointer select-none hover:text-foreground font-mono tracking-wide flex items-center gap-1">
              <span className="inline-block w-0 h-0 border-l-4 border-l-transparent border-t-4 border-t-current border-r-4 border-r-transparent group-open:rotate-90 transition-transform" />
              推理过程 (Reasoning)
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
          <span>等待仿真开始...</span>
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
                {r === 0 ? '初始化阶段' : `第 ${r} 轮 / Round ${r}`}
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
                    {state.current_round === 0 ? '初始化中...' : `第 ${state.current_round} 轮`}
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
                  <TooltipContent>编辑配置</TooltipContent>
                </Tooltip>
                <Tooltip>
                  <TooltipTrigger>
                    <button
                      onClick={handleStart}
                      disabled={controlLoading}
                      className="flex items-center justify-center h-8 px-3 rounded-lg bg-success hover:bg-success/90 disabled:bg-success/50 text-xs font-semibold text-success-foreground transition-colors cursor-pointer disabled:opacity-50"
                    >
                      <Play className="h-3.5 w-3.5 mr-1" /> 启动仿真
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>开始推演</TooltipContent>
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
                  <TooltipContent>暂停仿真</TooltipContent>
                </Tooltip>
                <Tooltip>
                  <TooltipTrigger>
                    <button
                      onClick={handleStopClick}
                      disabled={controlLoading}
                      className="flex items-center justify-center h-8 px-3 rounded-lg bg-destructive hover:bg-destructive/90 disabled:bg-destructive/50 text-xs font-semibold text-destructive-foreground transition-colors cursor-pointer"
                    >
                      <Square className="h-3.5 w-3.5 mr-1" /> 停止仿真
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>终止仿真</TooltipContent>
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
                  <TooltipContent>恢复仿真</TooltipContent>
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
                  <TooltipContent>单步推进</TooltipContent>
                </Tooltip>
                <Tooltip>
                  <TooltipTrigger>
                    <button
                      onClick={handleStopClick}
                      disabled={controlLoading}
                      className="flex items-center justify-center h-8 px-3 rounded-lg bg-destructive hover:bg-destructive/90 text-xs font-semibold text-destructive-foreground transition-colors cursor-pointer"
                    >
                      <Square className="h-3.5 w-3.5 mr-1" /> 停止仿真
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>终止仿真</TooltipContent>
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
                      setForkTopic(state.config.topic + ' (Forked)')
                      setForkMaxWallClockMin(
                        state.config.max_wall_clock_ms
                          ? Math.round(state.config.max_wall_clock_ms / 60000)
                          : 18
                      )
                      setForkDialogOpen(true)
                    }}
                    disabled={controlLoading}
                    className="flex items-center justify-center h-8 px-3 rounded-lg bg-indigo-600 hover:bg-indigo-700 text-xs font-semibold text-white transition-colors cursor-pointer"
                  >
                    <GitFork className="h-3.5 w-3.5 mr-1" /> 分叉仿真
                  </button>
                </TooltipTrigger>
                <TooltipContent>克隆仿真配置开始对照情景推演</TooltipContent>
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
              <TooltipContent>删除该推演及所有记录</TooltipContent>
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
                动态交互
              </button>
              <button
                onClick={() => setGraphLayer('relationship')}
                className={`text-[9px] font-mono px-2 py-1 rounded transition-colors cursor-pointer ${
                  graphLayer === 'relationship'
                    ? 'bg-primary/20 text-primary font-bold'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                社会关系
              </button>
              <button
                onClick={() => setGraphLayer('both')}
                className={`text-[9px] font-mono px-2 py-1 rounded transition-colors cursor-pointer ${
                  graphLayer === 'both'
                    ? 'bg-primary/20 text-primary font-bold'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                双层显示
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
                  <span className="text-xs font-bold text-foreground font-mono tracking-wide">沙盒推演监测</span>
                  {state.status === 'completed' && state.report && (
                    <button
                      onClick={() => setIsReportModalOpen(true)}
                      className="inline-flex items-center gap-1 rounded bg-primary/10 text-primary border border-primary/20 px-2 py-0.5 text-[9px] font-semibold cursor-pointer hover:bg-primary/20 transition-all font-mono"
                    >
                      <FileText className="h-2.5 w-2.5" />
                      全屏阅读报告
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
                            <Loader2 className="h-3 w-3 shrink-0 animate-spin text-primary" />
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
                    消息流
                  </TabsTrigger>
                  <TabsTrigger
                    value="world"
                    className="py-2.5 text-center text-xs font-semibold border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-card/25"
                  >
                    状态
                  </TabsTrigger>
                  <TabsTrigger
                    value="report"
                    className="py-2.5 text-center text-xs font-semibold border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-card/25"
                  >
                    报告
                  </TabsTrigger>
                  <TabsTrigger
                    value="agent"
                    className="py-2.5 text-center text-xs font-semibold border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-card/25"
                  >
                    角色
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
                      全部
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
                            isActive && !isSelected && isThinking && "border-primary/30 bg-primary/5 text-primary hover:border-primary/50 animate-pulse",
                            isActive && !isSelected && !isThinking && "border-border hover:bg-muted/40 text-muted-foreground"
                          )}
                        >
                          {isActive && isThinking ? (
                            <span className="relative flex h-1.5 w-1.5 shrink-0">
                              <span className="absolute inset-0 rounded-full bg-primary animate-ping opacity-60" />
                              <span className="absolute inset-0.5 rounded-full bg-primary" />
                            </span>
                          ) : isActive && agentState?.status === 'spoke' ? (
                            <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500" />
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
                      <span className="text-xs">分析报告尚未生成</span>
                      <p className="text-[10px] text-muted-foreground/60 max-w-[240px]">报告将在仿真运行结束后通过大模型自动汇总并提炼完成。</p>
                    </div>
                  ) : (
                    <Tabs defaultValue="doc" className="flex-1 flex flex-col min-h-0">
                      <TabsList className="flex border-b border-border bg-transparent shrink-0">
                        <TabsTrigger
                          value="doc"
                          className="flex-1 py-2 text-center text-xs font-semibold border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-card/20"
                        >
                          报告正文
                        </TabsTrigger>
                        <TabsTrigger
                          value="qa"
                          className="flex-1 py-2 text-center text-xs font-semibold border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-card/20"
                        >
                          报告问答
                        </TabsTrigger>
                      </TabsList>
                      
                      <TabsContent value="doc" className="flex-1 overflow-y-auto p-5 min-h-0 outline-none">
                        <div className="prose prose-sm dark:prose-invert max-w-none text-xs text-foreground/90 leading-relaxed select-text font-sans">
                          <ReactMarkdown remarkPlugins={[remarkGfm]}>{state.report}</ReactMarkdown>
                        </div>
                      </TabsContent>
                      
                      <TabsContent value="qa" className="flex-1 flex flex-col min-h-0 outline-none">
                        {/* Report QA Chat Feed */}
                        <div className="flex-1 overflow-y-auto p-4 space-y-3 min-h-0 scroll-container">
                          <div className="rounded-lg bg-primary/5 border border-primary/20 p-3 text-[10px] text-muted-foreground leading-relaxed">
                            您可以向报告分析专家提问，了解推演中的关键细节、角色反思及冲突争议点。
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
                                      分析中...
                                    </div>
                                  ) : (
                                    <div className="prose prose-sm dark:prose-invert max-w-none text-xs leading-relaxed">
                                      <ReactMarkdown remarkPlugins={[remarkGfm]}>{chat.a}</ReactMarkdown>
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
                            placeholder="向报告专家提问..."
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
                        <span className="text-[10px] font-bold text-muted-foreground font-mono">智能体画像详情</span>
                        <button
                          onClick={() => handleSelectAgent(null)}
                          className="text-[10px] text-primary hover:underline font-mono flex items-center gap-1 cursor-pointer"
                        >
                          <ArrowLeft className="h-3 w-3" /> 返回角色列表
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
                            <div className="p-4 text-xs text-muted-foreground">智能体未找到</div>
                          )
                        })()}
                      </div>
                    </div>
                  ) : (
                    <div className="flex-1 overflow-y-auto p-4 space-y-3 min-h-0">
                      <div className="flex flex-col gap-1 mb-2 select-none">
                        <h3 className="text-xs font-bold text-muted-foreground uppercase tracking-wider font-mono">所有参会角色</h3>
                        <p className="text-[10px] text-muted-foreground">点击角色卡片查看日程计划、社交关系、记忆或发起审问</p>
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
                                  <span className="text-[8px] text-primary/85 font-mono animate-pulse border border-primary/20 bg-primary/5 rounded px-1 py-0.2 shrink-0">THINKING</span>
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
              <MessageSquare className="h-4 w-4 text-primary" />与{' '}
              {chatAgentId === 'report'
                ? '报告分析专家'
                : chatAgentId
                  ? state.config.personas.find((p) => p.id === chatAgentId)?.name
                  : ''}{' '}
              对话访谈
            </DialogTitle>
            <p className="text-[10px] text-muted-foreground font-normal">
              扮演审查角色，就仿真推演内容和立场细节对智能体发起质问。
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
                          思考中...
                        </div>
                      ) : (
                        <div className="prose prose-sm dark:prose-invert max-w-none text-xs leading-relaxed">
                          <ReactMarkdown remarkPlugins={[remarkGfm]}>{chat.a}</ReactMarkdown>
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
              placeholder="输入提问问题..."
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
      <Dialog
        open={isReportModalOpen}
        onOpenChange={(open) => {
          setIsReportModalOpen(open)
          if (!open) setReportQuestion('')
        }}
      >
        <DialogContent
          showCloseButton={false}
          className="max-w-[1000px] w-[80vw] h-[80vh] flex flex-col p-0 overflow-hidden bg-card border border-border rounded-xl"
        >
          <div className="flex flex-col h-full">
            {/* Header */}
            <div className="shrink-0 flex items-center justify-between px-6 py-4 border-b border-border/50">
              <div className="flex items-center gap-3">
                <FileText className="h-5 w-5 text-primary" />
                <h2 className="text-sm font-bold text-foreground">仿真最终分析报告 (全文阅读)</h2>
                {state?.config?.topic && (
                  <span className="text-xs text-muted-foreground font-mono truncate max-w-[300px]">
                    {state.config.topic}
                  </span>
                )}
              </div>
              <button
                onClick={() => setIsReportModalOpen(false)}
                className="rounded-lg p-1.5 text-muted-foreground hover:text-foreground hover:bg-muted transition-colors cursor-pointer"
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            {/* Report content */}
            <div className="flex-1 overflow-y-auto p-8 min-h-0 scroll-container select-text">
              <div className="max-w-3xl mx-auto">
                <div className="prose prose-sm dark:prose-invert max-w-none text-foreground/90 leading-relaxed font-sans">
                  <ReactMarkdown remarkPlugins={[remarkGfm]}>{state?.report || ''}</ReactMarkdown>
                </div>
              </div>
            </div>
          </div>
        </DialogContent>
      </Dialog>

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
                系统提示词 (System Prompt)
              </h5>
              <div className="rounded-xl border border-border bg-muted/30 p-4 font-mono text-xs whitespace-pre-wrap leading-relaxed text-foreground select-text overflow-x-auto max-h-[40vh]">
                {viewingPersona?.system_prompt || '未配置系统提示词。'}
              </div>
            </div>

            {viewingPersona?.bio && (
              <div>
                <h5 className="text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-2">
                  人物背景
                </h5>
                <p className="text-xs text-foreground/90 leading-relaxed bg-muted/10 p-3 rounded-lg border border-border/40">
                  {viewingPersona.bio}
                </p>
              </div>
            )}

            {viewingPersona?.goals && viewingPersona.goals.length > 0 && (
              <div>
                <h5 className="text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-2">
                  智能体目标
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
                  特质属性
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
        <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto bg-card border border-border rounded-xl scroll-container">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-sm font-bold text-foreground">
              <Settings className="h-4.5 w-4.5 text-primary" />
              修改沙盒仿真参数
            </DialogTitle>
          </DialogHeader>

          <div className="space-y-5 py-2">
            {/* Topic */}
            <div className="space-y-1.5">
              <Input
                label="仿真主题"
                value={editTopic}
                onChange={(e) => setEditTopic(e.target.value)}
                className="text-xs"
              />
            </div>

            {/* Wall Clock & Simulated Hours */}
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-1.5">
                <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono flex justify-between items-center">
                  <span>最大运行时间 (分钟)</span>
                  <span className="text-primary font-bold">
                    {editMaxWallClockMin}m
                    {editMaxWallClockMin >= 60
                      ? ` (${(editMaxWallClockMin / 60).toFixed(1)}h)`
                      : ''}
                  </span>
                </label>
                <div className="flex items-center gap-2">
                  <input
                    type="range"
                    min={1}
                    max={180}
                    value={Math.min(editMaxWallClockMin, 180)}
                    onChange={(e) => setEditMaxWallClockMin(parseInt(e.target.value) || 5)}
                    className="flex-1 h-1.5 bg-muted rounded-lg appearance-none cursor-pointer accent-primary"
                  />
                  <Input
                    type="number"
                    min={1}
                    max={1440}
                    value={editMaxWallClockMin}
                    onChange={(e) => {
                      const val = Math.max(1, Math.min(1440, parseInt(e.target.value) || 1))
                      setEditMaxWallClockMin(val)
                    }}
                    className="w-16 text-center text-xs h-7 py-1 px-1.5 shrink-0"
                  />
                </div>
              </div>
              <div className="space-y-1.5">
                <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono">
                  虚拟仿真时间: {editSimHours}小时
                </label>
                <input
                  type="range"
                  min={6}
                  max={168}
                  step={6}
                  value={editSimHours}
                  onChange={(e) => {
                    const val = parseInt(e.target.value) || 168
                    const currentTheoryMin = (editSimHours * 60) / editTimeScale
                    const multiplier =
                      currentTheoryMin > 0 ? editMaxWallClockMin / currentTheoryMin : 3.75
                    const newTheoryMin = (val * 60) / editTimeScale
                    const newMaxMin = Math.max(
                      1,
                      Math.min(1440, Math.round(multiplier * newTheoryMin))
                    )
                    setEditSimHours(val)
                    setEditMaxWallClockMin(newMaxMin)
                  }}
                  className="w-full h-1.5 bg-muted rounded-lg appearance-none cursor-pointer accent-primary"
                />
              </div>
            </div>

            {/* Time Scale & Reflection */}
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <Select
                  label="时间流速比例 (Time Scale)"
                  value={String(editTimeScale)}
                  onChange={(v) => {
                    const newScale = parseInt(v) || 300
                    const currentTheoryMin = (editSimHours * 60) / editTimeScale
                    const multiplier =
                      currentTheoryMin > 0 ? editMaxWallClockMin / currentTheoryMin : 3.75
                    const newTheoryMin = (editSimHours * 60) / newScale
                    const newMaxMin = Math.max(
                      1,
                      Math.min(1440, Math.round(multiplier * newTheoryMin))
                    )
                    setEditTimeScale(newScale)
                    setEditMaxWallClockMin(newMaxMin)
                  }}
                  options={[
                    { value: '60', label: '1秒 = 1分钟' },
                    { value: '300', label: '1秒 = 5分钟' },
                    { value: '600', label: '1秒 = 10分钟' },
                    { value: '1800', label: '1秒 = 30分钟' },
                    { value: '3600', label: '1秒 = 1小时' },
                  ]}
                />
              </div>
              <div className="space-y-1.5">
                <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono">
                  高阶反思 (Reflection)
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
                    {editEnableReflection ? '开启' : '关闭'}
                  </span>
                </div>
              </div>
            </div>

            {/* Language */}
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <Select
                  label="仿真语言"
                  value={editLanguage}
                  onChange={(v) => setEditLanguage(v)}
                  options={[
                    { value: 'zh', label: '中文 (Chinese)' },
                    { value: 'en', label: 'English' },
                  ]}
                />
              </div>
            </div>

            {/* Agent Specific Models */}
            <div className="space-y-3 pt-2">
              <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono border-t border-border/40 pt-3">
                特定智能体的大模型配置
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
                          label="大模型服务商 (Provider)"
                          value={persona.provider_id || ''}
                          onChange={(v) => {
                            handleUpdatePersonaOverride(idx, 'provider_id', v)
                            handleUpdatePersonaOverride(idx, 'model_id', '')
                          }}
                          placeholder="(默认快速服务商)"
                          options={[
                            { value: '', label: '(默认快速服务商)' },
                            ...providers.map((p) => ({ value: p.id, label: p.name })),
                          ]}
                        />
                      </div>
                      <div>
                        <Select
                          label="大模型 (Model)"
                          value={persona.model_id || ''}
                          onChange={(v) => handleUpdatePersonaOverride(idx, 'model_id', v)}
                          placeholder="(默认快速模型)"
                          options={[
                            { value: '', label: '(默认快速模型)' },
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
              取消
            </button>
            <button
              type="button"
              onClick={handleSaveConfig}
              disabled={savingConfig}
              className="flex items-center justify-center gap-1.5 rounded-lg bg-primary hover:bg-primary/95 disabled:bg-primary/50 px-4 py-2 text-xs font-semibold text-primary-foreground transition-all cursor-pointer shadow-md shadow-primary/5 disabled:cursor-not-allowed"
            >
              {savingConfig ? (
                <>
                  <Loader2 className="h-3.5 w-3.5 animate-spin" /> 保存中...
                </>
              ) : (
                <>
                  <Save className="h-3.5 w-3.5" /> 保存配置
                </>
              )}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={stopConfirmOpen}
        onOpenChange={setStopConfirmOpen}
        title="停止仿真"
        message="您确定要停止此仿真吗？当前状态将被保存，但任何进行中的智能体动作都将被中断。"
        destructive
        onConfirm={confirmStop}
        confirmLabel="停止仿真"
        loading={controlLoading}
      />

      {/* Fork Simulation Dialog */}
      <Dialog open={forkDialogOpen} onOpenChange={setForkDialogOpen}>
        <DialogContent className="max-w-md bg-card border border-border rounded-xl">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-sm font-bold text-foreground">
              <GitFork className="h-4.5 w-4.5 text-primary" />
              分叉仿真
            </DialogTitle>
          </DialogHeader>

          <div className="space-y-4 py-2">
            <div className="rounded-lg bg-background/40 border border-border p-3 text-[10px] text-muted-foreground leading-relaxed">
              分叉操作将克隆当前仿真的配置（包括所有智能体画像、初始社会关系与运行参数）到一个新的沙盒中，以便您可以对其发起对照测试和差异演化研究。
            </div>

            <div className="space-y-1.5">
              <Input
                label="新仿真主题"
                value={forkTopic}
                onChange={(e) => setForkTopic(e.target.value)}
                required
                className="text-xs"
              />
            </div>

            <div className="space-y-1.5">
              <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono flex justify-between items-center">
                <span>最大运行时间 (分钟)</span>
                <span className="text-primary font-bold">{forkMaxWallClockMin}分钟</span>
              </label>
              <div className="flex items-center gap-2">
                <input
                  type="range"
                  min={1}
                  max={180}
                  value={Math.min(forkMaxWallClockMin, 180)}
                  onChange={(e) => setForkMaxWallClockMin(parseInt(e.target.value) || 5)}
                  className="flex-1 h-1.5 bg-muted rounded-lg appearance-none cursor-pointer accent-primary"
                />
                <Input
                  type="number"
                  min={1}
                  max={1440}
                  value={forkMaxWallClockMin}
                  onChange={(e) => {
                    const val = Math.max(1, Math.min(1440, parseInt(e.target.value) || 1))
                    setForkMaxWallClockMin(val)
                  }}
                  className="w-16 text-center text-xs h-7 py-1 px-1.5 shrink-0"
                />
              </div>
            </div>
          </div>

          <DialogFooter showCloseButton={false}>
            <button
              type="button"
              onClick={() => setForkDialogOpen(false)}
              disabled={forking}
              className="rounded-lg bg-muted hover:bg-muted/80 px-4 py-2 text-xs font-semibold text-foreground transition-colors cursor-pointer"
            >
              取消
            </button>
            <button
              type="button"
              onClick={async () => {
                if (!id) return
                try {
                  setForking(true)
                  const res = await fetch(`/api/simulations/${id}/fork`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                      new_topic: forkTopic,
                      new_max_wall_clock_ms: forkMaxWallClockMin * 60 * 1000,
                    }),
                  })
                  if (!res.ok) {
                    const errData = await res.json()
                    throw new Error(errData.error || '分叉仿真失败')
                  }
                  const data = await res.json()
                  toast.success('仿真分叉成功！')
                  setForkDialogOpen(false)
                  navigate(`/simulations/${data.new_simulation_id}`)
                } catch (err: any) {
                  toast.error(err.message)
                } finally {
                  setForking(false)
                }
              }}
              disabled={forking || !forkTopic.trim()}
              className="flex items-center justify-center gap-1.5 rounded-lg bg-primary hover:bg-primary/95 disabled:bg-primary/50 px-4 py-2 text-xs font-semibold text-primary-foreground transition-all cursor-pointer shadow-md shadow-primary/5 disabled:cursor-not-allowed"
            >
              {forking ? (
                <>
                  <Loader2 className="h-3.5 w-3.5 animate-spin" /> 分叉中...
                </>
              ) : (
                <>
                  <GitFork className="h-3.5 w-3.5" /> 分叉仿真
                </>
              )}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={deleteConfirmOpen}
        onOpenChange={setDeleteConfirmOpen}
        title="删除仿真"
        message="您确定要永久删除此仿真及其所有智能体记忆记录吗？此操作无法撤销。"
        destructive
        onConfirm={handleDelete}
        confirmLabel="永久删除"
        loading={controlLoading}
      />
    </div>
  )
}

