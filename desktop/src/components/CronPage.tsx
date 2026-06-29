import { useEffect, useRef, useState } from 'react'
import { listCronTasks, createCronTask, updateCronTask, deleteCronTask } from '@/lib/api'
import type { CronTask } from '@/types'
import { Button } from '@/components/ui/button'
import { Select } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { GlassCard } from '@/components/ui/glass-card'
import { toast } from 'sonner'
import {
  Clock,
  Plus,
  Trash2,
  Edit,
  RefreshCw,
  Copy,
  Check,
  Calendar,
  Bot,
  Terminal,
  Search,
  ChevronDown,
  ChevronUp,
} from 'lucide-react'

const agentOptions = [
  { value: 'L1', label: 'L1 Orchestrator' },
  { value: 'L2', label: 'L2 Supervisor' },
  { value: 'L3', label: 'L3 Worker' },
]

// ─── Skeleton Card ────────────────────────────────────────────────────────────

function SkeletonCard() {
  return (
    <div className="rounded-xl border border-border/40 bg-card/30 p-4 space-y-3 animate-pulse">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className="h-5 w-9 rounded-full bg-muted/60" />
          <div className="h-5 w-32 rounded-md bg-muted/60" />
        </div>
        <div className="h-5 w-16 rounded-full bg-muted/40" />
      </div>
      <div className="h-10 rounded-lg bg-muted/40" />
      <div className="flex items-center justify-between pt-1">
        <div className="h-4 w-40 rounded bg-muted/40" />
        <div className="flex gap-1">
          <div className="h-7 w-7 rounded-md bg-muted/40" />
          <div className="h-7 w-7 rounded-md bg-muted/40" />
        </div>
      </div>
    </div>
  )
}

// ─── Task Card ────────────────────────────────────────────────────────────────

interface TaskCardProps {
  task: CronTask
  isToggling: boolean
  isExpanded: boolean
  copiedId: string | null
  onToggle: (task: CronTask) => void
  onEdit: (task: CronTask) => void
  onDelete: (task: CronTask) => void
  onCopyId: (id: string) => void
  onToggleExpand: (id: string) => void
  formatTime: (t?: string) => string
}

