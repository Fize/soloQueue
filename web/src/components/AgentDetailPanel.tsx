import { useState, useEffect } from 'react'
import { useParams } from 'react-router-dom'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import {
  X,
  MessageSquare,
  Send,
  Loader2,
  Bot,
  Activity,
  Users,
  Info,
  Award,
  User,
  Clock,
} from 'lucide-react'
import type {
  SimulationPersona,
  SimulationMessage,
  SimulationProgress,
  RelationshipDTO,
  PlanItem,
  MemoryRecord,
} from '@/types'
import { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from '@/components/ui/tooltip'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

interface AgentDetailPanelProps {
  persona: SimulationPersona
  messages: SimulationMessage[]
  progress: SimulationProgress | null
  relationships?: RelationshipDTO[]
  onClose: () => void
  onInterview: (question: string) => Promise<string>
  status: 'running' | 'completed' | 'idle' | 'pending' | 'failed' | 'paused' | 'cancelled'
}

// ─── Relationship style mapping (matches SimulationGraph) ────────────────
// Keep in sync with SimulationGraph.tsx RELATION_STYLES
const RELATION_STYLES: Record<string, { color: string; label: string }> = {
  parent: { color: '#ec4899', label: '父母' },
  child: { color: '#ec4899', label: '子女' },
  sibling: { color: '#a855f7', label: '兄弟姐妹' },
  spouse: { color: '#ec4899', label: '配偶' },
  friend: { color: '#14b8a6', label: '朋友' },
  rival: { color: '#9a3412', label: '竞争对手' },
  colleague: { color: '#64748b', label: '同事' },
  mentor: { color: '#d97706', label: '导师' },
  mentee: { color: '#d97706', label: '徒弟' },
  neighbor: { color: '#94a3b8', label: '邻居' },
  stranger: { color: '#cbd5e1', label: '陌生人' },
}

const DEFAULT_STYLE = { color: '#9e9e9e', label: '熟人' }

export function AgentDetailPanel({
  persona,
  messages,
  progress: _progress,
  relationships = [],
  onClose,
  onInterview,
  status,
}: AgentDetailPanelProps) {
  const [question, setQuestion] = useState('')
  const [chatHistory, setChatHistory] = useState<{ q: string; a: string; loading?: boolean }[]>([])
  const [interviewing, setInterviewing] = useState(false)

  const { id: simId } = useParams<{ id: string }>()

  const [plan, setPlan] = useState<{ schedule: PlanItem[] } | null>(null)
  const [memories, setMemories] = useState<MemoryRecord[] | null>(null)
  const [reflections, setReflections] = useState<MemoryRecord[] | null>(null)

  const [planLoading, setPlanLoading] = useState(false)
  const [memoriesLoading, setMemoriesLoading] = useState(false)
  const [reflectionsLoading, setReflectionsLoading] = useState(false)

  const [planError, setPlanError] = useState<string | null>(null)
  const [memoriesError, setMemoriesError] = useState<string | null>(null)
  const [reflectionsError, setReflectionsError] = useState<string | null>(null)

  // Search & filters
  const [memorySearch, setMemorySearch] = useState('')
  const [memoryTypeFilter, setMemoryTypeFilter] = useState('all')

  useEffect(() => {
    if (!simId || !persona.id) return

    setPlan(null)
    setMemories(null)
    setReflections(null)
    setPlanError(null)
    setMemoriesError(null)
    setReflectionsError(null)

    const loadPlan = async () => {
      setPlanLoading(true)
      try {
        const res = await fetch(`/api/simulations/${simId}/agents/${persona.id}/plan`)
        if (res.ok) {
          const data = await res.json()
          setPlan(data.plan || null)
        } else {
          setPlanError('加载日程计划失败')
        }
      } catch (err) {
        setPlanError('网络错误')
      } finally {
        setPlanLoading(false)
      }
    }

    const loadMemories = async () => {
      setMemoriesLoading(true)
      try {
        const res = await fetch(`/api/simulations/${simId}/agents/${persona.id}/memory`)
        if (res.ok) {
          const data = await res.json()
          setMemories(data.memories || [])
        } else {
          setMemoriesError('加载记忆失败')
        }
      } catch (err) {
        setMemoriesError('网络错误')
      } finally {
        setMemoriesLoading(false)
      }
    }

    const loadReflections = async () => {
      setReflectionsLoading(true)
      try {
        const res = await fetch(`/api/simulations/${simId}/agents/${persona.id}/reflections`)
        if (res.ok) {
          const data = await res.json()
          setReflections(data.reflections || [])
        } else {
          setReflectionsError('加载高阶反思失败')
        }
      } catch (err) {
        setReflectionsError('网络错误')
      } finally {
        setReflectionsLoading(false)
      }
    }

    loadPlan()
    loadMemories()
    loadReflections()
  }, [simId, persona.id])

  const renderPlanTab = () => {
    if (planLoading) {
      return (
        <div className="flex h-32 items-center justify-center text-xs text-muted-foreground font-mono">
          <Loader2 className="mr-2 h-4 w-4 animate-spin text-primary" /> 正在加载每日计划...
        </div>
      )
    }
    if (planError) {
      return <div className="text-center text-xs font-mono text-rose-500 py-6">{planError}</div>
    }
    if (!plan || !plan.schedule || plan.schedule.length === 0) {
      return (
        <div className="flex flex-col items-center justify-center p-6 text-muted-foreground gap-2 border border-dashed border-border/80 rounded-xl bg-card/5">
          <Info className="h-6 w-6 opacity-30" />
          <span className="text-xs">未找到每日日程计划。计划将在仿真启动时生成。</span>
        </div>
      )
    }

    return (
      <div className="space-y-4">
        <h4 className="text-xs font-bold text-muted-foreground uppercase tracking-wider font-mono flex items-center gap-1.5 border-b border-border/40 pb-1.5">
          <Clock className="h-3.5 w-3.5" />
          今日日程
        </h4>
        <div className="relative border-l border-border/80 ml-2.5 pl-5 space-y-5">
          {plan.schedule.map((item: any, idx: number) => {
            const startStr = new Date(item.start_time).toLocaleTimeString([], {
              hour: '2-digit',
              minute: '2-digit',
              hour12: false,
            })
            const endStr = new Date(item.end_time).toLocaleTimeString([], {
              hour: '2-digit',
              minute: '2-digit',
              hour12: false,
            })

            let statusColor = 'bg-muted-foreground/30 border-muted-foreground/40'
            if (item.status === 'in_progress') {
              statusColor = 'bg-primary border-primary animate-pulse'
            } else if (item.status === 'completed') {
              statusColor = 'bg-success border-success'
            } else if (item.status === 'cancelled') {
              statusColor = 'bg-rose-500 border-rose-500'
            }

            const PLAN_STATUS_LABELS: Record<string, string> = {
              pending: '等待中',
              in_progress: '进行中',
              completed: '已完成',
              cancelled: '已取消',
            }

            return (
              <div key={idx} className="relative group">
                {/* Timeline Dot */}
                <span
                  className={`absolute left-[-26px] top-1.5 w-3.5 h-3.5 rounded-full border-2 bg-background transition-colors ${statusColor}`}
                />

                <div className="flex flex-col gap-1 rounded-xl bg-card/25 border border-border/60 p-3.5 hover:border-primary/40 transition-colors">
                  <div className="flex items-center justify-between text-[10px] font-mono text-muted-foreground">
                    <span className="font-semibold text-primary/95 bg-primary/5 border border-primary/10 rounded px-1.5 py-0.5">
                      {startStr} - {endStr}
                    </span>
                    <span className="capitalize">
                      {PLAN_STATUS_LABELS[item.status] || item.status}
                    </span>
                  </div>
                  <div className="text-xs font-semibold text-foreground/90 mt-1">
                    {item.activity}
                  </div>
                  <div className="text-[10px] text-muted-foreground/90 font-mono mt-0.5">
                    📍 {item.location}
                  </div>
                  {item.description && (
                    <div className="text-[10px] text-muted-foreground/80 leading-normal border-t border-border/20 pt-1.5 mt-1.5 italic">
                      {item.description}
                    </div>
                  )}
                </div>
              </div>
            )
          })}
        </div>
      </div>
    )
  }

  const renderMemoryTab = () => {
    if (memoriesLoading) {
      return (
        <div className="flex h-32 items-center justify-center text-xs text-muted-foreground font-mono">
          <Loader2 className="mr-2 h-4 w-4 animate-spin text-primary" /> 正在加载记忆...
        </div>
      )
    }
    if (memoriesError) {
      return <div className="text-center text-xs font-mono text-rose-500 py-6">{memoriesError}</div>
    }
    if (!memories || memories.length === 0) {
      return (
        <div className="flex flex-col items-center justify-center p-6 text-muted-foreground gap-2 border border-dashed border-border/80 rounded-xl bg-card/5">
          <Info className="h-6 w-6 opacity-30" />
          <span className="text-xs">暂无记忆记录。</span>
        </div>
      )
    }

    const filtered = memories
      .filter((m) => {
        const matchSearch =
          m.content.toLowerCase().includes(memorySearch.toLowerCase()) ||
          (m.location && m.location.toLowerCase().includes(memorySearch.toLowerCase()))
        const matchType = memoryTypeFilter === 'all' || m.record_type === memoryTypeFilter
        return matchSearch && matchType
      })
      .reverse() // Show latest memories first

    return (
      <div className="space-y-4">
        {/* Filters */}
        <div className="flex gap-2 shrink-0">
          <input
            type="text"
            placeholder="搜索记忆..."
            value={memorySearch}
            onChange={(e) => setMemorySearch(e.target.value)}
            className="flex-1 rounded-lg border border-border bg-background px-3 py-1.5 text-xs text-foreground placeholder:text-muted-foreground/50 focus:border-primary focus:outline-none transition-all"
          />
          <select
            value={memoryTypeFilter}
            onChange={(e) => setMemoryTypeFilter(e.target.value)}
            className="rounded-lg border border-border bg-background px-2 py-1.5 text-xs text-foreground focus:border-primary focus:outline-none transition-all font-mono"
          >
            <option value="all">所有类型</option>
            <option value="observation">观察 (Observation)</option>
            <option value="action">行动 (Action)</option>
            <option value="dialogue">对话 (Dialogue)</option>
            <option value="reflection">反思 (Reflection)</option>
            <option value="plan">计划 (Plan)</option>
          </select>
        </div>

        <div className="space-y-3">
          {filtered.length === 0 ? (
            <div className="text-center text-xs font-mono text-muted-foreground py-6">
              没有符合当前过滤器条件的记忆。
            </div>
          ) : (
            filtered.map((m, idx) => {
              const importanceColor =
                m.importance && m.importance >= 7
                  ? 'bg-amber-500/10 text-amber-500 border-amber-500/25'
                  : m.importance && m.importance >= 4
                    ? 'bg-primary/10 text-primary border-primary/25'
                    : 'bg-muted text-muted-foreground'

              const timeStr = m.simulated_time
                ? new Date(m.simulated_time).toLocaleTimeString([], {
                    hour: '2-digit',
                    minute: '2-digit',
                    hour12: false,
                  })
                : ''

              const RECORD_TYPE_LABELS: Record<string, string> = {
                observation: '观察',
                action: '行动',
                dialogue: '对话',
                reflection: '反思',
                plan: '计划',
              }

              return (
                <div
                  key={idx}
                  className="rounded-xl border border-border bg-card/20 p-4 space-y-2 text-xs hover:border-primary/30 transition-colors"
                >
                  <div className="flex flex-wrap items-center justify-between gap-1.5 border-b border-border/30 pb-1.5 text-[9px] font-mono text-muted-foreground">
                    <div className="flex items-center gap-1.5">
                      <span className="rounded bg-muted px-1.5 py-0.5 text-foreground font-semibold">
                        R{m.round}
                      </span>
                      {m.record_type && (
                        <span className="rounded bg-primary/10 border border-primary/25 text-primary font-bold px-1.5 py-0.5 uppercase tracking-wide">
                          {RECORD_TYPE_LABELS[m.record_type] || m.record_type}
                        </span>
                      )}
                      {timeStr && <span>🕒 {timeStr}</span>}
                      {m.location && <span>📍 {m.location}</span>}
                    </div>
                    {m.importance && (
                      <span
                        className={`px-1.5 py-0.5 rounded border font-semibold ${importanceColor}`}
                      >
                        重要度: {m.importance.toFixed(1)}
                      </span>
                    )}
                  </div>
                  <div className="prose prose-sm dark:prose-invert max-w-none text-foreground/90 select-text font-sans leading-relaxed">
                    <ReactMarkdown remarkPlugins={[remarkGfm]}>{m.content}</ReactMarkdown>
                  </div>
                </div>
              )
            })
          )}
        </div>
      </div>
    )
  }

  const renderReflectionsTab = () => {
    if (reflectionsLoading) {
      return (
        <div className="flex h-32 items-center justify-center text-xs text-muted-foreground font-mono">
          <Loader2 className="mr-2 h-4 w-4 animate-spin text-primary" /> 正在加载高阶反思...
        </div>
      )
    }
    if (reflectionsError) {
      return (
        <div className="text-center text-xs font-mono text-rose-500 py-6">{reflectionsError}</div>
      )
    }
    if (!reflections || reflections.length === 0) {
      return (
        <div className="flex flex-col items-center justify-center p-6 text-muted-foreground gap-2 border border-dashed border-border/80 rounded-xl bg-card/5">
          <Info className="h-6 w-6 opacity-30" />
          <span className="text-xs">暂无已生成的反思。反思会在仿真运行中周期性触发。</span>
        </div>
      )
    }

    return (
      <div className="space-y-4">
        <h4 className="text-xs font-bold text-muted-foreground uppercase tracking-wider font-mono flex items-center gap-1.5 border-b border-border/40 pb-1.5">
          <Award className="h-3.5 w-3.5" />
          智能体反思与洞察
        </h4>
        <div className="space-y-3">
          {reflections.map((r, idx) => {
            const timeStr = r.simulated_time
              ? new Date(r.simulated_time).toLocaleTimeString([], {
                  hour: '2-digit',
                  minute: '2-digit',
                  hour12: false,
                })
              : ''
            return (
              <div
                key={idx}
                className="rounded-xl border border-border/50 bg-card/30 p-4 space-y-2 text-xs"
              >
                <div className="flex items-center justify-between text-[9px] font-mono text-muted-foreground border-b border-border/20 pb-1 mt-0.5">
                  <span>
                    轮次 {r.round} {timeStr && `• 🕒 ${timeStr}`}
                  </span>
                  {r.importance && (
                    <span className="bg-amber-500/10 text-amber-500 font-bold px-1.5 py-0.2 rounded border border-amber-500/20">
                      重要度: {r.importance.toFixed(1)}
                    </span>
                  )}
                </div>
                <div className="prose prose-sm dark:prose-invert max-w-none text-foreground/90 select-text italic leading-relaxed">
                  <ReactMarkdown remarkPlugins={[remarkGfm]}>{r.content}</ReactMarkdown>
                </div>
              </div>
            )
          })}
        </div>
      </div>
    )
  }

  const agentMessages = messages.filter((m) => m.agent_id === persona.id).reverse()
  const isCompleted = status === 'completed'
  const isRunning = status === 'running'

  // Filter relationships for this agent, dedup by the other person.
  // Relationships are stored bidirectionally (A→B and B→A), so we group
  // by the other person's ID and keep only the subject-side entry (what
  // this agent thinks), falling back to target-side if no subject entry exists.
  const agentRels = Array.from(
    relationships
      .filter((r) => r.subject_id === persona.id || r.target_id === persona.id)
      .reduce((map, r) => {
        const otherId = r.subject_id === persona.id ? r.target_id : r.subject_id
        // Prefer subject-side entry (this agent's perspective)
        if (!map.has(otherId) || r.subject_id === persona.id) {
          map.set(otherId, r)
        }
        return map
      }, new Map<string, RelationshipDTO>())
      .values()
  )

  const handleSend = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!question.trim() || interviewing) return

    const q = question.trim()
    setQuestion('')
    setChatHistory((prev) => [...prev, { q, a: '', loading: true }])
    setInterviewing(true)

    try {
      const answer = await onInterview(q)
      setChatHistory((prev) => {
        const copy = [...prev]
        const idx = copy.findIndex((h) => h.q === q && h.loading)
        if (idx >= 0) copy[idx] = { q, a: answer || '无回复。' }
        return copy
      })
    } catch (err: any) {
      setChatHistory((prev) => {
        const copy = [...prev]
        const idx = copy.findIndex((h) => h.q === q && h.loading)
        if (idx >= 0) copy[idx] = { q, a: `Error: ${err.message || 'Interview failed.'}` }
        return copy
      })
    } finally {
      setInterviewing(false)
    }
  }

  const affinityToPercent = (affinity: number) => ((affinity + 1) / 2) * 100

  return (
    <div className="flex flex-col h-full bg-card/30 overflow-hidden">
      {/* Header */}
      <div className="shrink-0 flex items-center justify-between px-4 py-3 border-b border-border bg-card/25">
        <div className="flex items-center gap-3 min-w-0">
          <div className="flex items-center justify-center w-9 h-9 rounded-full bg-primary/10 text-primary text-sm font-bold shrink-0">
            {persona.name.charAt(0)}
          </div>
          <div className="min-w-0">
            <h3 className="font-semibold text-sm text-foreground truncate">{persona.name}</h3>
            <p className="text-[10px] text-muted-foreground truncate">{persona.role}</p>
          </div>
        </div>
        <button
          onClick={onClose}
          className="rounded-lg p-1.5 text-muted-foreground hover:text-foreground hover:bg-muted transition-colors cursor-pointer"
        >
          <X className="h-4 w-4" />
        </button>
      </div>

      {/* Upper Pane - Tabs */}
      <Tabs defaultValue="profile" className="flex-1 flex flex-col min-h-0 overflow-hidden">
        <TabsList className="flex border-b border-border w-full bg-transparent shrink-0">
          <TabsTrigger
            value="profile"
            className="flex-1 py-1.5 text-center text-[9px] font-bold font-mono border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-card/20"
          >
            智能体画像
          </TabsTrigger>
          <TabsTrigger
            value="plan"
            className="flex-1 py-1.5 text-center text-[9px] font-bold font-mono border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-card/20"
          >
            今日计划
          </TabsTrigger>
          <TabsTrigger
            value="memory"
            className="flex-1 py-1.5 text-center text-[9px] font-bold font-mono border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-card/20"
          >
            记忆数据库
          </TabsTrigger>
          <TabsTrigger
            value="reflections"
            className="flex-1 py-1.5 text-center text-[9px] font-bold font-mono border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-card/20"
          >
            高阶反思
          </TabsTrigger>
          <TabsTrigger
            value="logs"
            className="flex-1 py-1.5 text-center text-[9px] font-bold font-mono border-b-2 data-active:border-primary data-active:text-primary border-transparent text-muted-foreground hover:text-foreground rounded-none data-active:bg-card/20"
          >
            原始日志
          </TabsTrigger>
        </TabsList>

        <TabsContent
          value="profile"
          className="flex-1 overflow-y-auto p-6 space-y-6 min-h-0 focus-visible:outline-none"
        >
          {/* Metadata badges: Age, Gender, MBTI, Country, Profession */}
          <div className="flex flex-wrap gap-1.5 text-[10px] font-mono">
            {persona.age && (
              <span className="px-2 py-1 rounded bg-muted text-foreground">{persona.age} 岁</span>
            )}
            {persona.gender && (
              <span className="px-2 py-1 rounded bg-muted text-foreground uppercase">
                {persona.gender === 'male'
                  ? '男'
                  : persona.gender === 'female'
                    ? '女'
                    : persona.gender}
              </span>
            )}
            {persona.mbti && (
              <span className="px-2 py-1 rounded bg-primary/10 text-primary font-bold">
                {persona.mbti}
              </span>
            )}
            {persona.country && (
              <span className="px-2 py-1 rounded bg-muted text-foreground">
                📍 {persona.country}
              </span>
            )}
            {persona.profession && (
              <span className="px-2 py-1 rounded bg-muted text-foreground">
                💼 {persona.profession}
              </span>
            )}
          </div>

          {/* Bio */}
          {persona.bio && (
            <div className="space-y-2">
              <h4 className="text-xs font-bold text-muted-foreground uppercase tracking-wider font-mono flex items-center gap-1.5 border-b border-border/40 pb-1.5">
                <User className="h-3.5 w-3.5" />
                背景故事
              </h4>
              <p className="text-xs text-foreground/90 leading-relaxed bg-muted/10 p-3 rounded-lg border border-border/40 italic">
                &ldquo;{persona.bio}&rdquo;
              </p>
            </div>
          )}

          {/* Detailed Persona Description */}
          {persona.persona && (
            <div className="space-y-2">
              <h4 className="text-xs font-bold text-muted-foreground uppercase tracking-wider font-mono flex items-center gap-1.5 border-b border-border/40 pb-1.5">
                <Bot className="h-3.5 w-3.5" />
                详细画像
              </h4>
              <div className="rounded-xl border border-border bg-muted/10 p-4 prose prose-sm dark:prose-invert max-w-none text-xs text-foreground/90 max-h-48 overflow-y-auto">
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{persona.persona}</ReactMarkdown>
              </div>
            </div>
          )}

          {/* Goals */}
          {persona.goals && persona.goals.length > 0 && (
            <div className="space-y-2">
              <h4 className="text-xs font-bold text-muted-foreground uppercase tracking-wider font-mono flex items-center gap-1.5 border-b border-border/40 pb-1.5">
                <Award className="h-3.5 w-3.5" />
                智能体目标
              </h4>
              <ul className="list-disc list-inside space-y-1 text-xs text-foreground/90 pl-1">
                {persona.goals.map((g, i) => (
                  <li key={i} className="leading-relaxed select-text">
                    {g}
                  </li>
                ))}
              </ul>
            </div>
          )}

          {/* Traits */}
          {persona.traits && Object.keys(persona.traits).length > 0 && (
            <div className="space-y-2">
              <h4 className="text-xs font-bold text-muted-foreground uppercase tracking-wider font-mono flex items-center gap-1.5 border-b border-border/40 pb-1.5">
                <Info className="h-3.5 w-3.5" />
                特质属性
              </h4>
              <div className="flex flex-wrap gap-1.5">
                {Object.entries(persona.traits)
                  .filter(([k]) => k !== 'role_type' && !k.startsWith('stance:'))
                  .map(([k, v]) => (
                    <span
                      key={k}
                      className="px-2 py-1 rounded bg-muted/50 border border-border/40 text-[10px] font-mono text-foreground/80"
                    >
                      {k}={v}
                    </span>
                  ))}
              </div>
            </div>
          )}

          {/* Relationships Row */}
          <div className="space-y-4">
            <h4 className="text-xs font-bold text-muted-foreground uppercase tracking-wider font-mono flex items-center gap-1.5 border-b border-border/40 pb-1.5">
              <Users className="h-3.5 w-3.5" />
              社会关系
            </h4>
            {agentRels.length === 0 ? (
              <div className="text-xs text-muted-foreground italic pl-1">暂无社会关系。</div>
            ) : (
              <TooltipProvider>
                <div className="flex flex-wrap gap-2">
                  {agentRels.map((rel, idx) => {
                    const isSubject = rel.subject_id === persona.id
                    const otherName = isSubject ? rel.target_name : rel.subject_name
                    const style = RELATION_STYLES[rel.kind] || DEFAULT_STYLE
                    return (
                      <Tooltip key={`${rel.subject_id}-${rel.target_id}-${idx}`}>
                        <TooltipTrigger className="flex items-center gap-1.5 px-3 py-1.5 rounded-full bg-muted/30 border border-border/60 hover:bg-muted/50 transition-colors cursor-pointer select-none">
                          <span
                            className="w-2.5 h-2.5 rounded-full"
                            style={{ backgroundColor: style.color }}
                          />
                          <span className="text-xs font-medium text-foreground">{otherName}</span>
                          <span className="text-[10px] text-muted-foreground font-mono">
                            ({style.label})
                          </span>
                        </TooltipTrigger>
                        <TooltipContent
                          side="top"
                          className="flex flex-col gap-1.5 p-3 min-w-48 bg-card border border-border shadow-md"
                        >
                          <div className="font-semibold text-foreground text-xs">{otherName}</div>
                          <div className="text-[10px] text-muted-foreground flex items-center gap-1.5">
                            <span>类型:</span>
                            <span className="font-semibold text-foreground px-1 py-0.2 rounded bg-muted border border-border/30">
                              {style.label}
                            </span>
                            <span className="text-[9px] font-mono text-muted-foreground">
                              {isSubject ? '(主动)' : '(被动)'}
                            </span>
                          </div>
                          <div className="space-y-1">
                            <div className="flex justify-between text-[9px] font-mono text-muted-foreground">
                              <span>熟悉度</span>
                              <span>{(rel.familiarity * 100).toFixed(0)}%</span>
                            </div>
                            <div className="h-1 w-full rounded-full bg-muted overflow-hidden">
                              <div
                                className="h-full rounded-full"
                                style={{
                                  width: `${Math.min(rel.familiarity * 100, 100)}%`,
                                  backgroundColor: style.color,
                                }}
                              />
                            </div>
                          </div>
                          <div className="space-y-1">
                            <div className="flex justify-between text-[9px] font-mono text-muted-foreground">
                              <span>好感度</span>
                              <span
                                className={
                                  rel.affinity > 0
                                    ? 'text-success'
                                    : rel.affinity < 0
                                      ? 'text-error'
                                      : ''
                                }
                              >
                                {rel.affinity > 0 ? '+' : ''}
                                {rel.affinity.toFixed(2)}
                              </span>
                            </div>
                            <div className="h-1 w-full rounded-full bg-gradient-to-r from-red-400 via-gray-300 to-green-400 relative">
                              <div
                                className="absolute top-[-3px] w-1.5 h-1.5 rounded-full bg-foreground border border-background shadow-sm transition-all duration-300"
                                style={{
                                  left: `${affinityToPercent(rel.affinity)}%`,
                                  transform: 'translateX(-50%)',
                                  // Wait, is affinityToPercent defined? Yes, we saw it's at line 491: const affinityToPercent = (affinity: number) => ((affinity + 1) / 2) * 100
                                }}
                              />
                            </div>
                          </div>
                          {rel.tags &&
                            rel.tags.filter((t) => t !== rel.kind && t !== '').length > 0 && (
                              <div className="flex flex-wrap gap-0.5 border-t border-border/30 pt-1.5 mt-0.5">
                                {rel.tags
                                  .filter((t) => t !== rel.kind && t !== '')
                                  .map((tag) => (
                                    <span
                                      key={tag}
                                      className="px-1 py-0.2 text-[8px] rounded bg-muted text-muted-foreground font-mono border border-border/30"
                                    >
                                      {tag}
                                    </span>
                                  ))}
                              </div>
                            )}
                        </TooltipContent>
                      </Tooltip>
                    )
                  })}
                </div>
              </TooltipProvider>
            )}
          </div>
        </TabsContent>

        <TabsContent
          value="plan"
          className="flex-1 overflow-y-auto p-6 space-y-6 min-h-0 focus-visible:outline-none"
        >
          {renderPlanTab()}
        </TabsContent>

        <TabsContent
          value="memory"
          className="flex-1 overflow-y-auto p-6 space-y-6 min-h-0 focus-visible:outline-none"
        >
          {renderMemoryTab()}
        </TabsContent>

        <TabsContent
          value="reflections"
          className="flex-1 overflow-y-auto p-6 space-y-6 min-h-0 focus-visible:outline-none"
        >
          {renderReflectionsTab()}
        </TabsContent>

        <TabsContent
          value="logs"
          className="flex-1 overflow-y-auto p-6 space-y-4 min-h-0 focus-visible:outline-none"
        >
          {/* Activity Log */}
          <div className="space-y-4">
            <h4 className="text-xs font-bold text-muted-foreground uppercase tracking-wider font-mono flex items-center gap-1.5 border-b border-border/40 pb-1.5">
              <Activity className="h-3.5 w-3.5" />
              活动日志
            </h4>
            {agentMessages.length === 0 ? (
              <div className="flex flex-col items-center justify-center p-6 text-muted-foreground gap-2 border border-dashed rounded-xl">
                <Activity className="h-6 w-6 opacity-30" />
                <span className="text-xs">该智能体在此仿真中暂无活动日志。</span>
              </div>
            ) : (
              <div className="space-y-3">
                {agentMessages.map((msg, i) => (
                  <div
                    key={`${msg.seq_num}-${i}`}
                    className="rounded-xl border border-border/60 bg-card/30 p-4 space-y-2 text-xs"
                  >
                    <div className="flex items-center justify-between gap-2 border-b border-border/30 pb-1.5">
                      <span className="rounded bg-muted px-2 py-0.5 text-[9px] font-mono text-muted-foreground">
                        R{msg.round} • {msg.type}
                      </span>
                      {msg.to && msg.to !== '*' && (
                        <span className="text-[9px] text-violet-500 font-mono">→ {msg.to}</span>
                      )}
                    </div>
                    <div className="prose prose-sm dark:prose-invert max-w-none text-foreground/90 select-text">
                      <ReactMarkdown remarkPlugins={[remarkGfm]}>{msg.content}</ReactMarkdown>
                    </div>
                    {msg.reasoning && (
                      <details className="mt-2">
                        <summary className="text-[9px] text-muted-foreground cursor-pointer hover:text-foreground font-mono transition-colors">
                          思考过程
                        </summary>
                        <div className="mt-2 pl-3 border-l-2 border-border prose prose-sm dark:prose-invert max-w-none text-muted-foreground/80 leading-relaxed italic select-text">
                          <ReactMarkdown remarkPlugins={[remarkGfm]}>{msg.reasoning}</ReactMarkdown>
                        </div>
                      </details>
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>
        </TabsContent>
      </Tabs>

      {/* Lower Pane - Interview */}
      {isCompleted || isRunning ? (
        <div className="h-[320px] border-t border-border shrink-0 flex flex-col bg-card/25 min-h-0">
          <div className="shrink-0 flex items-center justify-between px-5 py-2 border-b border-border bg-card/20">
            <div className="flex items-center gap-2">
              <MessageSquare className="h-4 w-4 text-primary" />
              <span className="text-xs font-bold font-mono tracking-wide uppercase text-foreground">
                智能体访谈 (In-Character)
              </span>
            </div>
            <span className="text-[10px] text-muted-foreground">
              可提问智能体关于辩论的立场或想法
            </span>
          </div>

          {/* Interview messages */}
          <div className="flex-1 overflow-y-auto p-5 space-y-4 min-h-0 select-text">
            {chatHistory.length === 0 ? (
              <div className="flex flex-col items-center justify-center h-full text-muted-foreground gap-2">
                <MessageSquare className="h-8 w-8 opacity-20" />
                <div className="text-xs text-center max-w-[320px] leading-relaxed">
                  与 {persona.name} 启动一场关于仿真事件的角色访谈。
                </div>
              </div>
            ) : (
              chatHistory.map((chat, idx) => (
                <div key={idx} className="space-y-3">
                  {/* Question */}
                  <div className="flex justify-end">
                    <div className="rounded-xl bg-primary px-3 py-1.5 text-xs text-primary-foreground max-w-[85%] font-medium">
                      {chat.q}
                    </div>
                  </div>
                  {/* Answer */}
                  <div className="flex justify-start">
                    <div className="rounded-xl bg-muted/70 border border-border px-3 py-2 max-w-[90%] text-xs">
                      {chat.loading ? (
                        <div className="flex items-center gap-2 text-muted-foreground">
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                          思考中...
                        </div>
                      ) : (
                        <div className="prose prose-sm dark:prose-invert max-w-none text-xs leading-relaxed text-foreground select-text">
                          <ReactMarkdown remarkPlugins={[remarkGfm]}>{chat.a}</ReactMarkdown>
                        </div>
                      )}
                    </div>
                  </div>
                </div>
              ))
            )}
          </div>

          {/* Form */}
          <form
            onSubmit={handleSend}
            className="shrink-0 border-t border-border/40 p-4 bg-card/40 flex gap-2"
          >
            <input
              type="text"
              required
              placeholder={`向 ${persona.name} 提问...`}
              value={question}
              onChange={(e) => setQuestion(e.target.value)}
              className="flex-1 rounded-lg border border-border bg-background px-3 py-2 text-xs text-foreground placeholder:text-muted-foreground/50 focus:border-primary focus:ring-1 focus:ring-primary/20 focus:outline-none transition-all"
            />
            <button
              type="submit"
              disabled={interviewing || !question.trim()}
              className="rounded-lg bg-primary hover:bg-primary/90 disabled:bg-primary/50 p-2 text-primary-foreground transition-colors cursor-pointer shrink-0 disabled:cursor-not-allowed"
            >
              <Send className="h-4 w-4" />
            </button>
          </form>
        </div>
      ) : (
        <div className="h-[120px] border-t border-border shrink-0 flex flex-col bg-card/25 items-center justify-center p-4">
          <span className="text-xs text-muted-foreground text-center">
            角色访谈功能仅在仿真运行中或已完成时可用。
          </span>
        </div>
      )}
    </div>
  )
}
