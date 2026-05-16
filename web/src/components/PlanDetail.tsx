import { useState, useEffect, useCallback, useMemo } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { Plan } from '@/types'
import type { Components } from 'react-markdown'
import { getPlan, toggleTodo, deleteTodo as apiDeleteTodo } from '@/lib/api'
import { usePlanStore } from '@/stores/planStore'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import { Badge } from '@/components/ui/badge'
import { Progress } from '@/components/ui/progress'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { TodoList } from './TodoList'
import { FilePreview } from './FilePreview'
import { Calendar, Tag, User, Loader2, ListChecks, Pencil, Trash2, Check, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'

interface PlanDetailProps {
  plan: Plan
  open: boolean
  onClose: () => void
}

const statusLabel = {
  plan: 'Plan',
  running: 'Running',
  done: 'Done',
} as const

const statusBadgeClass = {
  plan: 'bg-status-plan text-foreground border-border',
  running: 'bg-status-running text-foreground border-border',
  done: 'bg-status-done text-foreground border-border',
}

const statusOptions = [
  { value: 'plan', label: 'Plan' },
  { value: 'running', label: 'Running' },
  { value: 'done', label: 'Done' },
]

function linkifyFilePaths(content: string): string {
  return content
    .replace(/(~\/\.soloqueue\/plan\/[^\s)\]]+)/g, '[$1](file://$1)')
    .replace(/(\/home\/\w+\/\.soloqueue\/plan\/[^\s)\]]+)/g, '[$1](file://$1)')
}

