import { useEffect, useState, type FormEvent, type MouseEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Plus,
  AlertCircle,
  Loader2,
  X,
  Workflow,
  Clock,
  RefreshCw,
} from 'lucide-react'
import { toast } from 'sonner'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { useTranslation } from '@/lib/i18n'
import {
  defaultYAMLTemplate,
  unknownAgentTemplates,
  useWorkflowStore,
  yamlToGraph,
} from '@/stores/workflowStore'
import { WorkflowCard } from './WorkflowCard'
import { WorkflowRunCard } from './WorkflowRunCard'

// ─── Create Sheet ───────────────────────────────────────────────────────

interface CreateSheetProps {
  open: boolean
  onClose: () => void
  onCreated: (name: string) => void
}

function CreateSheet({ open, onClose, onCreated }: CreateSheetProps) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [yaml, setYaml] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [selectedAgent, setSelectedAgent] = useState('')
  const {
    availableAgents,
    availableAgentsLoading,
    availableAgentsError,
    fetchAvailableAgents,
  } = useWorkflowStore()

  useEffect(() => {
    if (open) void fetchAvailableAgents()
  }, [open, fetchAvailableAgents])

  const effectiveSelectedAgent = selectedAgent || availableAgents[0]?.id || ''

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (!name.trim()) return
    try {
      setCreating(true)
      setCreateError(null)
      const workflowYaml = yaml.trim() || defaultYAMLTemplate(name.trim(), effectiveSelectedAgent)
      const parsed = yamlToGraph(workflowYaml)
      const unknownAgents = parsed
        ? unknownAgentTemplates(parsed.agents, availableAgents)
        : []
      if (!parsed || unknownAgents.length > 0) {
        throw new Error(t('workflow.unknownAgents', { names: unknownAgents.join(', ') || '—' }))
      }
      const store = useWorkflowStore.getState()
      const success = await store.createWorkflow(name.trim(), workflowYaml)
      if (success) {
        toast.success(t('workflow.createSuccess'))
        onCreated(name.trim())
      } else {
        throw new Error(t('workflow.createFailed'))
      }
    } catch (err: any) {
      setCreateError(err.message || t('workflow.createFailed'))
    } finally {
      setCreating(false)
    }
  }

  const handleClose = () => {
    if (creating) return
    onClose()
  }

  if (!open) return null

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 z-40 bg-black/30 backdrop-blur-sm transition-opacity duration-300"
        onClick={handleClose}
      />

      {/* Sheet Panel */}
      <div className="fixed right-0 top-0 bottom-0 z-50 w-[480px] flex flex-col bg-card border-l border-border shadow-2xl transition-transform duration-300 ease-out translate-x-0">
        {/* Sheet Header */}
        <div className="shrink-0 flex items-center justify-between px-6 py-4 border-b border-border/60">
          <div className="flex items-center gap-2.5">
            <div className="h-8 w-8 rounded-lg bg-primary/15 flex items-center justify-center">
              <Workflow className="h-4 w-4 text-primary" />
            </div>
            <div>
              <h2 className="text-sm font-semibold text-foreground">{t('workflow.newWorkflow')}</h2>
              <p className="text-[10px] text-muted-foreground font-mono">{t('workflow.fromYAML')}</p>
            </div>
          </div>
          <button
            type="button"
            onClick={handleClose}
            disabled={creating}
            className="rounded-lg p-1.5 text-muted-foreground hover:text-foreground hover:bg-muted transition-colors disabled:opacity-40 electron-no-drag"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        {/* Sheet Body */}
        <div className="flex-1 overflow-y-auto p-6">
          <p className="mb-5 text-xs text-muted-foreground leading-relaxed">
            {t('workflow.description')}
          </p>

          <form id="create-workflow-form" onSubmit={handleSubmit} className="space-y-4">
            {/* Name */}
            <div>
              <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-1.5">
                {t('workflow.name')}
              </label>
              <Input
                required
                placeholder="my-workflow"
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="font-mono text-xs"
              />
            </div>

            {/* Existing agent — the workflow may only reference DB-backed agents. */}
            <div>
              <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-1.5">
                {t('workflow.startAgent')}
              </label>
              <select
                value={effectiveSelectedAgent}
                onChange={(event) => setSelectedAgent(event.target.value)}
                disabled={availableAgentsLoading || availableAgents.length === 0}
                className="flex h-9 w-full rounded-lg border border-input bg-card px-3 text-xs text-foreground disabled:opacity-50"
              >
                {availableAgents.map(agent => (
                  <option key={agent.id} value={agent.id}>
                    {agent.name}{agent.team_name ? ` · ${agent.team_name}` : ''}
                  </option>
                ))}
              </select>
              {availableAgentsLoading && (
                <span className="mt-1 block text-[10px] text-muted-foreground">{t('common.loading')}</span>
              )}
              {!availableAgentsLoading && availableAgentsError && (
                <span className="mt-1 block text-[10px] text-rose-500">{t('workflow.agentsLoadFailed')}</span>
              )}
              {!availableAgentsLoading && !availableAgentsError && availableAgents.length === 0 && (
                <span className="mt-1 block text-[10px] text-rose-500">{t('workflow.noAgentsAvailable')}</span>
              )}
            </div>

            {/* Optional YAML */}
            <div>
              <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-1.5">
                YAML {t('common.preview')}
              </label>
              <Textarea
                rows={16}
                placeholder={defaultYAMLTemplate(name || 'my-workflow', effectiveSelectedAgent || undefined)}
                value={yaml}
                onChange={(e) => setYaml(e.target.value)}
                className="resize-none font-mono text-xs"
              />
              <span className="text-[10px] text-muted-foreground/50 font-mono mt-1">
                {t('workflow.fromYAML')}
              </span>
            </div>

            {/* Error */}
            {createError && (
              <div className="flex items-start gap-2 rounded-xl bg-rose-500/10 p-3 text-xs text-rose-500 border border-rose-500/20">
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
            form="create-workflow-form"
            disabled={creating || !name.trim() || availableAgentsLoading || availableAgents.length === 0}
            className="flex w-full items-center justify-center gap-2 rounded-xl bg-primary hover:bg-primary/90 disabled:bg-primary/40 px-4 py-3 text-sm font-semibold text-primary-foreground transition-all shadow-lg shadow-primary/20 cursor-pointer disabled:cursor-not-allowed"
          >
            {creating ? (
              <>
                <Loader2 className="h-4 w-4 animate-spin" />
                {t('common.creating')}
              </>
            ) : (
              <>
                <Plus className="h-4 w-4" />
                {t('workflow.createAndOpen')}
              </>
            )}
          </button>
        </div>
      </div>
    </>
  )
}

