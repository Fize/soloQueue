import { useState, useEffect, useCallback, useMemo } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { useParams, useNavigate } from 'react-router-dom'
import type { Plan } from '@/types'
import type { Components } from 'react-markdown'
import { getPlan, toggleTodo, deleteTodo as apiDeleteTodo } from '@/lib/api'
import { usePlanStore } from '@/stores/planStore'
import { Separator } from '@/components/ui/separator'
import { Badge } from '@/components/ui/badge'
import { Progress } from '@/components/ui/progress'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { TodoList } from './TodoList'
import { DependencyChain } from './DependencyChain'
import { FilePreview } from './FilePreview'
import { GlassCard } from '@/components/ui/glass-card'
import {
  Calendar,
  Tag,
  User,
  Loader2,
  ListChecks,
  Pencil,
  Trash2,
  Check,
  X,
  GitBranch,
  ArrowLeft,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'

interface PlanDetailProps {
  plan?: Plan
  open?: boolean
  onClose?: () => void
}

const statusLabel = {
  plan: 'Plan',
  running: 'Running',
  done: 'Done',
} as const

const statusBadgeClass = {
  plan: 'bg-blue-500/10 text-blue-400 border-blue-500/30',
  running: 'bg-amber-500/10 text-amber-400 border-amber-500/30',
  done: 'bg-emerald-500/10 text-emerald-400 border-emerald-500/30',
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

export function PlanDetail({
  plan: propPlan,
  open: propOpen,
  onClose: propOnClose,
}: PlanDetailProps = {}) {
  const { id: routeId } = useParams<{ id: string }>()
  const navigate = useNavigate()

  const planId = propPlan?.id ?? routeId
  const isOpen = propOpen ?? true
  const handleClose = propOnClose ?? (() => navigate('/plans'))

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
  const [showDepsGraph, setShowDepsGraph] = useState(false)

  const current = fullPlan ?? propPlan ?? ({} as Plan)

  const refreshPlan = useCallback(() => {
    if (!planId) return
    getPlan(planId)
      .then((data) => setFullPlan(data))
      .catch(() => {})
  }, [planId])

  useEffect(() => {
    if (!isOpen || !planId) return
    setEditing(false)
    setShowDeleteConfirm(false)
    setPreviewPath(null)
    setLoading(true)
    getPlan(planId)
      .then((data) => setFullPlan(data))
      .catch(() => {
        if (propPlan) setFullPlan(propPlan)
      })
      .finally(() => setLoading(false))
  }, [isOpen, planId, propPlan])

  // Sync from store if plan was updated externally
  useEffect(() => {
    if (!isOpen || !planId) return
    const updated = storePlans.find((p) => p.id === planId)
    if (updated && fullPlan && updated.updated_at !== fullPlan.updated_at) {
      setFullPlan(updated)
    }
  }, [storePlans, planId, fullPlan, isOpen])

  function startEditing() {
    setEditTitle(current.title || '')
    setEditContent(current.content ?? '')
    setEditTags(current.tags ?? '')
    setEditStatus(current.status || 'plan')
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
      handleClose()
    } catch {
      setDeleting(false)
      setShowDeleteConfirm(false)
    }
  }

  const handleFileClick = useCallback((path: string) => {
    setPreviewPath(path)
  }, [])

  const tags = current.tags
    ? current.tags
        .split(',')
        .map((t) => t.trim())
        .filter(Boolean)
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

  if (!isOpen) return null

  return (
    <div className="flex h-full flex-col min-w-0 bg-background overflow-hidden pb-16 md:pb-0">
      {/* Sticky Header */}
      <header className="flex shrink-0 items-center justify-between border-b border-border/80 px-4 py-3 md:px-6 bg-card/65 backdrop-blur-md sticky top-0 z-10">
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <Button variant="ghost" size="icon" onClick={handleClose} className="h-8 w-8 shrink-0">
            <ArrowLeft className="h-4.5 w-4.5 text-foreground" />
          </Button>
          <div className="min-w-0 flex-1">
            {editing ? (
              <input
                value={editTitle}
                onChange={(e) => setEditTitle(e.target.value)}
                className="w-full max-w-xl rounded-md border border-border bg-transparent px-3 py-1 text-base font-semibold text-foreground outline-none focus:border-primary/55 transition-all"
                placeholder="Plan Title"
              />
            ) : (
              <h1 className="text-base font-bold text-foreground truncate pr-4">
                {current.title || 'Loading Plan...'}
              </h1>
            )}
          </div>
        </div>

        {/* Action Controls */}
        {!loading && !editing && (
          <div className="flex items-center gap-1.5 shrink-0">
            <Button
              variant="outline"
              size="xs"
              className="h-8 w-8 shrink-0"
              onClick={startEditing}
              title="Edit"
            >
              <Pencil className="h-3.5 w-3.5" />
            </Button>
            <Button
              variant="ghost"
              size="xs"
              className="h-8 w-8 shrink-0 text-muted-foreground hover:text-destructive"
              onClick={() => setShowDeleteConfirm(true)}
              title="Delete"
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>
        )}

        {!loading && editing && (
          <div className="flex items-center gap-1.5 shrink-0">
            <Button
              variant="default"
              size="xs"
              className="h-8 gap-1.5"
              onClick={handleSave}
              disabled={saving}
              title="Save"
            >
              {saving ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Check className="h-3.5 w-3.5" />
              )}
              Save
            </Button>
            <Button
              variant="outline"
              size="xs"
              className="h-8 gap-1.5"
              onClick={cancelEditing}
              disabled={saving}
              title="Cancel"
            >
              <X className="h-3.5 w-3.5" />
              Cancel
            </Button>
          </div>
        )}
      </header>

      {/* Main Content (Scrollable) */}
      <div className="flex-1 overflow-y-auto min-h-0 bg-card/10">
        {loading ? (
          <div className="flex items-center justify-center py-20">
            <Loader2 className="h-6 w-6 animate-spin text-primary" />
          </div>
        ) : (
          <div className="max-w-4xl mx-auto px-4 py-6 md:px-8 md:py-8 space-y-6">
            {/* Metadata & Config Panel */}
            <GlassCard className="space-y-4">
              <div className="flex flex-wrap items-center justify-between gap-4 border-b border-border/40 pb-3">
                <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-muted-foreground">
                  {current.creator && (
                    <span className="flex items-center gap-1.5">
                      <User className="h-3.5 w-3.5" />@{current.creator}
                    </span>
                  )}
                  {current.created_at && (
                    <span className="flex items-center gap-1.5">
                      <Calendar className="h-3.5 w-3.5" />
                      {new Date(current.created_at).toLocaleDateString()}
                    </span>
                  )}
                </div>

                {/* Status selector or badge */}
                <div>
                  {editing ? (
                    <Select options={statusOptions} value={editStatus} onChange={setEditStatus} />
                  ) : (
                    <Badge
                      variant="outline"
                      className={cn(
                        'border font-semibold py-0.5 px-2.5 text-xs',
                        statusBadgeClass[current.status]
                      )}
                    >
                      {statusLabel[current.status]}
                    </Badge>
                  )}
                </div>
              </div>

              {/* Tags Section */}
              <div className="space-y-1.5">
                <span className="text-[10px] text-muted-foreground font-bold uppercase tracking-wider block">
                  Tags
                </span>
                {editing ? (
                  <Input
                    placeholder="Comma-separated, e.g. bug,frontend"
                    value={editTags}
                    onChange={(e) => setEditTags(e.target.value)}
                    className="max-w-md"
                  />
                ) : tags.length > 0 ? (
                  <div className="flex flex-wrap gap-1.5">
                    {tags.map((tag) => (
                      <Badge
                        key={tag}
                        variant="secondary"
                        className="text-xs font-semibold py-0.5 px-2"
                      >
                        <Tag className="h-3 w-3 mr-1 text-muted-foreground" />
                        {tag}
                      </Badge>
                    ))}
                  </div>
                ) : (
                  <span className="text-xs text-muted-foreground italic">No tags configured</span>
                )}
              </div>
            </GlassCard>

            {/* Markdown Content Section */}
            {(editing || current.content) && (
              <div className="space-y-2">
                <h3 className="text-xs font-bold uppercase tracking-wider text-muted-foreground">
                  Description / Content
                </h3>
                {editing ? (
                  <textarea
                    value={editContent}
                    onChange={(e) => setEditContent(e.target.value)}
                    rows={12}
                    className="w-full resize-y rounded-lg border border-border bg-muted/30 p-4 font-mono text-xs leading-relaxed focus:outline-none focus:border-primary/55 transition-all text-foreground"
                    placeholder="Describe this plan in Markdown..."
                  />
                ) : (
                  <GlassCard
                    variant="flat"
                    className="p-5 overflow-x-auto prose dark:prose-invert max-w-none text-sm leading-relaxed border border-border/80 bg-card/30"
                  >
                    <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>
                      {linkifiedContent}
                    </ReactMarkdown>
                  </GlassCard>
                )}
              </div>
            )}

            {/* Error messaging */}
            {saveError && <p className="text-sm text-destructive font-semibold">{saveError}</p>}

            {/* Delete confirmation banner */}
            {showDeleteConfirm && (
              <GlassCard variant="error" className="space-y-3">
                <p className="text-sm font-semibold text-destructive">
                  Delete this plan? This action cannot be undone.
                </p>
                <div className="flex items-center gap-2">
                  <Button
                    variant="destructive"
                    size="sm"
                    onClick={handleDelete}
                    disabled={deleting}
                  >
                    {deleting ? (
                      <>
                        <Loader2 className="mr-1 h-3.5 w-3.5 animate-spin" /> Deleting...
                      </>
                    ) : (
                      'Delete'
                    )}
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setShowDeleteConfirm(false)}
                    disabled={deleting}
                  >
                    Cancel
                  </Button>
                </div>
              </GlassCard>
            )}

            {!editing && !showDeleteConfirm && (
              <>
                <Separator className="border-border/40" />

                {/* Tasks & Todo Section */}
                <div className="space-y-4">
                  <GlassCard
                    variant="flat"
                    className="p-4 md:p-6 bg-card/20 border border-border/80"
                  >
                    <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 border-b border-border/40 pb-4 mb-4">
                      <h3 className="text-sm font-bold text-foreground flex items-center gap-2">
                        <ListChecks className="h-4.5 w-4.5 text-primary" />
                        Execution Checklist
                      </h3>

                      {todos.length > 0 && (
                        <div className="flex items-center gap-3">
                          <span className="text-xs font-semibold tabular-nums text-muted-foreground">
                            <span>
                              {completedCount}/{todos.length}
                            </span>{' '}
                            completed ({progressPct}%)
                          </span>
                          <Progress value={progressPct} className="w-24 h-2 bg-muted" />
                        </div>
                      )}
                    </div>

                    <TodoList
                      todos={todos}
                      onToggle={handleToggleTodo}
                      onDelete={handleTodoDelete}
                      planId={current.id}
                      onRefresh={refreshPlan}
                    />
                  </GlassCard>

                  {/* Dependency Graph Accordion */}
                  {todos.some(
                    (t) => (t.depends_on ?? []).length > 0 || (t.blockers ?? []).length > 0
                  ) && (
                    <div className="pt-1">
                      <button
                        type="button"
                        onClick={() => setShowDepsGraph(!showDepsGraph)}
                        className="inline-flex items-center gap-1.5 text-xs font-semibold text-muted-foreground hover:text-foreground transition-colors"
                      >
                        <GitBranch
                          className={cn(
                            'h-3.5 w-3.5 transition-transform text-primary',
                            showDepsGraph && 'rotate-90'
                          )}
                        />
                        Toggle Dependency Chains
                      </button>
                      {showDepsGraph && (
                        <div className="mt-3 p-4 rounded-lg bg-card/35 border border-border/60 animate-in fade-in slide-in-from-top-2 duration-200">
                          <div className="space-y-3 max-h-80 overflow-y-auto">
                            {todos
                              .filter(
                                (t) =>
                                  (t.depends_on ?? []).length > 0 || (t.blockers ?? []).length > 0
                              )
                              .map((t) => (
                                <div
                                  key={t.id}
                                  className="border-b border-border/20 last:border-0 pb-3 last:pb-0"
                                >
                                  <DependencyChain
                                    todos={todos}
                                    selectedTodoId={t.id}
                                    planId={current.id}
                                  />
                                </div>
                              ))}
                          </div>
                        </div>
                      )}
                    </div>
                  )}
                </div>
              </>
            )}
          </div>
        )}
      </div>

      {previewPath && (
        <FilePreview path={previewPath} open={!!previewPath} onClose={() => setPreviewPath(null)} />
      )}
    </div>
  )
}