export function PlanDetail({ plan, open, onClose }: PlanDetailProps) {
  const updatePlan = usePlanStore((s) => s.updatePlan)
  const deletePlan = usePlanStore((s) => s.deletePlan)
  const storePlans = usePlanStore((s) => s.plans)

  const [fullPlan, setFullPlan] = useState<Plan | null>(null)
  const [loading, setLoading] = useState(false)
  const [previewPath, setPreviewPath] = useState<string | null>(null)

  const [editing, setEditing] = useState(false)
  const [editTitle, setEditTitle] = useState('')
  const [editContent, setEditContent] = useState('')
  const [editTags, setEditTags] = useState('')
  const [editStatus, setEditStatus] = useState('plan')
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)
  const [deleting, setDeleting] = useState(false)

  const current = fullPlan ?? plan

  useEffect(() => {
    if (!open) return
    setEditing(false)
    setShowDeleteConfirm(false)
    setPreviewPath(null)
    setLoading(true)
    getPlan(plan.id)
      .then((data) => setFullPlan(data))
      .catch(() => setFullPlan(plan))
      .finally(() => setLoading(false))
  }, [open, plan.id, plan])

  // Sync from store if plan was updated externally
  useEffect(() => {
    if (!open) return
    const updated = storePlans.find((p) => p.id === plan.id)
    if (updated && fullPlan && updated.updated_at !== fullPlan.updated_at) {
      setFullPlan(updated)
    }
  }, [storePlans, plan.id, fullPlan, open])

  function startEditing() {
    setEditTitle(current.title)
    setEditContent(current.content ?? '')
    setEditTags(current.tags ?? '')
    setEditStatus(current.status)
    setSaveError(null)
    setEditing(true)
  }

  function cancelEditing() {
    setEditing(false)
    setSaveError(null)
  }

  async function handleSave() {
    if (!editTitle.trim()) {
      setSaveError('Title is required')
      return
    }
    setSaving(true)
    setSaveError(null)
    try {
      const updated = await updatePlan(current.id, {
        title: editTitle.trim(),
        content: editContent.trim() || undefined,
        tags: editTags.trim() || undefined,
        status: editStatus,
      })
      setFullPlan(updated)
      setEditing(false)
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : 'Failed to save')
    } finally {
      setSaving(false)
    }
  }

  async function handleDelete() {
    setDeleting(true)
    try {
      await deletePlan(current.id)
      setDeleting(false)
      onClose()
    } catch {
      setDeleting(false)
      setShowDeleteConfirm(false)
    }
  }

  const handleFileClick = useCallback((path: string) => {
    setPreviewPath(path)
  }, [])

  const tags = current.tags
    ? current.tags.split(',').map((t) => t.trim()).filter(Boolean)
    : []

  const todos = fullPlan?.todo_items ?? current.todo_items ?? []
  const completedCount = todos.filter((t) => t.completed).length
  const progressPct = todos.length > 0 ? Math.round((completedCount / todos.length) * 100) : 0

  function handleToggleTodo(todoId: string) {
    if (!fullPlan) return
    toggleTodo(fullPlan.id, todoId)
      .then((updated) => {
        setFullPlan((prev) => {
          if (!prev) return prev
          return {
            ...prev,
            todo_items: prev.todo_items?.map((t) => (t.id === todoId ? updated : t)) ?? [],
          }
        })
      })
      .catch(() => {})
  }

  function handleTodoDelete(todoId: string) {
    if (!fullPlan) return
    apiDeleteTodo(fullPlan.id, todoId).catch(() => {})
    setFullPlan((prev) => {
      if (!prev) return prev
      return {
        ...prev,
        todo_items: prev.todo_items?.filter((t) => t.id !== todoId) ?? [],
      }
    })
  }

  const linkifiedContent = useMemo(
    () => (current.content ? linkifyFilePaths(current.content) : ''),
    [current.content]
  )

  const markdownComponents: Components = useMemo(
    () => ({
      a({ href, children }) {
        if (href?.startsWith('file://')) {
          const realPath = href.slice(7)
          return (
            <button
              type="button"
              className="text-primary underline underline-offset-2 hover:text-primary/80 cursor-pointer bg-transparent border-0 p-0 font-inherit"
              onClick={(e) => {
                e.preventDefault()
                handleFileClick(realPath)
              }}
            >
              {children}
            </button>
          )
        }
        return (
          <a
            href={href}
            target="_blank"
            rel="noopener noreferrer"
            className="text-primary underline underline-offset-2"
          >
            {children}
          </a>
        )
      },
    }),
    [handleFileClick]
  )

  return (
    <>
      <Dialog open={open} onOpenChange={(v) => !v && !deleting && onClose()}>
        <DialogContent className="max-w-xl max-h-[85vh] flex flex-col p-0 overflow-hidden">
          {/* Header with edit/delete actions */}
          <DialogHeader className="px-6 pt-6 pb-0">
            <div className="flex items-start justify-between gap-2">
              <DialogTitle className="text-lg font-semibold leading-tight pr-4 flex-1 min-w-0">
                {editing ? (
                  <input
                    value={editTitle}
                    onChange={(e) => setEditTitle(e.target.value)}
                    className="w-full rounded-md border border-border bg-transparent px-2 py-1 text-lg font-semibold outline-none focus-visible:border-primary focus-visible:ring-2 focus-visible:ring-ring/50"
                  />
                ) : (
                  current.title
                )}
              </DialogTitle>

              {!loading && !editing && (
                <div className="flex items-center gap-1 shrink-0 pt-1">
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="h-7 w-7 text-muted-foreground hover:text-primary"
                    onClick={startEditing}
                    title="Edit"
                  >
                    <Pencil className="h-3.5 w-3.5" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="h-7 w-7 text-muted-foreground hover:text-destructive"
                    onClick={() => setShowDeleteConfirm(true)}
                    title="Delete"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
              )}

              {!loading && editing && (
                <div className="flex items-center gap-1 shrink-0 pt-1">
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="h-7 w-7 text-muted-foreground hover:text-primary"
                    onClick={handleSave}
                    disabled={saving}
                    title="Save"
                  >
                    {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Check className="h-3.5 w-3.5" />}
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="h-7 w-7 text-muted-foreground hover:text-muted-foreground/50"
                    onClick={cancelEditing}
                    disabled={saving}
                    title="Cancel"
                  >
                    <X className="h-3.5 w-3.5" />
                  </Button>
                </div>
              )}
            </div>
          </DialogHeader>

          {loading ? (
            <div className="flex flex-1 items-center justify-center py-16">
              <Loader2 className="h-7 w-7 animate-spin text-muted-foreground" />
            </div>
          ) : (
            <ScrollArea className="flex-1 max-h-[calc(85vh-8rem)] px-6 py-5">
              <div className="space-y-5">
                {/* Status + meta row */}
                <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
                  {editing ? (
                    <Select
                      options={statusOptions}
                      value={editStatus}
                      onChange={setEditStatus}
                    />
                  ) : (
                    <Badge
                      variant="outline"
                      className={cn('border font-medium', statusBadgeClass[current.status])}
                    >
                      {statusLabel[current.status]}
                    </Badge>
                  )}

                  {current.creator && (
                    <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                      <User className="h-3 w-3" />
                      {current.creator}
                    </span>
                  )}

                  <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                    <Calendar className="h-3 w-3" />
                    {new Date(current.created_at).toLocaleDateString()}
                  </span>
                </div>

                {/* Tags */}
                {editing ? (
                  <Input
                    label="Tags"
                    placeholder="Comma-separated, e.g. bug,frontend"
                    value={editTags}
                    onChange={(e) => setEditTags(e.target.value)}
                  />
                ) : tags.length > 0 ? (
                  <div className="flex flex-wrap gap-1.5">
                    {tags.map((tag) => (
                      <span
                        key={tag}
                        className="inline-flex items-center gap-1 rounded-md bg-muted px-2 py-0.5 text-xs text-muted-foreground"
                      >
                        <Tag className="h-2.5 w-2.5" />
                        {tag}
                      </span>
                    ))}
                  </div>
                ) : null}

                {/* Content */}
                {editing ? (
                  <div className="flex flex-col gap-1.5">
                    <label className="text-xs font-medium text-muted-foreground">Content</label>
                    <textarea
                      value={editContent}
                      onChange={(e) => setEditContent(e.target.value)}
                      rows={10}
                      className="w-full resize-y rounded-md border border-border bg-transparent px-3 py-2 text-sm text-foreground outline-none transition-colors placeholder:text-muted-foreground/50 focus-visible:border-primary focus-visible:ring-2 focus-visible:ring-ring/50 font-mono"
                    />
                  </div>
                ) : current.content ? (
                  <div className="rounded-lg border-2 border-[#EEEEEE] bg-muted/30 p-4">
                    <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>
                      {linkifiedContent}
                    </ReactMarkdown>
                  </div>
                ) : null}

                {/* Save error */}
                {saveError && (
                  <p className="text-xs text-destructive">{saveError}</p>
                )}

                {/* Delete confirmation */}
                {showDeleteConfirm && (
                  <div className="rounded-lg border border-destructive/30 bg-destructive/5 p-4">
                    <p className="text-sm font-medium text-destructive mb-3">
                      Delete this plan? This action cannot be undone.
                    </p>
                    <div className="flex items-center gap-2">
                      <Button variant="destructive" size="sm" onClick={handleDelete} disabled={deleting}>
                        {deleting ? (
                          <><Loader2 className="mr-1 h-3 w-3 animate-spin" /> Deleting...</>
                        ) : (
                          'Delete'
                        )}
                      </Button>
                      <Button variant="outline" size="sm" onClick={() => setShowDeleteConfirm(false)} disabled={deleting}>
                        Cancel
                      </Button>
                    </div>
                  </div>
                )}

                {!editing && !showDeleteConfirm && (
                  <>
                    <Separator />

                    {/* Tasks section */}
                    <div className="space-y-3">
                      <div className="flex items-center justify-between">
                        <h3 className="text-sm font-semibold tracking-tight text-foreground flex items-center gap-2">
                          <ListChecks className="h-4 w-4 text-primary" />
                          Tasks
                        </h3>

                        {todos.length > 0 && (
                          <div className="flex items-center gap-2">
                            <span className="text-xs tabular-nums text-muted-foreground">
                              {completedCount}/{todos.length}
                            </span>
                            <span className="text-xs text-muted-foreground">completed</span>
                            <Progress value={progressPct} className="w-20 h-1.5" />
                          </div>
                        )}
                      </div>

                      <TodoList
                        todos={todos}
                        onToggle={handleToggleTodo}
                        onDelete={handleTodoDelete}
                        planId={current.id}
                      />
                    </div>
                  </>
                )}
              </div>
            </ScrollArea>
          )}
        </DialogContent>
      </Dialog>

      {previewPath && (
        <FilePreview path={previewPath} open={!!previewPath} onClose={() => setPreviewPath(null)} />
      )}
    </>
  )
}
