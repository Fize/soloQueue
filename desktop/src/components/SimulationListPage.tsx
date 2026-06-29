import { useEffect, useState, useRef, ChangeEvent, FormEvent, MouseEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Plus,
  Sparkles,
  Trash2,
  Calendar,
  Users,
  AlertCircle,
  Loader2,
  X,
  Upload,
  ChevronDown,
  ChevronRight,
  Activity,
  Clock,
  Zap,
  ArrowRight,
} from 'lucide-react'
import { toast } from 'sonner'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { Select } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import type { SimulationState } from '@/types'

// ─── Status helpers ────────────────────────────────────────────────────────
function getStatusDot(status: string) {
  switch (status) {
    case 'running':
      return (
        <span className="relative flex h-2 w-2 shrink-0">
          <span className="absolute inset-0 rounded-full bg-emerald-500 animate-ping opacity-60" />
          <span className="absolute inset-0.5 rounded-full bg-emerald-500" />
        </span>
      )
    case 'completed':
      return <span className="h-2 w-2 shrink-0 rounded-full bg-primary" />
    case 'failed':
      return <span className="h-2 w-2 shrink-0 rounded-full bg-rose-500" />
    case 'paused':
      return <span className="h-2 w-2 shrink-0 rounded-full bg-amber-500" />
    default:
      return <span className="h-2 w-2 shrink-0 rounded-full bg-muted-foreground/40" />
  }
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

function getStatusBadgeClass(status: string) {
  switch (status) {
    case 'running':
      return 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border border-emerald-500/25'
    case 'completed':
      return 'bg-primary/10 text-primary border border-primary/25'
    case 'failed':
      return 'bg-rose-500/10 text-rose-500 dark:text-rose-400 border border-rose-500/25'
    case 'paused':
      return 'bg-amber-500/10 text-amber-600 dark:text-amber-400 border border-amber-500/25'
    default:
      return 'bg-muted-foreground/10 text-muted-foreground border border-muted-foreground/25'
  }
}

// ─── Create Sheet (Drawer) ─────────────────────────────────────────────────
interface CreateSheetProps {
  open: boolean
  onClose: () => void
  onCreated: (simId: string) => void
}

function CreateSheet({ open, onClose, onCreated }: CreateSheetProps) {
  const [topic, setTopic] = useState('')
  const [seedText, setSeedText] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  const [providers, setProviders] = useState<{ id: string; name: string }[]>([])
  const [models, setModels] = useState<{ id: string; name: string; providerId: string }[]>([])
  const [selectedModel, setSelectedModel] = useState('')
  const [selectedProvider, setSelectedProvider] = useState('')
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [simHours, setSimHours] = useState(168)
  const [enableReflection, setEnableReflection] = useState(true)
  const [timeScale, setTimeScale] = useState(300)
  const [tickIntervalMs, setTickIntervalMs] = useState(1000)
  const [language, setLanguage] = useState('zh')
  const [maxWallClockMs, setMaxWallClockMs] = useState(18 * 60 * 1000)

  const fileInputRef = useRef<HTMLInputElement | null>(null)
  const MAX_FILE_SIZE = 50 * 1024 * 1024

  useEffect(() => {
    if (!open) return
    const fetchOptions = async () => {
      try {
        const [provRes, modelRes, simRes] = await Promise.all([
          fetch('/api/config/providers'),
          fetch('/api/config/models'),
          fetch('/api/config/simulation'),
        ])
        if (provRes.ok) setProviders(await provRes.json() || [])
        if (modelRes.ok) setModels(await modelRes.json() || [])
        if (simRes.ok) {
          const d = await simRes.json()
          if (d.enableReflection !== undefined) setEnableReflection(d.enableReflection)
          if (d.simulatedHours !== undefined) setSimHours(d.simulatedHours)
          if (d.defaultModelId !== undefined) setSelectedModel(d.defaultModelId)
          if (d.defaultProviderId !== undefined) setSelectedProvider(d.defaultProviderId)
          if (d.timeScale !== undefined) setTimeScale(d.timeScale)
          if (d.tickIntervalMs !== undefined) setTickIntervalMs(d.tickIntervalMs)
          if (d.language !== undefined) setLanguage(d.language)
          if (d.defaultMaxWallClockMs !== undefined) setMaxWallClockMs(d.defaultMaxWallClockMs)
        }
      } catch (err) {
        console.error('Failed to load LLM configs', err)
      }
    }
    fetchOptions()
  }, [open])

  const handleFileUpload = (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    if (file.size > MAX_FILE_SIZE) {
      setCreateError(`文件 "${file.name}" 超过 50MB 限制`)
      if (fileInputRef.current) fileInputRef.current.value = ''
      return
    }
    setCreateError(null)
    const reader = new FileReader()
    reader.onload = (event) => {
      const text = event.target?.result as string
      if (text) {
        setSeedText(text)
        if (!topic.trim()) {
          setTopic(file.name.replace(/\.[^/.]+$/, '').replace(/[-_]/g, ' '))
        }
      }
    }
    reader.onerror = () => setCreateError(`读取文件 "${file.name}" 失败，请重试`)
    reader.readAsText(file)
  }

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (!seedText.trim()) return
    try {
      setCreating(true)
      setCreateError(null)
      const res = await fetch('/api/simulations/from-seed', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          seed_text: seedText,
          topic: topic.trim() || undefined,
          persona_count: 0,
          model_id: selectedModel || undefined,
          provider_id: selectedProvider || undefined,
          max_wall_clock_ms: maxWallClockMs > 0 ? maxWallClockMs : undefined,
          simulated_hours: simHours > 0 ? simHours : undefined,
          time_scale: timeScale,
          tick_interval_ms: tickIntervalMs,
          enable_reflection: enableReflection || undefined,
          language,
        }),
      })
      if (!res.ok) {
        const errData = await res.json()
        throw new Error(errData.error || '从种子文档生成失败')
      }
      const data = await res.json()
      onCreated(data.simulation_id)
    } catch (err: any) {
      setCreateError(err.message || '创建仿真失败')
    } finally {
      setCreating(false)
    }
  }

  const handleClose = () => {
    if (creating) return
    onClose()
  }

  return (
    <>
      {/* Backdrop */}
      {open && (
        <div
          className="fixed inset-0 z-40 bg-black/30 backdrop-blur-sm transition-opacity duration-300"
          onClick={handleClose}
        />
      )}

      {/* Sheet Panel */}
      <div
        className={`fixed right-0 top-0 bottom-0 z-50 w-[480px] flex flex-col bg-card border-l border-border shadow-2xl transition-transform duration-300 ease-out ${
          open ? 'translate-x-0' : 'translate-x-full'
        }`}
      >
        {/* Sheet Header */}
        <div className="shrink-0 flex items-center justify-between px-6 py-4 border-b border-border/60">
          <div className="flex items-center gap-2.5">
            <div className="h-8 w-8 rounded-lg bg-primary/15 flex items-center justify-center">
              <Sparkles className="h-4 w-4 text-primary" />
            </div>
            <div>
              <h2 className="text-sm font-semibold text-foreground">新建模拟推演</h2>
              <p className="text-[10px] text-muted-foreground font-mono">Auto-Generate Sandbox</p>
            </div>
          </div>
          <button
            type="button"
            onClick={handleClose}
            disabled={creating}
            className="rounded-lg p-1.5 text-muted-foreground hover:text-foreground hover:bg-muted transition-colors disabled:opacity-40"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        {/* Sheet Body */}
        <div className="flex-1 overflow-y-auto p-6">
          <p className="mb-5 text-xs text-muted-foreground leading-relaxed">
            注入种子文档，AI 将自动提取关键实体并生成持有不同立场的虚拟角色，构建自主多 Agent 沙盒。
          </p>

          <form id="create-sim-form" onSubmit={handleSubmit} className="space-y-4">
            {/* Seed Material */}
            <div>
              <div className="flex items-center justify-between mb-1.5">
                <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono">
                  种子材料 *
                </label>
                <button
                  type="button"
                  onClick={() => fileInputRef.current?.click()}
                  className="flex items-center gap-1 text-[10px] text-primary hover:underline font-mono cursor-pointer"
                >
                  <Upload className="h-3 w-3" />
                  导入文件
                </button>
                <input
                  type="file"
                  ref={fileInputRef}
                  onChange={handleFileUpload}
                  accept=".txt,.md,.json,.toml,.csv"
                  className="hidden"
                />
              </div>
              <Textarea
                required
                rows={8}
                placeholder="粘贴新闻文章、政策文件、研究报告或任何需要模拟推演的背景材料..."
                value={seedText}
                onChange={(e) => setSeedText(e.target.value)}
                className="resize-none font-sans text-xs"
              />
              <div className="flex items-center justify-between mt-1.5">
                {topic ? (
                  <p className="text-[10px] text-muted-foreground/60 font-mono">
                    主题自动检测：<span className="text-foreground/80">{topic}</span>
                  </p>
                ) : (
                  <span />
                )}
                <span className="text-[10px] text-muted-foreground/50 font-mono">
                  {seedText.length} 字符
                </span>
              </div>
            </div>

            {/* Advanced Config (collapsible) */}
            <div className="border border-border/60 rounded-xl overflow-hidden">
              <button
                type="button"
                onClick={() => setShowAdvanced(!showAdvanced)}
                className="flex w-full items-center justify-between px-4 py-3 text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono hover:text-foreground hover:bg-muted/30 transition-colors cursor-pointer select-none"
              >
                <span>高级配置</span>
                {showAdvanced ? (
                  <ChevronDown className="h-3.5 w-3.5" />
                ) : (
                  <ChevronRight className="h-3.5 w-3.5" />
                )}
              </button>

              {showAdvanced && (
                <div className="px-4 pb-4 space-y-4 border-t border-border/40 pt-4">
                  {/* Provider & Model */}
                  <div className="grid grid-cols-2 gap-3">
                    <Select
                      label="大模型服务商"
                      value={selectedProvider}
                      onChange={(v) => {
                        setSelectedProvider(v)
                        setSelectedModel('')
                      }}
                      placeholder="(默认)"
                      options={[
                        { value: '', label: '(默认快速服务商)' },
                        ...providers.map((p) => ({ value: p.id, label: p.name })),
                      ]}
                    />
                    <Select
                      label="大模型"
                      value={selectedModel}
                      onChange={setSelectedModel}
                      placeholder="(默认)"
                      options={[
                        { value: '', label: '(默认快速模型)' },
                        ...models
                          .filter((m) => !selectedProvider || m.providerId === selectedProvider)
                          .map((m) => ({ value: m.id, label: m.name })),
                      ]}
                    />
                  </div>

                  {/* Simulated Hours */}
                  <div>
                    <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-2">
                      虚拟仿真时间：<span className="text-primary">{simHours} 小时</span>
                    </label>
                    <input
                      type="range"
                      min={6}
                      max={168}
                      step={6}
                      value={simHours}
                      onChange={(e) => setSimHours(parseInt(e.target.value) || 168)}
                      className="w-full h-1.5 bg-muted rounded-lg appearance-none cursor-pointer accent-primary"
                    />
                  </div>

                  {/* Max Wall Clock */}
                  <div>
                    <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-2">
                      最大运行时间：<span className="text-primary">{Math.round(maxWallClockMs / 60000)} 分钟</span>
                    </label>
                    <input
                      type="range"
                      min={60000}
                      max={10800000}
                      step={60000}
                      value={maxWallClockMs}
                      onChange={(e) => setMaxWallClockMs(parseInt(e.target.value))}
                      className="w-full h-1.5 bg-muted rounded-lg appearance-none cursor-pointer accent-primary"
                    />
                  </div>

                  {/* Time Scale & Reflection */}
                  <div className="grid grid-cols-2 gap-3">
                    <Select
                      label="时间流速"
                      value={String(timeScale)}
                      onChange={(v) => setTimeScale(parseInt(v) || 300)}
                      options={[
                        { value: '60', label: '1秒 = 1分钟' },
                        { value: '300', label: '1秒 = 5分钟' },
                        { value: '600', label: '1秒 = 10分钟' },
                        { value: '1800', label: '1秒 = 30分钟' },
                        { value: '3600', label: '1秒 = 1小时' },
                      ]}
                    />
                    <Select
                      label="仿真语言"
                      value={language}
                      onChange={setLanguage}
                      options={[
                        { value: 'zh', label: '中文 (Chinese)' },
                        { value: 'en', label: 'English' },
                      ]}
                    />
                  </div>

                  {/* Reflection toggle */}
                  <div className="flex items-center justify-between">
                    <label className="text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono">
                      高阶反思 (Reflection)
                    </label>
                    <button
                      type="button"
                      onClick={() => setEnableReflection(!enableReflection)}
                      className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors ${
                        enableReflection ? 'bg-primary' : 'bg-muted'
                      }`}
                    >
                      <span
                        className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white transition-transform ${
                          enableReflection ? 'translate-x-[18px]' : 'translate-x-[3px]'
                        }`}
                      />
                    </button>
                  </div>
                </div>
              )}
            </div>

            {/* Error */}
            {createError && (
              <div className="flex items-start gap-2 rounded-xl bg-rose-500/10 p-3 text-xs text-rose-500 dark:text-rose-400 border border-rose-500/20">
                <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
                <span>{createError}</span>
              </div>
            )}
          </form>
        </div>

        {/* Sheet Footer */}
        <div className="shrink-0 px-6 py-4 border-t border-border/60 bg-card/60">
          <button
            type="submit"
            form="create-sim-form"
            disabled={creating || !seedText.trim()}
            className="flex w-full items-center justify-center gap-2 rounded-xl bg-primary hover:bg-primary/90 disabled:bg-primary/40 px-4 py-3 text-sm font-semibold text-primary-foreground transition-all shadow-lg shadow-primary/20 cursor-pointer disabled:cursor-not-allowed"
          >
            {creating ? (
              <>
                <Loader2 className="h-4 w-4 animate-spin" />
                AI 正在提取实体并生成角色...
              </>
            ) : (
              <>
                <Sparkles className="h-4 w-4" />
                生成并启动推演
              </>
            )}
          </button>
          <p className="text-center text-[10px] text-muted-foreground/50 mt-2 font-mono">
            通常需要 15–60 秒完成初始化
          </p>
        </div>
      </div>
    </>
  )
}

