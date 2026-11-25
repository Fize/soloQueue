import { useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Check, ChevronDown } from 'lucide-react'
import { listModels, listProviders } from '@/lib/api'
import type { LLMModel, LLMProvider } from '@/types'
import type { SessionModelSelection, SessionModelState } from '@/types/chat'
import { cn, getModelColorVar } from '@/lib/utils'
import { useTranslation } from '@/lib/i18n'

const EFFORTS = ['', 'low', 'high', 'max'] as const

const EFFORT_LABELS: Record<string, string> = {
  '': 'chat.modelSelectorEffortDefault',
  low: 'chat.modelSelectorEffortLow',
  high: 'chat.modelSelectorEffortHigh',
  max: 'chat.modelSelectorEffortMax',
}

export interface ModelMenuProps {
  /** Server-reported state for the active session; null while loading. */
  state: SessionModelState | null
  /** True while a PUT is in flight. */
  saving?: boolean
  /** The composer is disabled (offline or busy). */
  disabled?: boolean
  onSelect: (selection: SessionModelSelection) => void
}

/**
 * Model pill for the composer footer. Shows the model that will run the next
 * turn and opens a popover listing enabled models plus the thinking effort.
 * The selection is session state on the server, so it also applies to channel
 * conversations and survives restarts.
 */