function TaskCard({
  task,
  isToggling,
  isExpanded,
  copiedId,
  onToggle,
  onEdit,
  onDelete,
  onCopyId,
  onToggleExpand,
  formatTime,
}: TaskCardProps) {
  const isActive = task.status === 'active'
  const isCompleted = task.status === 'completed'
  const instructionLong = task.instruction.length > 100

  return (
    <GlassCard className="flex flex-col gap-0 p-0 overflow-hidden border-border/40 bg-card/40 hover:bg-card/60 transition-colors">
      {/* Card Header */}
      <div className="flex items-center justify-between gap-3 px-4 py-3 border-b border-border/30">
        {/* Left: switch + expression */}
        <div className="flex items-center gap-3 min-w-0">
          <Tooltip>
            <TooltipTrigger>
              <Switch
                id={`cron-switch-${task.id}`}
                checked={isActive}
                onCheckedChange={() => onToggle(task)}
                disabled={isCompleted || isToggling}
              />
            </TooltipTrigger>
            <TooltipContent>
              {isCompleted ? 'Completed' : isActive ? 'Click to pause' : 'Click to activate'}
            </TooltipContent>
          </Tooltip>

          <div className="flex items-center gap-1.5 font-mono text-xs font-semibold text-foreground bg-primary/5 border border-primary/10 px-2.5 py-1 rounded-md min-w-0">
            <Clock className="h-3 w-3 text-primary shrink-0" />
            <span className="truncate max-w-[180px]" title={task.expression}>
              {task.expression}
            </span>
          </div>
        </div>

        {/* Right: agent badge + status */}
        <div className="flex items-center gap-2 shrink-0">
          <Badge variant="outline" className="gap-1 bg-background/50 text-xs hidden sm:flex">
            <Bot className="h-3 w-3 text-primary" />
            {task.target_agent || 'L1'}
          </Badge>
          <span
            className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-medium border ${
              isActive
                ? 'bg-[var(--success)]/10 text-[var(--success)] border-[var(--success)]/20'
                : isCompleted
                  ? 'bg-[var(--info,#60a5fa)]/10 text-blue-400 border-blue-400/20'
                  : 'bg-muted-foreground/10 text-muted-foreground border-muted-foreground/20'
            }`}
          >
            <span
              className={`h-1.5 w-1.5 rounded-full ${
                isActive
                  ? 'bg-[var(--success)] animate-pulse'
                  : isCompleted
                    ? 'bg-blue-400'
                    : 'bg-muted-foreground'
              }`}
            />
            {task.status}
          </span>
        </div>
      </div>

      {/* Instruction Body */}
      <div className="px-4 py-3">
        <div className="flex items-start gap-2">
          <Terminal className="h-3.5 w-3.5 text-muted-foreground/50 mt-0.5 shrink-0" />
          <div className="min-w-0 flex-1">
            <p className={`text-sm text-foreground leading-relaxed ${isExpanded ? '' : 'line-clamp-2'}`}>
              {task.instruction}
            </p>
            {instructionLong && (
              <button
                onClick={() => onToggleExpand(task.id)}
                className="mt-1 flex items-center gap-0.5 text-[10px] text-muted-foreground hover:text-primary transition-colors"
              >
                {isExpanded ? (
                  <><ChevronUp className="h-3 w-3" /> Show less</>
                ) : (
                  <><ChevronDown className="h-3 w-3" /> Show more</>
                )}
              </button>
            )}
          </div>
        </div>
      </div>

      {/* Card Footer */}
      <div className="flex items-center justify-between gap-2 px-4 py-2.5 border-t border-border/30 bg-muted/10">
        {/* Time info */}
        <div className="flex items-center gap-3 text-[11px] text-muted-foreground min-w-0">
          <div className="flex items-center gap-1 min-w-0">
            <Calendar className="h-3 w-3 shrink-0" />
            <span className="truncate">Next: {formatTime(task.next_run_at)}</span>
          </div>
          {task.last_run_at && (
            <span className="hidden md:block text-muted-foreground/60 truncate">
              Last: {formatTime(task.last_run_at)}
            </span>
          )}
        </div>

        {/* Actions */}
        <div className="flex items-center gap-0.5 shrink-0">
          {/* Task ID copy */}
          <Tooltip>
            <TooltipTrigger>
              <button
                onClick={() => onCopyId(task.id)}
                className="flex items-center gap-1 px-2 py-1 rounded text-[10px] font-mono text-muted-foreground/60 hover:text-foreground hover:bg-muted transition-colors"
              >
                {copiedId === task.id ? (
                  <Check className="h-3 w-3 text-[var(--success)]" />
                ) : (
                  <Copy className="h-3 w-3" />
                )}
                <span className="hidden sm:block max-w-[64px] truncate">{task.id.slice(0, 8)}</span>
              </button>
            </TooltipTrigger>
            <TooltipContent>{copiedId === task.id ? 'Copied!' : `Copy ID: ${task.id}`}</TooltipContent>
          </Tooltip>

          <div className="w-px h-4 bg-border/60 mx-1" />

          <Tooltip>
            <TooltipTrigger>
              <button
                onClick={() => onEdit(task)}
                className="p-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
                aria-label="Edit task"
              >
                <Edit className="h-3.5 w-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent>Edit task</TooltipContent>
          </Tooltip>

          <Tooltip>
            <TooltipTrigger>
              <button
                onClick={() => onDelete(task)}
                className="p-1.5 rounded-md text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors"
                aria-label="Delete task"
              >
                <Trash2 className="h-3.5 w-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent>Delete task</TooltipContent>
          </Tooltip>
        </div>
      </div>
    </GlassCard>
  )
}

// ─── Main Page ────────────────────────────────────────────────────────────────

export function CronPage() {
  const [tasks, setTasks] = useState<CronTask[]>([])
  const [loading, setLoading] = useState(true)
  const [searchQuery, setSearchQuery] = useState('')

  // Expanded instruction rows
  const [expandedRows, setExpandedRows] = useState<Set<string>>(new Set())

  // Dialog State
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingTask, setEditingTask] = useState<CronTask | null>(null)
  const [expression, setExpression] = useState('')
  const [instruction, setInstruction] = useState('')
  const [targetAgent, setTargetAgent] = useState('L1')
  const [dialogSaving, setDialogSaving] = useState(false)
  const [dialogError, setDialogError] = useState<string | null>(null)

  // Delete confirmation state
  const [deleteTarget, setDeleteTarget] = useState<CronTask | null>(null)

  // Clipboard copy state
  const [copiedId, setCopiedId] = useState<string | null>(null)

  // Optimistic toggle tracking
  const [togglingIds, setTogglingIds] = useState<Set<string>>(new Set())

  // Dialog first-field ref for auto-focus
  const expressionInputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    fetchTasks()
  }, [])

  // Keyboard shortcuts: N = new task, ⌘R = refresh
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      const target = e.target as HTMLElement
      if (
        target.tagName === 'INPUT' ||
        target.tagName === 'TEXTAREA' ||
        target.closest('[role="dialog"]')
      ) return

      if (e.key === 'n' || e.key === 'N') {
        e.preventDefault()
        handleOpenCreateDialog()
      }
      if ((e.metaKey || e.ctrlKey) && e.key === 'r') {
        e.preventDefault()
        fetchTasks()
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [])

  async function fetchTasks() {
    setLoading(true)
    try {
      const data = await listCronTasks()
      setTasks(data)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to fetch scheduled tasks')
    } finally {
      setLoading(false)
    }
  }

  function handleOpenCreateDialog() {
    setEditingTask(null)
    setExpression('')
    setInstruction('')
    setTargetAgent('L1')
    setDialogError(null)
    setDialogOpen(true)
    requestAnimationFrame(() => expressionInputRef.current?.focus())
  }

  function handleOpenEditDialog(task: CronTask) {
    setEditingTask(task)
    setExpression(task.expression)
    setInstruction(task.instruction)
    setTargetAgent(task.target_agent || 'L1')
    setDialogError(null)
    setDialogOpen(true)
  }

  async function handleSaveTask() {
    if (!expression.trim() || !instruction.trim()) {
      setDialogError('Schedule expression and instruction are required.')
      return
    }
    setDialogSaving(true)
    setDialogError(null)
    try {
      if (editingTask) {
        await updateCronTask(editingTask.id, {
          expression: expression.trim(),
          instruction: instruction.trim(),
          target_agent: targetAgent,
        })
        toast.success('Task updated')
      } else {
        await createCronTask({
          expression: expression.trim(),
          instruction: instruction.trim(),
          target_agent: targetAgent,
        })
        toast.success('Task scheduled')
      }
      setDialogOpen(false)
      fetchTasks()
    } catch (err) {
      setDialogError(err instanceof Error ? err.message : 'Failed to save task')
    } finally {
      setDialogSaving(false)
    }
  }

  async function handleToggleStatus(task: CronTask) {
    if (togglingIds.has(task.id)) return
    const newStatus = task.status === 'active' ? 'paused' : 'active'
    const previousStatus = task.status

    setTogglingIds((prev) => new Set(prev).add(task.id))
    setTasks((prev) => prev.map((t) => (t.id === task.id ? { ...t, status: newStatus } : t)))

    try {
      await updateCronTask(task.id, { status: newStatus })
    } catch (err) {
      setTasks((prev) => prev.map((t) => (t.id === task.id ? { ...t, status: previousStatus } : t)))
      toast.error(err instanceof Error ? err.message : 'Failed to update task status')
    } finally {
      setTogglingIds((prev) => {
        const s = new Set(prev)
        s.delete(task.id)
        return s
      })
    }
  }

  function handleDeleteTask(task: CronTask) {
    setDeleteTarget(task)
  }

  async function confirmDeleteTask() {
    if (!deleteTarget) return
    try {
      await deleteCronTask(deleteTarget.id)
      setDeleteTarget(null)
      fetchTasks()
      toast.success('Task deleted')
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to delete task')
      setDeleteTarget(null)
    }
  }

  function copyToClipboard(id: string) {
    navigator.clipboard.writeText(id)
    setCopiedId(id)
    setTimeout(() => setCopiedId(null), 2000)
  }

  function toggleExpanded(id: string) {
    setExpandedRows((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  function formatTime(timeStr?: string) {
    if (!timeStr) return '—'
    try {
      const date = new Date(timeStr)
      if (isNaN(date.getTime())) return timeStr
      return date.toLocaleString(undefined, {
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
      })
    } catch {
      return timeStr
    }
  }

  const filteredTasks = tasks.filter((task) => {
    if (!searchQuery.trim()) return true
    const q = searchQuery.toLowerCase()
    return (
      task.expression.toLowerCase().includes(q) ||
      task.instruction.toLowerCase().includes(q) ||
      task.target_agent?.toLowerCase().includes(q) ||
      task.status.toLowerCase().includes(q)
    )
  })

  const activeCount = tasks.filter((t) => t.status === 'active').length

  return (
    <TooltipProvider delay={400}>
      {/* Outer wrapper — matches AgentListPage pattern: h-full overflow-y-auto */}
      <div className="h-full overflow-y-auto">
        {/* Centered content with breathing room */}
        <div className="max-w-4xl mx-auto px-4 py-6 md:px-8 md:py-8 space-y-6 pb-12">

          {/* ── Page Header ──────────────────────────────────── */}
          <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <h1 className="text-2xl font-bold tracking-tight text-foreground flex items-center gap-2">
                <Clock className="h-6 w-6 text-primary" />
                Scheduled Tasks
              </h1>
              <p className="text-sm text-muted-foreground mt-0.5">
                {tasks.length > 0
                  ? `${tasks.length} task${tasks.length !== 1 ? 's' : ''} · ${activeCount} active`
                  : 'Automate recurring reminders and agent executions'}
              </p>
            </div>

            <div className="flex items-center gap-2">
              <Tooltip>
                <TooltipTrigger>
                  <Button
                    variant="outline"
                    size="icon-sm"
                    onClick={fetchTasks}
                    disabled={loading}
                    id="cron-refresh-btn"
                  >
                    <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>Refresh (⌘R)</TooltipContent>
              </Tooltip>
              <Button
                size="sm"
                onClick={handleOpenCreateDialog}
                className="gap-1.5 shadow-sm"
                id="cron-new-task-btn"
              >
                <Plus className="h-4 w-4" />
                New Task
              </Button>
            </div>
          </div>

          {/* ── Search Bar (only when tasks exist) ───────────── */}
          {tasks.length > 0 && (
            <div className="relative">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground pointer-events-none" />
              <input
                type="search"
                placeholder="Filter by expression, instruction, agent, or status…"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="h-9 w-full rounded-lg border border-border bg-card/40 pl-10 pr-4 text-sm text-foreground placeholder:text-muted-foreground/50 outline-none focus-visible:border-primary focus-visible:ring-2 focus-visible:ring-ring/50 transition-colors backdrop-blur-sm"
              />
            </div>
          )}

          {/* ── Content Area ─────────────────────────────────── */}
          {loading && tasks.length === 0 ? (
            /* Skeleton loading cards */
            <div className="grid gap-3 sm:grid-cols-1 md:grid-cols-2">
              {Array.from({ length: 3 }).map((_, i) => (
                <SkeletonCard key={i} />
              ))}
            </div>
          ) : tasks.length === 0 ? (
            /* Empty state */
            <GlassCard
              variant="ghost"
              className="flex flex-col items-center justify-center py-16 text-center border-dashed"
            >
              <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-primary/10 text-primary mb-4">
                <Clock className="h-7 w-7" />
              </div>
              <h3 className="text-base font-semibold text-foreground">No scheduled tasks yet</h3>
              <p className="mt-2 text-sm text-muted-foreground max-w-sm leading-relaxed">
                Automate recurring reminders and commands. Or try chatting with AI:{' '}
                <span className="font-mono text-primary bg-primary/5 px-1.5 py-0.5 rounded text-xs">
                  "Remind me to check the DB daily at 9am"
                </span>
              </p>
              <Button size="sm" onClick={handleOpenCreateDialog} className="gap-1.5 mt-6">
                <Plus className="h-4 w-4" />
                Create First Task
              </Button>
            </GlassCard>
          ) : filteredTasks.length === 0 ? (
            /* No search results */
            <div className="flex flex-col items-center justify-center py-16 text-center">
              <Search className="h-8 w-8 text-muted-foreground/30 mb-3" />
              <p className="text-sm text-muted-foreground">
                No tasks match{' '}
                <span className="font-medium text-foreground">"{searchQuery}"</span>
              </p>
              <button
                onClick={() => setSearchQuery('')}
                className="text-xs text-primary hover:underline underline-offset-2 mt-2"
              >
                Clear filter
              </button>
            </div>
          ) : (
            /* Task Cards Grid */
            <div className="grid gap-3 sm:grid-cols-1 lg:grid-cols-2">
              {filteredTasks.map((task) => (
                <TaskCard
                  key={task.id}
                  task={task}
                  isToggling={togglingIds.has(task.id)}
                  isExpanded={expandedRows.has(task.id)}
                  copiedId={copiedId}
                  onToggle={handleToggleStatus}
                  onEdit={handleOpenEditDialog}
                  onDelete={handleDeleteTask}
                  onCopyId={copyToClipboard}
                  onToggleExpand={toggleExpanded}
                  formatTime={formatTime}
                />
              ))}
            </div>
          )}
        </div>
      </div>

      {/* ── Create / Edit Dialog ─────────────────────────────── */}
      <Dialog
        open={dialogOpen}
        onOpenChange={(open) => {
          setDialogOpen(open)
          if (open) requestAnimationFrame(() => expressionInputRef.current?.focus())
        }}
      >
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-base">
              <Clock className="h-4 w-4 text-primary" />
              {editingTask ? 'Edit Scheduled Task' : 'New Scheduled Task'}
            </DialogTitle>
          </DialogHeader>

          <div className="space-y-4 py-1">
            {/* Expression */}
            <div className="flex flex-col gap-1.5">
              <label htmlFor="cron-expr-input" className="text-xs font-medium text-muted-foreground">
                Schedule Expression / Datetime *
              </label>
              <input
                id="cron-expr-input"
                ref={expressionInputRef}
                type="text"
                value={expression}
                onChange={(e) => setExpression(e.target.value)}
                placeholder="e.g. 0 12 * * 1  ·  daily  ·  2026-05-24 15:30:00"
                className="flex h-8 w-full rounded-md border border-border bg-transparent px-3 py-1 text-sm font-mono text-foreground outline-none placeholder:text-muted-foreground/50 focus-visible:border-primary focus-visible:ring-2 focus-visible:ring-ring/50 transition-colors"
                autoComplete="off"
                spellCheck={false}
              />
              <p className="text-[10px] text-muted-foreground/70 leading-relaxed px-0.5">
                5-field cron (<code className="font-mono">m h dom mon dow</code>), shorthands (
                <code className="font-mono">daily</code>, <code className="font-mono">weekly</code>,{' '}
                <code className="font-mono">hourly</code>), or datetime (
                <code className="font-mono">YYYY-MM-DD HH:MM:SS</code>).
              </p>
            </div>

            {/* Instruction */}
            <Textarea
              id="cron-instr-input"
              label="Instruction Prompt *"
              value={instruction}
              onChange={(e) => setInstruction(e.target.value)}
              placeholder="Type the prompt or reminder to send to the agent…"
              rows={5}
              className="font-mono"
            />

            {/* Target Agent */}
            <Select
              label="Target Execution Agent"
              options={agentOptions}
              value={targetAgent}
              onChange={setTargetAgent}
              id="cron-agent-select"
            />
          </div>

          {/* Footer */}
          <div className="flex items-center justify-between border-t border-border pt-4 mt-1">
            {dialogError ? (
              <p className="text-xs font-medium text-destructive">{dialogError}</p>
            ) : (
              <span />
            )}
            <div className="flex items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={() => setDialogOpen(false)}
                disabled={dialogSaving}
              >
                Cancel
              </Button>
              <Button size="sm" onClick={handleSaveTask} disabled={dialogSaving} id="cron-save-btn">
                {dialogSaving ? 'Saving…' : editingTask ? 'Save Changes' : 'Schedule Task'}
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>

      {/* ── Delete Confirm ───────────────────────────────────── */}
      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => { if (!open) setDeleteTarget(null) }}
        title="Delete Scheduled Task"
        message={`Are you sure you want to delete "${deleteTarget?.expression}"? This action cannot be undone.`}
        destructive
        onConfirm={confirmDeleteTask}
        confirmLabel="Delete Task"
      />
    </TooltipProvider>
  )
}