// ─── Simulation Card ───────────────────────────────────────────────────────
interface SimCardProps {
  sim: SimulationState
  onClick: () => void
  onDelete: (e: MouseEvent) => void
}

function SimCard({ sim, onClick, onDelete }: SimCardProps) {
  const personas = sim.config?.personas || []
  const isRunning = sim.status === 'running'
  const isFailed = sim.status === 'failed'

  return (
    <div
      onClick={onClick}
      className={`group relative flex flex-col gap-3 rounded-xl border bg-card/40 hover:bg-card/70 transition-all cursor-pointer px-5 py-4 ${
        isRunning
          ? 'border-emerald-500/30 hover:border-emerald-500/50 shadow-sm shadow-emerald-500/5'
          : isFailed
            ? 'border-rose-500/20 hover:border-rose-500/30'
            : 'border-border hover:border-border/80'
      }`}
    >
      {/* Top row: status + title */}
      <div className="flex items-start gap-3 min-w-0">
        <div className="flex items-center gap-2 mt-0.5 shrink-0">
          {getStatusDot(sim.status)}
          <span
            className={`rounded px-1.5 py-0.5 text-[9px] font-bold uppercase tracking-wide ${getStatusBadgeClass(sim.status)}`}
          >
            {getStatusLabel(sim.status)}
          </span>
        </div>

        <div className="min-w-0 flex-1">
          <h3 className="text-sm font-semibold text-foreground group-hover:text-primary transition-colors truncate leading-tight">
            {sim.config?.topic || '未命名推演'}
          </h3>
        </div>

        <div className="flex items-center gap-1 shrink-0 opacity-0 group-hover:opacity-100 transition-opacity">
          <button
            onClick={onDelete}
            className="rounded-lg p-1.5 text-muted-foreground hover:text-rose-500 hover:bg-rose-500/10 transition-colors"
            title="删除推演"
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
          <ArrowRight className="h-3.5 w-3.5 text-muted-foreground/50" />
        </div>
      </div>

      {/* Bottom row: meta info */}
      <div className="flex items-center gap-4 text-[10px] text-muted-foreground font-mono">
        {personas.length > 0 && (
          <span className="flex items-center gap-1">
            <Users className="h-3 w-3" />
            {personas.length} 个角色
          </span>
        )}
        {sim.started_at && (
          <span className="flex items-center gap-1">
            <Calendar className="h-3 w-3" />
            {new Date(sim.started_at).toLocaleDateString('zh-CN')}
          </span>
        )}
        {isRunning && sim.current_round > 0 && (
          <span className="flex items-center gap-1 text-emerald-600 dark:text-emerald-400">
            <Activity className="h-3 w-3" />
            第 {sim.current_round} 轮
          </span>
        )}
      </div>

      {/* Running progress bar */}
      {isRunning && (
        <div className="absolute bottom-0 left-0 right-0 h-0.5 rounded-b-xl overflow-hidden bg-emerald-500/10">
          <div className="h-full bg-emerald-500/60 animate-pulse" style={{ width: '60%' }} />
        </div>
      )}
    </div>
  )
}

