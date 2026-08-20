import { useState } from 'react'
import { Trash2, Plus, X } from 'lucide-react'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import type { AgentResponse, GraphNode, OutputDef } from '@/types'
import { useTranslation } from '@/lib/i18n'

// ─── Props ──────────────────────────────────────────────────────────────

interface NodePropertyPanelProps {
  node: GraphNode
  agentRefs: Record<string, { template: string; model?: string }>
  availableAgents: AgentResponse[]
  isEntry: boolean
  onUpdate: (updates: Partial<GraphNode>) => void
  onAgentChange: (template: string) => void
  onToggleEntry: () => void
  onDelete: () => void
}

// ─── Component ──────────────────────────────────────────────────────────

export function NodePropertyPanel({
  node,
  agentRefs,
  availableAgents,
  isEntry,
  onUpdate,
  onAgentChange,
  onToggleEntry,
  onDelete,
}: NodePropertyPanelProps) {
  const { t } = useTranslation()
  const [newOutcome, setNewOutcome] = useState('')

  const outcomes = node.outputs || {}
  const resolvedAgent = agentRefs[node.agent]?.template || ''
  const yamlMapping = [
    `- id: ${node.id}`,
    `  agent: ${node.agent || '—'}`,
    '  outputs:',
    ...Object.entries(outcomes).flatMap(([outcome, output]) => [
      `    ${outcome}:`,
      `      to: [${output.to.join(', ')}]`,
    ]),
  ].join('\n')

  const handleAddOutcome = () => {
    if (!newOutcome.trim()) return
    onUpdate({
      outputs: { ...outcomes, [newOutcome.trim()]: { to: [], loop: false, max_traversals: 0 } },
    })
    setNewOutcome('')
  }

  const handleRemoveOutcome = (outcome: string) => {
    const updated = { ...outcomes }
    delete updated[outcome]
    onUpdate({ outputs: updated })
  }

  const handleOutcomeChange = (outcome: string, updates: Partial<OutputDef>) => {
    const existing = outcomes[outcome] || { to: [], loop: false, max_traversals: 0 }
    onUpdate({ outputs: { ...outcomes, [outcome]: { ...existing, ...updates } } })
  }

  return (
    <div className="w-80 shrink-0 border-l border-border/40 bg-card/10 flex flex-col h-full overflow-hidden">
      {/* Header */}
      <div className="shrink-0 flex items-center justify-between px-4 py-3 border-b border-border/40">
        <div>
          <h3 className="text-xs font-semibold text-foreground font-mono">
            {t('workflow.nodeId')}: {node.id}
          </h3>
        </div>
        <button
          type="button"
          onClick={onDelete}
          className="rounded-lg p-1 text-muted-foreground hover:text-rose-500 hover:bg-rose-500/10 transition-colors"
          title={t('common.delete')}
        >
          <Trash2 className="h-3.5 w-3.5" />
        </button>
      </div>

      {/* Body */}
      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {/* ID */}
        <div>
          <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-1">
            ID
          </label>
          <Input
            value={node.id}
            onChange={(e) => onUpdate({ id: e.target.value })}
            className="font-mono text-xs"
          />
        </div>

        {/* Agent */}
        <div>
          <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-1">
            {t('workflow.agentRef')}
          </label>
          <select
            value={resolvedAgent}
            onChange={(e) => onAgentChange(e.target.value)}
            className="flex h-9 w-full rounded-lg border border-input bg-card px-3 py-1 text-xs text-foreground shadow-sm transition-colors font-mono"
          >
            {!resolvedAgent && <option value="">Invalid agent reference</option>}
            {availableAgents.map((agent) => (
              <option key={agent.id} value={agent.id}>{agent.name}</option>
            ))}
          </select>
          <p className="mt-1 text-[9px] text-muted-foreground">
            YAML: <code>agents.{node.agent || '—'}.template: {resolvedAgent || '—'}</code>
          </p>
        </div>

        {/* Entry is an execution semantic, not an inferred graph decoration. */}
        <label className="flex items-center gap-2 rounded-lg border border-border/40 bg-muted/20 px-3 py-2 cursor-pointer">
          <input
            type="checkbox"
            checked={isEntry}
            onChange={onToggleEntry}
            className="h-3.5 w-3.5 rounded accent-primary"
          />
          <span className="text-[10px] font-mono text-foreground">{t('workflow.entryNode')}</span>
        </label>

        {/* Prompt */}
        <div>
          <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-1">
            {t('workflow.prompt')}
          </label>
          <Textarea
            rows={4}
            value={node.prompt}
            onChange={(e) => onUpdate({ prompt: e.target.value })}
            className="resize-none font-sans text-xs"
          />
        </div>

        {/* Timeout */}
        <div>
          <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-1">
            {t('workflow.timeout')}
          </label>
          <select
            value={node.timeout || ''}
            onChange={(e) => onUpdate({ timeout: e.target.value || undefined })}
            className="flex h-9 w-full rounded-lg border border-input bg-card px-3 py-1 text-xs text-foreground shadow-sm transition-colors font-mono"
          >
            <option value="">Default (20m)</option>
            <option value="30s">30s</option>
            <option value="1m">1m</option>
            <option value="5m">5m</option>
            <option value="10m">10m</option>
            <option value="20m">20m</option>
            <option value="30m">30m</option>
          </select>
        </div>

        {/* Divider */}
        <div className="border-t border-border/40" />

        <div>
          <label className="mb-2 block text-[10px] font-bold uppercase tracking-wider text-muted-foreground">
            {t('workflow.yamlMapping')}
          </label>
          <pre className="overflow-x-auto whitespace-pre rounded-lg border border-border/50 bg-muted/25 p-3 font-mono text-[9px] leading-relaxed text-muted-foreground">
            {yamlMapping}
          </pre>
        </div>

        <div className="border-t border-border/40" />

        {/* Outputs */}
        <div>
          <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-2">
            {t('workflow.outputs')}
          </label>

          <div className="space-y-3">
            {Object.entries(outcomes).map(([outcome, output]) => (
              <div
                key={outcome}
                className="rounded-lg border border-border/40 bg-card/50 p-3 space-y-2"
              >
                <div className="flex items-center justify-between">
                  <span className="text-[10px] font-bold text-foreground font-mono">{outcome}</span>
                  <button
                    type="button"
                    onClick={() => handleRemoveOutcome(outcome)}
                    className="p-0.5 text-muted-foreground hover:text-rose-500 transition-colors"
                  >
                    <X className="h-3 w-3" />
                  </button>
                </div>

                {/* Target nodes */}
                <div>
                  <label className="text-[9px] text-muted-foreground font-mono">
                    {t('workflow.targetNodes')}
                  </label>
                  <Input
                    value={output.to.join(', ')}
                    onChange={(e) => {
                      const targets = e.target.value.split(',').map(s => s.trim()).filter(Boolean)
                      handleOutcomeChange(outcome, { to: targets })
                    }}
                    placeholder="node1, node2"
                    className="font-mono text-[10px] h-7 mt-0.5"
                  />
                  {output.to.length === 0 && (
                    <span className="text-[9px] text-muted-foreground/60 font-mono">
                      ({t('workflow.terminalOutput')})
                    </span>
                  )}
                </div>

                {/* Loop toggle */}
                <div className="flex items-center gap-2">
                  <label className="flex items-center gap-1.5 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={output.loop}
                      onChange={(e) =>
                        handleOutcomeChange(outcome, { loop: e.target.checked })
                      }
                      className="h-3 w-3 rounded accent-primary"
                    />
                    <span className="text-[9px] text-muted-foreground font-mono">{t('workflow.loop')}</span>
                  </label>
                  {output.loop && (
                    <Input
                      type="number"
                      value={output.max_traversals || 1}
                      onChange={(e) =>
                        handleOutcomeChange(outcome, { max_traversals: parseInt(e.target.value) || 1 })
                      }
                      className="font-mono text-[10px] h-6 w-16"
                      placeholder="max"
                    />
                  )}
                </div>
              </div>
            ))}
          </div>

          {/* Add outcome */}
          <div className="flex items-center gap-1.5 mt-2">
            <Input
              value={newOutcome}
              onChange={(e) => setNewOutcome(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); handleAddOutcome() } }}
              placeholder={t('workflow.outcomePlaceholder')}
              className="font-mono text-[10px] h-7"
            />
            <button
              type="button"
              onClick={handleAddOutcome}
              disabled={!newOutcome.trim()}
              className="flex items-center gap-1 rounded-lg border border-border/60 px-2.5 py-1 text-[10px] text-muted-foreground hover:text-foreground hover:bg-muted/40 transition-colors disabled:opacity-40 shrink-0"
            >
              <Plus className="h-3 w-3" />
            </button>
          </div>
        </div>

        {/* Divider */}
        <div className="border-t border-border/40" />

        {/* Join (Fan-in) */}
        <div>
          <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-1">
            {t('workflow.joinMode')}
          </label>
          <div className="flex items-center gap-2 mb-2">
            <label className="flex items-center gap-1.5 cursor-pointer">
              <input
                type="checkbox"
                checked={!!node.join}
                onChange={(e) => {
                  if (e.target.checked) {
                    onUpdate({ join: { mode: 'all', from: [] } })
                  } else {
                    onUpdate({ join: undefined })
                  }
                }}
                className="h-3 w-3 rounded accent-primary"
              />
              <span className="text-[9px] text-muted-foreground font-mono">{t('workflow.joinWaitAll')}</span>
            </label>
          </div>
          {node.join && (
            <div>
              <label className="text-[9px] text-muted-foreground font-mono">{t('workflow.joinFrom')}</label>
              <Input
                value={node.join.from.join(', ')}
                onChange={(e) => {
                  const from = e.target.value.split(',').map(s => s.trim()).filter(Boolean)
                  onUpdate({ join: { mode: 'all', from } })
                }}
                placeholder="node1, node2"
                className="font-mono text-[10px] h-7 mt-0.5"
              />
            </div>
          )}
        </div>

        {/* Divider */}
        <div className="border-t border-border/40" />

        {/* On Error */}
        <div>
          <label className="block text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono mb-1">
            {t('workflow.onError')}
          </label>
          <select
            value={node.onError?.strategy || 'fail'}
            onChange={(e) => {
              const strategy = e.target.value as 'fail' | 'retry'
              if (strategy === 'fail') {
                onUpdate({ onError: undefined })
              } else {
                onUpdate({ onError: { strategy: 'retry', max_attempts: node.onError?.max_attempts || 3 } })
              }
            }}
            className="flex h-9 w-full rounded-lg border border-input bg-card px-3 py-1 text-xs text-foreground shadow-sm transition-colors font-mono"
          >
            <option value="fail">{t('workflow.errorFail')}</option>
            <option value="retry">{t('workflow.errorRetry')}</option>
          </select>
          {node.onError?.strategy === 'retry' && (
            <div className="mt-2">
              <label className="text-[9px] text-muted-foreground font-mono">{t('workflow.maxAttempts')}</label>
              <Input
                type="number"
                value={node.onError.max_attempts}
                onChange={(e) =>
                  onUpdate({ onError: { strategy: 'retry', max_attempts: parseInt(e.target.value) || 2 } })
                }
                className="font-mono text-[10px] h-7 mt-0.5 w-20"
                min={2}
              />
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