export function ModelMenu({ state, saving = false, disabled = false, onSelect }: ModelMenuProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [models, setModels] = useState<LLMModel[]>([])
  const [providers, setProviders] = useState<LLMProvider[]>([])
  const [loading, setLoading] = useState(false)
  const [loaded, setLoaded] = useState(false)
  const [pos, setPos] = useState<{ bottom: number; right: number } | null>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)

  // Load the catalog on first open so the chat page pays nothing until needed.
  function loadModels() {
    if (loaded || loading) return
    setLoading(true)
    Promise.all([listModels(), listProviders()])
      .then(([nextModels, nextProviders]) => {
        setModels(nextModels || [])
        setProviders(nextProviders || [])
        setLoaded(true)
      })
      .catch(() => setLoaded(true))
      .finally(() => setLoading(false))
  }

  const groups = useMemo(() => {
    const enabledProviders = new Set(providers.filter((p) => p.enabled).map((p) => p.id))
    const byProvider = new Map<string, LLMModel[]>()
    for (const model of models) {
      if (!model.enabled || !enabledProviders.has(model.providerId)) continue
      const list = byProvider.get(model.providerId) ?? []
      list.push(model)
      byProvider.set(model.providerId, list)
    }
    return [...byProvider.entries()]
  }, [models, providers])

  const effective = state?.effective
  const selectedRef = state?.selection
    ? `${state.selection.provider_id}:${state.selection.model_id}`
    : ''
  const effort = state?.selection?.reasoning_effort ?? ''
  const label = effective ? effective.name || effective.model_id : t('chat.modelSelectorAuto')

  const selectedModel = selectedRef
    ? models.find((m) => `${m.providerId}:${m.id}` === selectedRef)
    : undefined
  const thinkingAvailable = Boolean(selectedModel?.thinking?.enabled)

  function toggle() {
    if (disabled) return
    if (open) {
      setOpen(false)
      return
    }
    const rect = triggerRef.current?.getBoundingClientRect()
    if (!rect) return
    setPos({
      bottom: window.innerHeight - rect.top + 8,
      right: Math.max(8, window.innerWidth - rect.right),
    })
    loadModels()
    setOpen(true)
  }

  return (
    <div className="relative shrink-0">
      <button
        ref={triggerRef}
        type="button"
        onClick={toggle}
        disabled={disabled}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label={t('chat.modelSelectorTitle')}
        title={t('chat.modelSelectorTitle')}
        className={cn(
          'flex max-w-[200px] items-center gap-1 rounded-md border border-border/60 px-1.5 py-0.5 text-[11px] font-medium transition-colors',
          disabled
            ? 'cursor-default text-muted-foreground/60'
            : 'cursor-pointer text-foreground/80 hover:bg-muted/40',
          saving && 'opacity-60'
        )}
      >
        <span
          className="h-1.5 w-1.5 shrink-0 rounded-full"
          style={{ background: getModelColorVar(effective?.provider_id || effective?.model_id) }}
        />
        <span className="truncate">{label}</span>
        {effort && (
          <span className="shrink-0 text-muted-foreground/70">
            · {t(EFFORT_LABELS[effort] || EFFORT_LABELS[''])}
          </span>
        )}
        <ChevronDown className="h-2.5 w-2.5 shrink-0 opacity-60" />
      </button>

      {open &&
        pos &&
        createPortal(
          <div
            role="listbox"
            data-model-menu
            className="fixed z-50 w-80 rounded-xl border border-border bg-popover p-1 shadow-lg"
            style={{ bottom: `${pos.bottom}px`, right: `${pos.right}px` }}
          >
            <div className="px-2 py-1.5 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
              {t('chat.modelSelectorTitle')}
            </div>
            <button
              type="button"
              onClick={() => {
                onSelect({ provider_id: '', model_id: '', reasoning_effort: '' })
                setOpen(false)
              }}
              className="flex w-full items-center justify-between rounded-lg px-2 py-1.5 text-left text-xs font-semibold text-foreground transition-colors hover:bg-muted"
            >
              <span>{t('chat.modelSelectorAuto')}</span>
              {!selectedRef && <Check className="h-3 w-3 text-primary" />}
            </button>
            <div className="my-1 h-px bg-border/60" />

            {loading && (
              <p className="px-2 py-2 text-[11px] text-muted-foreground">
                {t('chat.modelSelectorLoading')}
              </p>
            )}
            {!loading && groups.length === 0 && (
              <p className="px-2 py-2 text-[11px] text-muted-foreground">
                {t('chat.modelSelectorEmpty')}
              </p>
            )}
            <div className="max-h-64 overflow-y-auto hig-menu-scroll">
              {groups.map(([providerId, providerModels]) => (
                <div key={providerId}>
                  <div className="px-2 pt-1.5 pb-0.5 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground/80">
                    {providers.find((p) => p.id === providerId)?.name || providerId}
                  </div>
                  {providerModels.map((model) => {
                    const ref = `${model.providerId}:${model.id}`
                    return (
                      <button
                        key={ref}
                        type="button"
                        onClick={() => {
                          onSelect({
                            provider_id: model.providerId,
                            model_id: model.id,
                            reasoning_effort: model.thinking?.enabled ? effort : '',
                          })
                          setOpen(false)
                        }}
                        className="flex w-full items-center justify-between gap-2 rounded-lg px-2 py-1.5 text-left text-xs text-foreground transition-colors hover:bg-muted"
                      >
                        <span className="truncate">{model.name || model.id}</span>
                        <span className="flex shrink-0 items-center gap-1.5 text-[10px] text-muted-foreground">
                          {model.vision && <span aria-hidden="true">👁</span>}
                          {model.contextWindow ? (
                            <span>{Math.round(model.contextWindow / 1000)}K</span>
                          ) : null}
                          {selectedRef === ref && <Check className="h-3 w-3 text-primary" />}
                        </span>
                      </button>
                    )
                  })}
                </div>
              ))}
            </div>

            {thinkingAvailable && (
              <>
                <div className="my-1 h-px bg-border/60" />
                <div className="px-2 pb-1 pt-0.5 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
                  {t('chat.modelSelectorEffort')}
                </div>
                <div className="flex gap-1 px-1 pb-1">
                  {EFFORTS.map((value) => (
                    <button
                      key={value || 'default'}
                      type="button"
                      aria-pressed={effort === value}
                      onClick={() =>
                        onSelect({
                          provider_id: state?.selection?.provider_id || '',
                          model_id: state?.selection?.model_id || '',
                          reasoning_effort: value,
                        })
                      }
                      className={cn(
                        'flex-1 rounded-md px-1.5 py-1 text-[11px] font-medium transition-colors',
                        effort === value
                          ? 'bg-primary/10 text-primary'
                          : 'text-muted-foreground hover:bg-muted'
                      )}
                    >
                      {t(EFFORT_LABELS[value])}
                    </button>
                  ))}
                </div>
              </>
            )}

            <p className="px-2 py-1 text-[10px] text-muted-foreground">
              {t('chat.modelSelectorNextTurn')}
            </p>
          </div>,
          document.body
        )}
    </div>
  )
}