// ─── Main Page ─────────────────────────────────────────────────────────────
export function SimulationListPage() {
  const navigate = useNavigate()
  const [simulations, setSimulations] = useState<SimulationState[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [sheetOpen, setSheetOpen] = useState(false)

  const fetchSimulations = async () => {
    try {
      setLoading(true)
      const res = await fetch('/api/simulations')
      if (!res.ok) throw new Error('获取推演列表失败')
      const data = await res.json()
      const mapped = (data || []).map((sim: any) => ({
        ...sim,
        id: sim.config?.id || sim.run_id || sim.id,
        personas: sim.config?.personas || [],
        round: sim.current_round || 0,
        simulated_hours: sim.config?.simulated_hours || 168,
        time_scale: sim.config?.time_scale || 300,
        enable_reflection: sim.config?.enable_reflection || false,
      }))
      setSimulations(mapped)
      setError(null)
    } catch (err: any) {
      setError(err.message || '发生错误')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchSimulations()
  }, [])

  const handleDelete = (id: string, e: MouseEvent) => {
    e.stopPropagation()
    setDeleteTarget(id)
  }

  const confirmDelete = async () => {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      const res = await fetch(`/api/simulations/${deleteTarget}`, { method: 'DELETE' })
      if (!res.ok) throw new Error('删除推演失败')
      setSimulations((prev) => prev.filter((s) => s.id !== deleteTarget))
      toast.success('推演已删除')
    } catch (err: any) {
      toast.error(err.message || '删除失败')
    } finally {
      setDeleting(false)
      setDeleteTarget(null)
    }
  }

  // Partition: running first, then rest sorted by date desc
  const runningSims = simulations.filter((s) => s.status === 'running' || s.status === 'paused')
  const otherSims = simulations
    .filter((s) => s.status !== 'running' && s.status !== 'paused')
    .sort((a, b) => {
      const aDate = a.started_at ? new Date(a.started_at).getTime() : 0
      const bDate = b.started_at ? new Date(b.started_at).getTime() : 0
      return bDate - aDate
    })

  return (
    <>
      <div className="flex h-full flex-col overflow-hidden bg-background text-foreground">
        {/* Header */}
        <div className="shrink-0 flex items-center justify-between border-b border-border/60 px-8 py-5 bg-card/20 backdrop-blur-sm">
          <div>
            <h1 className="text-xl font-bold tracking-tight text-foreground">模拟推演</h1>
            <p className="mt-0.5 text-xs text-muted-foreground">
              构建自主多智能体沙盒，预测社会动态并分析复杂议题
            </p>
          </div>
          <div className="flex items-center gap-3">
            <button
              onClick={fetchSimulations}
              disabled={loading}
              className="flex items-center gap-1.5 rounded-lg border border-border/60 px-3 py-2 text-xs text-muted-foreground hover:text-foreground hover:bg-muted/40 transition-colors disabled:opacity-50"
            >
              <Loader2 className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
              刷新
            </button>
            <button
              onClick={() => setSheetOpen(true)}
              className="flex items-center gap-2 rounded-xl bg-primary hover:bg-primary/90 px-4 py-2.5 text-sm font-semibold text-primary-foreground transition-all shadow-md shadow-primary/20 cursor-pointer"
            >
              <Plus className="h-4 w-4" />
              新建推演
            </button>
          </div>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto px-8 py-6">
          <div className="mx-auto max-w-4xl space-y-6">

            {/* Active Simulations (sticky banner group) */}
            {runningSims.length > 0 && (
              <div>
                <div className="flex items-center gap-2 mb-3">
                  <span className="relative flex h-2 w-2">
                    <span className="absolute inset-0 rounded-full bg-emerald-500 animate-ping opacity-60" />
                    <span className="absolute inset-0.5 rounded-full bg-emerald-500" />
                  </span>
                  <h2 className="text-xs font-bold text-muted-foreground uppercase tracking-wider font-mono">
                    进行中 ({runningSims.length})
                  </h2>
                </div>
                <div className="space-y-2">
                  {runningSims.map((sim) => (
                    <SimCard
                      key={sim.id}
                      sim={sim}
                      onClick={() => navigate(`/simulations/${sim.id}`)}
                      onDelete={(e) => handleDelete(sim.id, e)}
                    />
                  ))}
                </div>
              </div>
            )}

            {/* History list */}
            <div>
              <div className="flex items-center justify-between mb-3">
                <h2 className="text-xs font-bold text-muted-foreground uppercase tracking-wider font-mono">
                  历史记录 ({otherSims.length})
                </h2>
              </div>

              {loading ? (
                /* Skeleton */
                <div className="space-y-2">
                  {[1, 2, 3].map((i) => (
                    <div
                      key={i}
                      className="rounded-xl border border-border/40 bg-card/20 h-[84px] animate-pulse"
                    />
                  ))}
                </div>
              ) : error ? (
                <div className="flex flex-col items-center justify-center rounded-xl border border-border/50 bg-card/20 py-16 text-center">
                  <AlertCircle className="h-8 w-8 text-rose-500 mb-3" />
                  <p className="text-sm font-semibold text-rose-500">{error}</p>
                  <button
                    onClick={fetchSimulations}
                    className="mt-4 rounded-lg bg-muted hover:bg-muted/80 px-4 py-1.5 text-xs text-foreground transition-colors"
                  >
                    重试
                  </button>
                </div>
              ) : otherSims.length === 0 && runningSims.length === 0 ? (
                /* Empty state */
                <div className="flex flex-col items-center justify-center rounded-xl border border-dashed border-border/60 bg-card/10 py-20 text-center">
                  <div className="h-16 w-16 rounded-2xl bg-primary/10 flex items-center justify-center mb-4">
                    <Zap className="h-8 w-8 text-primary/60" />
                  </div>
                  <h3 className="text-base font-semibold text-foreground mb-1">还没有任何推演</h3>
                  <p className="text-sm text-muted-foreground max-w-xs mb-6">
                    注入种子材料，AI 将自动生成虚拟角色并构建自主多智能体沙盒
                  </p>
                  <button
                    onClick={() => setSheetOpen(true)}
                    className="flex items-center gap-2 rounded-xl bg-primary hover:bg-primary/90 px-6 py-3 text-sm font-semibold text-primary-foreground transition-all shadow-md shadow-primary/20"
                  >
                    <Sparkles className="h-4 w-4" />
                    创建第一个沙盒
                  </button>
                </div>
              ) : otherSims.length === 0 ? null : (
                <div className="space-y-2">
                  {otherSims.map((sim) => (
                    <SimCard
                      key={sim.id}
                      sim={sim}
                      onClick={() => navigate(`/simulations/${sim.id}`)}
                      onDelete={(e) => handleDelete(sim.id, e)}
                    />
                  ))}
                </div>
              )}
            </div>

            {/* Stats footer */}
            {!loading && !error && simulations.length > 0 && (
              <div className="flex items-center gap-6 pt-2 text-[10px] text-muted-foreground/60 font-mono">
                <span className="flex items-center gap-1">
                  <Clock className="h-3 w-3" />
                  共 {simulations.length} 次推演
                </span>
                <span className="flex items-center gap-1">
                  <Users className="h-3 w-3" />
                  {simulations.reduce((acc, s) => acc + (s.config?.personas?.length || 0), 0)} 个虚拟角色
                </span>
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Create Sheet */}
      <CreateSheet
        open={sheetOpen}
        onClose={() => setSheetOpen(false)}
        onCreated={(simId) => {
          setSheetOpen(false)
          navigate(`/simulations/${simId}`)
        }}
      />

      {/* Delete Confirm */}
      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => { if (!open) setDeleteTarget(null) }}
        title="删除推演"
        message="确定要永久删除此推演吗？此操作无法撤销。"
        destructive
        onConfirm={confirmDelete}
        confirmLabel="删除"
        loading={deleting}
      />
    </>
  )
}
