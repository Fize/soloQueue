import { useEffect, useState } from 'react'
import { listCronTasks, createCronTask, updateCronTask, deleteCronTask } from '@/lib/api'
import type { CronTask } from '@/types'
import { GlassCard } from '@/components/ui/glass-card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Badge } from '@/components/ui/badge'
import {
  Clock,
  Plus,
  Trash2,
  Edit,
  Play,
  Pause,
  RefreshCw,
  AlertCircle,
  Copy,
  Check,
  Calendar,
  Bot,
  Terminal,
} from 'lucide-react'

const agentOptions = [
  { value: 'L1', label: 'L1 Orchestrator' },
  { value: 'L2', label: 'L2 Supervisor' },
  { value: 'L3', label: 'L3 Worker' },
]

export function CronPage() {
  const [tasks, setTasks] = useState<CronTask[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Dialog State
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingTask, setEditingTask] = useState<CronTask | null>(null)
  const [expression, setExpression] = useState('')
  const [instruction, setInstruction] = useState('')
  const [targetAgent, setTargetAgent] = useState('L1')
  const [dialogSaving, setDialogSaving] = useState(false)
  const [dialogError, setDialogError] = useState<string | null>(null)

  // Clipboard copy state
  const [copiedId, setCopiedId] = useState<string | null>(null)

  useEffect(() => {
    fetchTasks()
  }, [])

  async function fetchTasks() {
    setLoading(true)
    setError(null)
    try {
      const data = await listCronTasks()
      setTasks(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch scheduled tasks')
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
      setDialogError('Expression and Instruction are required')
      return
    }

    setDialogSaving(true)
    setDialogError(null)

    try {
      if (editingTask) {
        // Edit task
        await updateCronTask(editingTask.id, {
          expression: expression.trim(),
          instruction: instruction.trim(),
          target_agent: targetAgent,
        })
      } else {
        // Create task
        await createCronTask({
          expression: expression.trim(),
          instruction: instruction.trim(),
          target_agent: targetAgent,
        })
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
    const newStatus = task.status === 'active' ? 'paused' : 'active'
    try {
      await updateCronTask(task.id, { status: newStatus })
      fetchTasks()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to toggle task status')
    }
  }

  async function handleDeleteTask(id: string) {
    if (!confirm('Are you sure you want to delete this scheduled task?')) {
      return
    }
    try {
      await deleteCronTask(id)
      fetchTasks()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete task')
    }
  }

  function copyToClipboard(id: string) {
    navigator.clipboard.writeText(id)
    setCopiedId(id)
    setTimeout(() => setCopiedId(null), 2000)
  }

  function formatTime(timeStr?: string) {
    if (!timeStr) return '-'
    try {
      const date = new Date(timeStr)
      if (isNaN(date.getTime())) return timeStr
      return date.toLocaleString()
    } catch {
      return timeStr
    }
  }

  return (
    <div className="flex h-full flex-col min-h-0 bg-background/50">
      {/* Top Header Section */}
      <div className="flex items-center justify-between shrink-0 px-6 py-4 border-b border-border/45 bg-card/10 backdrop-blur-md">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-foreground flex items-center gap-2">
            <Clock className="h-5 w-5 text-primary" />
            Scheduled Tasks
          </h1>
          <p className="text-xs text-muted-foreground mt-0.5">
            Manage automated, recurring and one-time cron agent reminders & executions.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={fetchTasks}
            disabled={loading}
            className="h-8"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
          </Button>
          <Button size="sm" onClick={handleOpenCreateDialog} className="h-8 gap-1.5 shadow-sm">
            <Plus className="h-4 w-4" />
            New Task
          </Button>
        </div>
      </div>

      {/* Main Content Area */}
      <div className="flex-1 overflow-y-auto p-6 min-h-0">
        {error && (
          <div className="mb-4 flex items-center gap-2 rounded-lg border border-destructive/20 bg-destructive/10 p-3.5 text-sm text-destructive">
            <AlertCircle className="h-4 w-4 shrink-0" />
            <span className="font-medium">{error}</span>
          </div>
        )}

        {loading && tasks.length === 0 ? (
          <div className="flex h-72 items-center justify-center">
            <div className="flex flex-col items-center gap-2.5">
              <RefreshCw className="h-8 w-8 text-primary animate-spin" />
              <p className="text-sm font-medium text-muted-foreground">Loading tasks...</p>
            </div>
          </div>
        ) : tasks.length === 0 ? (
          <GlassCard
            variant="ghost"
            className="flex flex-col items-center justify-center py-16 text-center border-dashed"
          >
            <div className="flex h-12 w-12 items-center justify-center rounded-full bg-primary/10 text-primary mb-4 animate-pulse">
              <Clock className="h-6 w-6" />
            </div>
            <h3 className="text-base font-semibold text-foreground">No scheduled tasks</h3>
            <p className="max-w-md text-sm text-muted-foreground mt-1 mb-6">
              Remind yourself or trigger automated commands. Try chatting with AI:{' '}
              <span className="font-mono text-primary bg-primary/5 px-1 rounded">
                "Reminder to daily check database at 9am"
              </span>
              .
            </p>
            <Button size="sm" onClick={handleOpenCreateDialog} className="gap-1.5">
              <Plus className="h-4 w-4" />
              Create First Task
            </Button>
          </GlassCard>
        ) : (
          <div className="grid gap-4 md:grid-cols-1">
            {/* Desktop Table View */}
            <div className="hidden lg:block overflow-hidden rounded-xl border border-border bg-card/30 backdrop-blur-md">
              <table className="w-full border-collapse text-left text-sm">
                <thead>
                  <tr className="border-b border-border bg-muted/40 font-medium text-muted-foreground">
                    <th className="p-4 w-[160px]">Task ID</th>
                    <th className="p-4 w-[150px]">Schedule</th>
                    <th className="p-4">Instruction Prompt</th>
                    <th className="p-4 w-[110px]">Target</th>
                    <th className="p-4 w-[100px]">Status</th>
                    <th className="p-4 w-[180px]">Next Run</th>
                    <th className="p-4 w-[120px] text-right">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border/60">
                  {tasks.map((task) => (
                    <tr key={task.id} className="hover:bg-muted/20 transition-colors group">
                      <td className="p-4 font-mono text-xs text-muted-foreground">
                        <div className="flex items-center gap-1.5">
                          <span className="truncate max-w-[100px]">{task.id}</span>
                          <button
                            onClick={() => copyToClipboard(task.id)}
                            className="text-muted-foreground/60 hover:text-foreground p-0.5 rounded transition-colors"
                            title="Copy ID"
                          >
                            {copiedId === task.id ? (
                              <Check className="h-3 w-3 text-emerald-500" />
                            ) : (
                              <Copy className="h-3 w-3" />
                            )}
                          </button>
                        </div>
                      </td>
                      <td className="p-4">
                        <div className="flex items-center gap-1.5 font-mono text-xs font-semibold text-foreground bg-primary/5 px-2 py-1 rounded w-fit border border-primary/10">
                          <Clock className="h-3.5 w-3.5 text-primary" />
                          {task.expression}
                        </div>
                      </td>
                      <td className="p-4 font-medium text-foreground max-w-sm">
                        <div className="flex items-start gap-2">
                          <Terminal className="h-4 w-4 text-muted-foreground/75 mt-0.5 shrink-0" />
                          <span className="line-clamp-2" title={task.instruction}>
                            {task.instruction}
                          </span>
                        </div>
                      </td>
                      <td className="p-4">
                        <Badge variant="outline" className="gap-1 bg-background/50">
                          <Bot className="h-3 w-3 text-primary" />
                          {task.target_agent || 'L1'}
                        </Badge>
                      </td>
                      <td className="p-4">
                        <span
                          onClick={() => handleToggleStatus(task)}
                          className={`inline-flex items-center gap-1.5 cursor-pointer rounded-full px-2.5 py-0.5 text-xs font-medium transition-all hover:opacity-85 ${
                            task.status === 'active'
                              ? 'bg-emerald-500/10 text-emerald-500 border border-emerald-500/20'
                              : task.status === 'completed'
                                ? 'bg-blue-500/10 text-blue-500 border border-blue-500/20'
                                : 'bg-zinc-500/10 text-zinc-500 border border-zinc-500/20'
                          }`}
                        >
                          <span
                            className={`h-1.5 w-1.5 rounded-full ${
                              task.status === 'active'
                                ? 'bg-emerald-500 animate-pulse'
                                : task.status === 'completed'
                                  ? 'bg-blue-500'
                                  : 'bg-zinc-500'
                            }`}
                          />
                          {task.status}
                        </span>
                      </td>
                      <td className="p-4 text-xs text-muted-foreground">
                        <div className="flex flex-col gap-0.5">
                          <div className="flex items-center gap-1">
                            <Calendar className="h-3.5 w-3.5" />
                            <span>{formatTime(task.next_run_at)}</span>
                          </div>
                          {task.last_run_at && (
                            <span className="text-[10px] text-muted-foreground/60 pl-4.5">
                              Last: {formatTime(task.last_run_at)}
                            </span>
                          )}
                        </div>
                      </td>
                      <td className="p-4 text-right">
                        <div className="flex items-center justify-end gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                          <button
                            onClick={() => handleToggleStatus(task)}
                            className="p-1.5 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
                            title={task.status === 'active' ? 'Pause' : 'Activate'}
                          >
                            {task.status === 'active' ? (
                              <Pause className="h-4 w-4" />
                            ) : (
                              <Play className="h-4 w-4" />
                            )}
                          </button>
                          <button
                            onClick={() => handleOpenEditDialog(task)}
                            className="p-1.5 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
                            title="Edit"
                          >
                            <Edit className="h-4 w-4" />
                          </button>
                          <button
                            onClick={() => handleDeleteTask(task.id)}
                            className="p-1.5 rounded hover:bg-muted text-destructive hover:text-destructive/80 transition-colors"
                            title="Delete"
                          >
                            <Trash2 className="h-4 w-4" />
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            {/* Mobile/Tablet Card View */}
            <div className="grid gap-3.5 lg:hidden">
              {tasks.map((task) => (
                <GlassCard
                  key={task.id}
                  className="relative flex flex-col gap-3.5 border-border bg-card/30"
                >
                  <div className="flex items-start justify-between">
                    <div className="flex flex-col gap-1">
                      <div className="flex items-center gap-1.5">
                        <div className="flex items-center gap-1 font-mono text-[11px] font-semibold text-foreground bg-primary/5 px-2 py-0.5 rounded border border-primary/10">
                          <Clock className="h-3 w-3 text-primary" />
                          {task.expression}
                        </div>
                        <Badge
                          variant="outline"
                          className="text-[10px] py-0 px-1.5 bg-background/50"
                        >
                          {task.target_agent || 'L1'}
                        </Badge>
                      </div>
                      <span className="text-[10px] text-muted-foreground font-mono mt-0.5">
                        {task.id}
                      </span>
                    </div>

                    <span
                      onClick={() => handleToggleStatus(task)}
                      className={`inline-flex items-center gap-1.5 cursor-pointer rounded-full px-2 py-0.5 text-[10px] font-medium transition-all ${
                        task.status === 'active'
                          ? 'bg-emerald-500/10 text-emerald-500 border border-emerald-500/20'
                          : task.status === 'completed'
                            ? 'bg-blue-500/10 text-blue-500 border border-blue-500/20'
                            : 'bg-zinc-500/10 text-zinc-500 border border-zinc-500/20'
                      }`}
                    >
                      <span
                        className={`h-1.5 w-1.5 rounded-full ${
                          task.status === 'active'
                            ? 'bg-emerald-500 animate-pulse'
                            : task.status === 'completed'
                              ? 'bg-blue-500'
                              : 'bg-zinc-500'
                        }`}
                      />
                      {task.status}
                    </span>
                  </div>

                  <div className="text-sm font-medium text-foreground bg-background/25 p-2.5 rounded-lg border border-border/30">
                    <div className="flex items-start gap-2">
                      <Terminal className="h-3.5 w-3.5 text-muted-foreground/75 mt-0.5 shrink-0" />
                      <span className="font-mono text-xs">{task.instruction}</span>
                    </div>
                  </div>

                  <div className="flex items-center justify-between border-t border-border/50 pt-3 text-[11px] text-muted-foreground">
                    <div className="flex flex-col gap-0.5">
                      <div className="flex items-center gap-1">
                        <Calendar className="h-3 w-3" />
                        <span>Next: {formatTime(task.next_run_at)}</span>
                      </div>
                      {task.last_run_at && (
                        <div className="flex items-center gap-1 opacity-70">
                          <Check className="h-3 w-3 text-emerald-500" />
                          <span>Last: {formatTime(task.last_run_at)}</span>
                        </div>
                      )}
                    </div>

                    <div className="flex items-center gap-1">
                      <button
                        onClick={() => handleToggleStatus(task)}
                        className="p-1.5 rounded bg-muted/40 hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
                      >
                        {task.status === 'active' ? (
                          <Pause className="h-3.5 w-3.5" />
                        ) : (
                          <Play className="h-3.5 w-3.5" />
                        )}
                      </button>
                      <button
                        onClick={() => handleOpenEditDialog(task)}
                        className="p-1.5 rounded bg-muted/40 hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
                      >
                        <Edit className="h-3.5 w-3.5" />
                      </button>
                      <button
                        onClick={() => handleDeleteTask(task.id)}
                        className="p-1.5 rounded bg-destructive/10 hover:bg-destructive/20 text-destructive transition-colors"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    </div>
                  </div>
                </GlassCard>
              ))}
            </div>
          </div>
        )}
      </div>

      {/* Task Create/Edit Dialog Modal */}
      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Clock className="h-4.5 w-4.5 text-primary" />
              {editingTask ? 'Edit Scheduled Task' : 'New Scheduled Task'}
            </DialogTitle>
          </DialogHeader>

          <div className="space-y-4 py-2">
            <Input
              label="Schedule Expression / Datetime *"
              placeholder="e.g. 0 12 * * 1 or 2026-05-24 15:30:00"
              value={expression}
              onChange={(e) => setExpression(e.target.value)}
            />
            <p className="text-[10px] text-muted-foreground/80 leading-relaxed -mt-2.5 px-0.5">
              Supports standard 5-field cron (<code>m h dom mon dow</code>), shorthands (
              <code>daily</code>, <code>weekly</code>, <code>hourly</code>) or local absolute
              datetime (<code>YYYY-MM-DD HH:MM:SS</code>).
            </p>

            <div className="flex flex-col gap-1.5">
              <label className="text-xs font-medium text-muted-foreground">
                Instruction Prompt *
              </label>
              <textarea
                value={instruction}
                onChange={(e) => setInstruction(e.target.value)}
                placeholder="Type the prompt or reminder you want to send to the agent..."
                rows={5}
                className="w-full resize-y rounded-md border border-border bg-transparent px-3 py-2 text-sm text-foreground outline-none transition-colors placeholder:text-muted-foreground/40 focus-visible:border-primary focus-visible:ring-2 focus-visible:ring-ring/50 font-mono"
              />
            </div>

            <Select
              label="Target Execution Agent"
              options={agentOptions}
              value={targetAgent}
              onChange={setTargetAgent}
            />
          </div>

          <div className="flex items-center justify-between border-t border-border pt-4 mt-2">
            {dialogError ? (
              <p className="text-xs font-semibold text-destructive">{dialogError}</p>
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
              <Button size="sm" onClick={handleSaveTask} disabled={dialogSaving}>
                {dialogSaving ? 'Saving...' : editingTask ? 'Save Changes' : 'Schedule Task'}
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}
