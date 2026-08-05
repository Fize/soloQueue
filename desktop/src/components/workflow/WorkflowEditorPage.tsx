import { useEffect, useState, useCallback } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  ArrowLeft,
  Play,
  Save,
  CheckCircle2,
  AlertCircle,
  Loader2,
  RefreshCw,
} from 'lucide-react'
import { toast } from 'sonner'
import { Textarea } from '@/components/ui/textarea'
import { useTranslation } from '@/lib/i18n'
import { unknownAgentTemplates, useWorkflowStore } from '@/stores/workflowStore'
import { useConnectionStore } from '@/stores/connectionStore'
import { useRuntimeStore } from '@/stores/runtimeStore'
import { ReactFlowProvider } from '@xyflow/react'
import { VisualDAGEditor } from './VisualDAGEditor'
import { DAGPreview } from './DAGPreview'
import { shouldWaitForWorkflowBackend } from './workflowPageState'
import { cn } from '@/lib/utils'

const isElectron = typeof window !== 'undefined' && !!window.electronAPI

// ─── YAML Mode Layout ───────────────────────────────────────────────────

export function YAMLEditorView() {
  const { t } = useTranslation()
  const {
    activeWorkflowYAML,
    activeWorkflowGraph,
    activeWorkflowValidationError,
    setYAML,
    validateWorkflow,
  } = useWorkflowStore()
  const [validating, setValidating] = useState(false)
  const lineCount = activeWorkflowYAML.split('\n').length

  const handleChange = useCallback(
    (value: string) => {
      // Keep the canonical draft in the store in lockstep with the textarea so
      // Save and Validate can never observe an older value.
      setYAML(value)
    },
    [setYAML]
  )

  const handleValidate = useCallback(async () => {
    setValidating(true)
    try {
      const result = await validateWorkflow()
      if (result.valid) {
        toast.success(t('workflow.valid'))
      } else {
        toast.error(result.error || t('workflow.invalid'))
      }
    } catch {
      toast.error(t('workflow.invalid'))
    } finally {
      setValidating(false)
    }
  }, [validateWorkflow, t])

  // Compute entry nodes
  const entryNodes = activeWorkflowGraph.nodes
    .filter(n => !activeWorkflowGraph.edges.some(e => e.target === n.id))
    .map(n => n.id)

  return (
    <div className="flex flex-1 min-h-0 overflow-hidden">
      {/* Left: YAML Editor */}
      <div className="flex-1 flex flex-col min-w-0 border-r border-border/40">
        {/* Editor header */}
        <div className="shrink-0 flex items-center justify-between px-4 py-2 border-b border-border/40 bg-muted/10">
          <div className="flex items-center gap-2">
            <span className="text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono">
              {t('workflow.yamlEditor')}
            </span>
            <span className="text-[9px] text-muted-foreground/60 font-mono">
              {t('common.lineCount', { count: lineCount })}
            </span>
          </div>
          <div className="flex items-center gap-2">
            {activeWorkflowValidationError ? (
              <span className="text-[10px] text-rose-500 font-mono flex items-center gap-1">
                <AlertCircle className="h-3 w-3" />
                {t('workflow.invalid')}
              </span>
            ) : (
              <span className="text-[10px] text-success font-mono flex items-center gap-1">
                <CheckCircle2 className="h-3 w-3" />
                {t('workflow.valid')}
              </span>
            )}
            <button
              type="button"
              onClick={handleValidate}
              disabled={validating}
              className="flex items-center gap-1 rounded-md border border-border/60 px-2.5 py-1 text-[10px] text-muted-foreground hover:text-foreground hover:bg-muted/40 transition-colors disabled:opacity-40 font-mono"
            >
              {validating ? (
                <Loader2 className="h-3 w-3 animate-spin" />
              ) : (
                <CheckCircle2 className="h-3 w-3" />
              )}
              {t('workflow.validate')}
            </button>
          </div>
        </div>

        {/* Editor body */}
        <div className="flex-1 overflow-hidden">
          <Textarea
            value={activeWorkflowYAML}
            onChange={(e) => handleChange(e.target.value)}
            containerClassName="h-full min-h-0"
            className="h-full min-h-0 w-full resize-none font-mono text-xs rounded-none border-0 bg-background p-4"
            style={{ tabSize: 2 }}
            spellCheck={false}
            placeholder={`name: example
description: ""
version: "1"
...`}
          />
        </div>
      </div>

      {/* Right: DAG Preview */}
      <div className="w-96 shrink-0 flex flex-col bg-muted/5">
        {/* Preview header */}
        <div className="shrink-0 flex items-center justify-between px-4 py-2 border-b border-border/40">
          <span className="text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono">
            {t('workflow.dagPreview')}
          </span>
          <span className="text-[9px] text-muted-foreground/60 font-mono">
            {t('workflow.readOnly')}
          </span>
        </div>

        {/* DAG Graph */}
        <div className="flex-1 min-h-0">
          {activeWorkflowGraph.nodes.length > 0 ? (
            <DAGPreview
              nodes={activeWorkflowGraph.nodes}
              edges={activeWorkflowGraph.edges}
              entryNodes={entryNodes}
            />
          ) : (
            <div className="flex items-center justify-center h-full">
              <p className="text-xs text-muted-foreground">{t('workflow.noWorkflowsYet')}</p>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

// ─── Mode Toggle ────────────────────────────────────────────────────────

function ModeToggle({
  mode,
  onChange,
}: {
  mode: 'visual' | 'yaml'
  onChange: (mode: 'visual' | 'yaml') => void
}) {
  const { t } = useTranslation()

  return (
    <div className="inline-flex rounded-lg bg-muted p-0.5">
      <button
        type="button"
        onClick={() => onChange('visual')}
        className={cn(
          'rounded-md px-3 py-1.5 text-xs font-medium transition-all',
          mode === 'visual'
            ? 'bg-card text-foreground shadow-sm'
            : 'text-muted-foreground hover:text-foreground'
        )}
      >
        {t('workflow.modeVisual')}
      </button>
      <button
        type="button"
        onClick={() => onChange('yaml')}
        className={cn(
          'rounded-md px-3 py-1.5 text-xs font-medium transition-all',
          mode === 'yaml'
            ? 'bg-card text-foreground shadow-sm'
            : 'text-muted-foreground hover:text-foreground'
        )}
      >
        {t('workflow.modeYAML')}
      </button>
    </div>
  )
}

// ─── WorkflowEditorPage ─────────────────────────────────────────────────

export function WorkflowEditorPage() {
  const { name } = useParams<{ name: string }>()
  const navigate = useNavigate()
  const { t } = useTranslation()
  const connectionMode = useConnectionStore((state) => state.mode)
  const backendRunning = useConnectionStore((state) => state.backendStatus.running)
  const sidebarCollapsed = useRuntimeStore((state) => state.sidebarCollapsed)
  const {
    activeWorkflowGraph,
    activeWorkflowAgents,
    activeWorkflowLoading,
    activeWorkflowLoadError,
    availableAgents,
    availableAgentsLoading,
    availableAgentsError,
    editorMode,
    fetchAvailableAgents,
    setActiveWorkflow,
    setEditorMode,
    updateWorkflow,
    startRun,
  } = useWorkflowStore()

  const [saving, setSaving] = useState(false)
  const [running, setRunning] = useState(false)
  const waitingForBackend = shouldWaitForWorkflowBackend(isElectron, connectionMode, backendRunning)

  // Load workflow on mount
  useEffect(() => {
    if (name && !waitingForBackend) {
      void setActiveWorkflow(name).catch(() => {
        // The store exposes a recoverable editor error state.
      })
      void fetchAvailableAgents()
    }
  }, [name, waitingForBackend, setActiveWorkflow, fetchAvailableAgents])

  const invalidAgentTemplates = unknownAgentTemplates(activeWorkflowAgents, availableAgents)
  const agentDataBlocked = availableAgentsLoading
    || !!availableAgentsError
    || invalidAgentTemplates.length > 0

  const nodeCount = activeWorkflowGraph.nodes.length
  const edgeCount = activeWorkflowGraph.edges.length
  const agentCount = Object.keys(
    activeWorkflowGraph.nodes.reduce<Record<string, boolean>>((acc, n) => {
      if (n.agent) acc[n.agent] = true
      return acc
    }, {})
  ).length

  const handleSave = async () => {
    if (!name) return
    if (agentDataBlocked) {
      toast.error(t('workflow.unknownAgents', { names: invalidAgentTemplates.join(', ') || '—' }))
      return
    }
    setSaving(true)
    try {
      const success = await updateWorkflow(name)
      if (success) {
        toast.success(t('workflow.saved'))
      } else {
        toast.error(t('workflow.saveFailed'))
      }
    } catch (err: any) {
      toast.error(err.message || t('workflow.saveFailed'))
    } finally {
      setSaving(false)
    }
  }

  const handleRun = async () => {
    if (!name) return
    if (agentDataBlocked) {
      toast.error(t('workflow.unknownAgents', { names: invalidAgentTemplates.join(', ') || '—' }))
      return
    }
	const goal = window.prompt('Workflow task goal')?.trim() || ''
	if (!goal) {
		toast.error('A task goal is required')
		return
	}
	const criteria = (window.prompt('Acceptance criteria (one per line)') || '')
		.split('\n')
		.map(value => value.trim())
		.filter(Boolean)
	if (criteria.length === 0) {
		toast.error('At least one acceptance criterion is required')
		return
	}
    setRunning(true)
    try {
      const runId = await startRun(name, { goal, acceptance_criteria: criteria })
      if (runId) {
        toast.success(t('workflow.runStarted'))
        navigate(`/workflows/${name}/runs/${runId}`)
      } else {
        toast.error(t('workflow.runFailed'))
      }
    } catch (err: any) {
      toast.error(err.message || t('workflow.runFailed'))
    } finally {
      setRunning(false)
    }
  }

  const retryLoad = () => {
    if (!name || waitingForBackend) return
    void setActiveWorkflow(name).catch(() => {})
    void fetchAvailableAgents()
  }

  return (
    <div className="flex h-full flex-col bg-background text-foreground overflow-hidden">
      {/* Header */}
      <header className={cn(
        'shrink-0 flex items-center justify-between border-b border-border/60 bg-card/20 px-4 py-2.5',
        sidebarCollapsed && 'pl-[115px]'
      )}>
        <div className="flex items-center gap-4 min-w-0">
          {/* Back button */}
          <button
            type="button"
            onClick={() => navigate('/workflows')}
            className="rounded-lg p-1.5 text-muted-foreground hover:text-foreground hover:bg-muted/40 transition-colors shrink-0 electron-no-drag"
          >
            <ArrowLeft className="h-4 w-4" />
          </button>

          {/* Name + meta */}
          <div className="min-w-0">
            <h1 className="text-sm font-bold text-foreground truncate">
              {name || t('workflow.title')}
            </h1>
            <div className="flex items-center gap-3 text-[10px] text-muted-foreground font-mono mt-0.5">
              <span>{t('workflow.nodeCount', { count: nodeCount })}</span>
              <span>{t('workflow.edgeCount', { count: edgeCount })}</span>
              <span>{t('workflow.agents', { count: agentCount })}</span>
            </div>
          </div>
        </div>

        <div className="flex items-center gap-3 electron-no-drag">
          {/* Mode toggle */}
          <ModeToggle mode={editorMode} onChange={setEditorMode} />

          {/* Actions */}
          <button
            type="button"
            onClick={handleRun}
            disabled={waitingForBackend || running || activeWorkflowLoading || !!activeWorkflowLoadError || agentDataBlocked}
            className="flex items-center gap-1.5 rounded-lg bg-success hover:bg-success/90 disabled:bg-success/40 px-3 py-2 text-xs font-semibold text-success-foreground transition-all shadow-sm shadow-success/20 cursor-pointer disabled:cursor-not-allowed"
          >
            {running ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Play className="h-3.5 w-3.5" />
            )}
            {t('workflow.run')}
          </button>

          <button
            type="button"
            onClick={handleSave}
            disabled={waitingForBackend || saving || activeWorkflowLoading || !!activeWorkflowLoadError || agentDataBlocked}
            className="flex items-center gap-1.5 rounded-lg bg-primary hover:bg-primary/90 disabled:bg-primary/40 px-3 py-2 text-xs font-semibold text-primary-foreground transition-all shadow-sm cursor-pointer disabled:cursor-not-allowed"
          >
            {saving ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Save className="h-3.5 w-3.5" />
            )}
            {t('workflow.save')}
          </button>
        </div>
      </header>

      {!activeWorkflowLoading && !activeWorkflowLoadError && agentDataBlocked && (
        <div className="shrink-0 border-b border-rose-500/20 bg-rose-500/8 px-4 py-2 text-[11px] text-rose-500">
          <div className="flex items-center gap-2">
            {availableAgentsLoading ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <AlertCircle className="h-3.5 w-3.5 shrink-0" />
            )}
            <span>
              {availableAgentsLoading
                ? t('workflow.loadingAgents')
                : availableAgentsError
                  ? t('workflow.agentsLoadFailed')
                  : t('workflow.unknownAgents', { names: invalidAgentTemplates.join(', ') })}
            </span>
            {availableAgentsError && (
              <button
                type="button"
                onClick={() => void fetchAvailableAgents()}
                disabled={availableAgentsLoading}
                className="ml-auto shrink-0 rounded-md border border-rose-500/30 px-2 py-1 text-[10px] font-medium text-rose-500 hover:bg-rose-500/10 disabled:opacity-50"
              >
                {t('common.retry')}
              </button>
            )}
          </div>
        </div>
      )}

      {/* Body — loading and errors must never masquerade as an empty editor. */}
      {waitingForBackend || activeWorkflowLoading ? (
        <div className="flex flex-1 items-center justify-center bg-background">
          <div className="flex items-center gap-2 text-xs text-muted-foreground font-mono">
            <Loader2 className="h-4 w-4 animate-spin" />
            {t('common.loading')}
          </div>
        </div>
      ) : activeWorkflowLoadError ? (
        <div className="flex flex-1 items-center justify-center bg-background p-6">
          <div className="max-w-md rounded-xl border border-rose-500/25 bg-rose-500/5 p-5 text-center shadow-sm">
            <AlertCircle className="mx-auto h-6 w-6 text-rose-500" />
            <p className="mt-3 text-sm font-semibold text-foreground">{t('workflow.loadFailed')}</p>
            <p className="mt-1 text-xs text-muted-foreground break-words">{activeWorkflowLoadError}</p>
            <div className="mt-4 flex justify-center gap-2">
              <button
                type="button"
                onClick={() => navigate('/workflows')}
                className="rounded-lg border border-border/60 px-3 py-2 text-xs text-muted-foreground hover:bg-muted/50"
              >
                {t('common.back')}
              </button>
              <button
                type="button"
                onClick={retryLoad}
                className="flex items-center gap-1.5 rounded-lg bg-primary px-3 py-2 text-xs font-semibold text-primary-foreground hover:bg-primary/90"
              >
                <RefreshCw className="h-3.5 w-3.5" />
                {t('common.retry')}
              </button>
            </div>
          </div>
        </div>
      ) : editorMode === 'visual' ? (
        <ReactFlowProvider>
          <VisualDAGEditor />
        </ReactFlowProvider>
      ) : (
        <YAMLEditorView />
      )}
    </div>
  )
}