// ─── Main Page ──────────────────────────────────────────────────────────

export function WorkflowListPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const {
    workflowMetas,
    workflowMetasLoading,
    workflowMetasError,
    fetchWorkflowMetas,
    fetchRuns,
    deleteWorkflow,
    runs,
  } = useWorkflowStore()

  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [sheetOpen, setSheetOpen] = useState(false)

  useEffect(() => {
    fetchWorkflowMetas()
  }, [fetchWorkflowMetas])

  useEffect(() => {
    workflowMetas.forEach((workflow) => { void fetchRuns(workflow.name) })
  }, [workflowMetas, fetchRuns])

  const handleDelete = (name: string, e: MouseEvent) => {
    e.stopPropagation()
    setDeleteTarget(name)
  }

  const confirmDelete = async () => {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      const success = await deleteWorkflow(deleteTarget)
      if (success) {
        toast.success(t('workflow.deleted'))
        fetchWorkflowMetas()
      } else {
        toast.error(t('workflow.deleteFailed'))
      }
    } catch (err: any) {
      toast.error(err.message || t('workflow.deleteFailed'))
    } finally {
      setDeleting(false)
      setDeleteTarget(null)
    }
  }

  // Partition: running runs first
  const allRuns = Object.values(runs).flat()
  const activeRuns = allRuns.filter(r => r.status === 'running')
  const validWorkflows = workflowMetas.filter(m => m.valid)
  const invalidWorkflows = workflowMetas.filter(m => !m.valid)

  return (
    <>
      <div className="flex h-full flex-col overflow-hidden bg-background text-foreground">
        {/* Header */}
        <div className="shrink-0 flex items-center justify-between border-b border-border/60 px-8 py-5 bg-card/20 backdrop-blur-sm">
          <div>
            <h1 className="text-xl font-bold tracking-tight text-foreground">{t('workflow.title')}</h1>
            <p className="mt-0.5 text-xs text-muted-foreground">
              {t('workflow.description')}
            </p>
          </div>
          <div className="flex items-center gap-3 electron-no-drag">
            <button
              onClick={fetchWorkflowMetas}
              disabled={workflowMetasLoading}
              className="flex items-center gap-1.5 rounded-lg border border-border/60 px-3 py-2 text-xs text-muted-foreground hover:text-foreground hover:bg-muted/40 transition-colors disabled:opacity-50"
            >
              <RefreshCw className={`h-3.5 w-3.5 ${workflowMetasLoading ? 'animate-spin' : ''}`} />
              {t('workflow.refresh')}
            </button>
            <button
              onClick={() => setSheetOpen(true)}
              className="flex items-center gap-2 rounded-xl bg-primary hover:bg-primary/90 px-4 py-2.5 text-sm font-semibold text-primary-foreground transition-all shadow-md shadow-primary/20 cursor-pointer"
            >
              <Plus className="h-4 w-4" />
              {t('workflow.newWorkflow')}
            </button>
          </div>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto px-8 py-6">
          <div className="mx-auto max-w-4xl space-y-6">
            {/* Active Runs */}
            {activeRuns.length > 0 && (
              <div>
                <div className="flex items-center gap-2 mb-3">
                  <span className="relative flex h-2 w-2">
                    <span className="absolute inset-0 rounded-full bg-signal animate-ping opacity-60" />
                    <span className="absolute inset-0.5 rounded-full bg-signal" />
                  </span>
                  <h2 className="text-xs font-bold text-muted-foreground uppercase tracking-wider font-mono">
                    {t('workflow.progress', { completed: activeRuns.reduce((s, r) => s + r.completed_count, 0), total: activeRuns.reduce((s, r) => s + r.node_count, 0) })}
                  </h2>
                </div>
                <div className="space-y-2">
                  {activeRuns.map((run) => (
                    <WorkflowRunCard
                      key={run.id}
                      run={run}
                      onClick={() => navigate(`/workflows/${run.workflow_name}/runs/${run.id}`)}
                    />
                  ))}
                </div>
              </div>
            )}

            {/* Workflow Definitions */}
            <div>
              <div className="flex items-center justify-between mb-3">
                <h2 className="text-xs font-bold text-muted-foreground uppercase tracking-wider font-mono">
                  {t('workflow.title')} ({workflowMetas.length})
                </h2>
              </div>

              {workflowMetasLoading ? (
                /* Skeleton */
                <div className="space-y-2">
                  {[1, 2, 3].map((i) => (
                    <div
                      key={i}
                      className="rounded-xl border border-border/40 bg-card/20 h-[84px] animate-pulse"
                    />
                  ))}
                </div>
              ) : workflowMetasError ? (
                /* Error state */
                <div className="flex flex-col items-center justify-center rounded-xl border border-border/50 bg-card/20 py-16 text-center">
                  <AlertCircle className="h-8 w-8 text-rose-500 mb-3" />
                  <p className="text-sm font-semibold text-rose-500">{workflowMetasError}</p>
                  <button
                    onClick={fetchWorkflowMetas}
                    className="mt-4 rounded-lg bg-muted hover:bg-muted/80 px-4 py-1.5 text-xs text-foreground transition-colors"
                  >
                    {t('workflow.refresh')}
                  </button>
                </div>
              ) : workflowMetas.length === 0 ? (
                /* Empty state */
                <div className="flex flex-col items-center justify-center rounded-xl border border-dashed border-border/60 bg-card/10 py-20 text-center">
                  <div className="h-16 w-16 rounded-2xl bg-primary/10 flex items-center justify-center mb-4">
                    <Workflow className="h-8 w-8 text-primary/60" />
                  </div>
                  <h3 className="text-base font-semibold text-foreground mb-1">
                    {t('workflow.noWorkflowsYet')}
                  </h3>
                  <p className="text-sm text-muted-foreground max-w-xs mb-6">
                    {t('workflow.noWorkflowsDesc')}
                  </p>
                  <button
                    onClick={() => setSheetOpen(true)}
                    className="flex items-center gap-2 rounded-xl bg-primary hover:bg-primary/90 px-6 py-3 text-sm font-semibold text-primary-foreground transition-all shadow-md shadow-primary/20"
                  >
                    <Plus className="h-4 w-4" />
                    {t('workflow.createFirst')}
                  </button>
                </div>
              ) : (
                <div className="space-y-2">
                  {validWorkflows.map((meta) => (
                    <WorkflowCard
                      key={meta.name}
                      meta={meta}
                      onClick={() => navigate(`/workflows/${meta.name}`)}
                      onDelete={(e) => handleDelete(meta.name, e)}
                    />
                  ))}
                  {invalidWorkflows.map((meta) => (
                    <WorkflowCard
                      key={meta.name}
                      meta={meta}
                      onClick={() => navigate(`/workflows/${meta.name}`)}
                      onDelete={(e) => handleDelete(meta.name, e)}
                    />
                  ))}
                </div>
              )}
            </div>

            {/* Stats footer */}
            {!workflowMetasLoading && !workflowMetasError && workflowMetas.length > 0 && (
              <div className="flex items-center gap-6 pt-2 text-[10px] text-muted-foreground/60 font-mono">
                <span className="flex items-center gap-1">
                  <Workflow className="h-3 w-3" />
                  {t('workflow.title')}: {workflowMetas.length}
                </span>
                <span className="flex items-center gap-1">
                  <Clock className="h-3 w-3" />
                  {allRuns.length > 0
                    ? `${t('workflow.progress', { completed: '', total: '' }).replace(' / ', ` ${allRuns.length} / `)}` // rough
                    : t('workflow.noRuns')
                  }
                </span>
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Create Sheet */}
      {sheetOpen && <CreateSheet
        open
        onClose={() => setSheetOpen(false)}
        onCreated={(name) => {
          setSheetOpen(false)
          navigate(`/workflows/${name}`)
        }}
      />}

      {/* Delete Confirm */}
      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => { if (!open) setDeleteTarget(null) }}
        title={t('workflow.deleteTitle')}
        message={t('workflow.deleteMessage')}
        destructive
        onConfirm={confirmDelete}
        confirmLabel={t('workflow.deleteConfirm')}
        loading={deleting}
      />
    </>
  )
}
