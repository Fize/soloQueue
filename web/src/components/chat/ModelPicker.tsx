import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Check, ChevronDown } from 'lucide-react'
import { toast } from 'sonner'
import { listModels, listProviders } from '@/lib/api'
import type { LLMModel, LLMProvider } from '@/types'
import type { SessionModelSelection, SessionModelState, SessionModelView } from '@/types/chat'
import { cn, getModelColorVar } from '@/lib/utils'
import { useTranslation } from '@/lib/i18n'

const PANEL_WIDTH = 320
const PANEL_MAX_HEIGHT = 336
const PANEL_MIN_HEIGHT = 128
const ANCHOR_GAP = 6
const VIEWPORT_MARGIN = 12

// Effort values are API parameters; render the vocabulary verbatim so the UI
// and the configured capability list always use the same names.
const EFFORT_DISPLAY: Record<string, string> = {
  low: 'Low',
  medium: 'Medium',
  high: 'High',
  xhigh: 'XHigh',
  max: 'Max',
}

export type ComposerTier = 'A' | 'B' | 'C'

const NAME_WIDTH: Record<ComposerTier, number> = { A: 128, B: 96, C: 64 }

export interface ModelPickerProps {
  /** Server-reported state for the active session; null while loading. */
  state: SessionModelState | null
  /** True while a PUT is in flight. */
  saving?: boolean
  /** The composer cannot change the model (no session, offline). */
  disabled?: boolean
  /** Composer width tier; narrows the trigger and hides the effort text. */
  tier?: ComposerTier
  /** Resolves false when the server rejected the change. */
  onSelect: (selection: SessionModelSelection) => Promise<boolean> | boolean | void
}

function effortLabel(value: string): string {
  return EFFORT_DISPLAY[value] ?? value
}

/**
 * Model + thinking effort for the composer meta row. One hot area because both
 * answer the same question, but the panel keeps the model list and the effort
 * radios as siblings: a listbox may not own a second composite widget.
 */
export function ModelPicker({
  state,
  saving = false,
  disabled = false,
  tier = 'A',
  onSelect,
}: ModelPickerProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [models, setModels] = useState<LLMModel[]>([])
  const [providers, setProviders] = useState<LLMProvider[]>([])
  const [loading, setLoading] = useState(false)
  const [loaded, setLoaded] = useState(false)
  const [pos, setPos] = useState<{ top?: number; bottom?: number; left: number; maxHeight: number } | null>(null)
  const [activeIndex, setActiveIndex] = useState(0)
  const [receipt, setReceipt] = useState('')
  const [live, setLive] = useState('')
  const triggerRef = useRef<HTMLButtonElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const pendingInitialIndex = useRef(false)
  const typeahead = useRef({ buffer: '', at: 0 })

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

  // The selectable models in visual order, so arrow keys walk the same list the
  // eye does. Index 0 is the "auto" row that clears the session override.
  const options = useMemo(() => groups.flatMap(([, providerModels]) => providerModels), [groups])
  const optionCount = options.length + 1

  const effective = state?.effective
  const selection = state?.selection
  const selectedRef = selection ? `${selection.provider_id}:${selection.model_id}` : ''
  const effort = selection?.reasoning_effort ?? ''
  const triggerLabel = effective ? effective.name || effective.model_id : t('chat.modelSelectorAuto')

  const viewFor = useCallback(
    (model: LLMModel): SessionModelView | null => {
      const ref = `${model.providerId}:${model.id}`
      if (selection && `${selection.provider_id}:${selection.model_id}` === ref) return selection
      if (effective && `${effective.provider_id}:${effective.model_id}` === ref) return effective
      return null
    },
    [selection, effective],
  )

  // A model declares its capabilities; `thinking.enabled = false` means none.
  const effortsFor = useCallback((model: LLMModel | undefined): string[] => {
    if (!model) return []
    // `thinking.enabled = false` means no capability, whatever an older
    // selection still carries.
    if (!model.thinking?.enabled) return []
    const view = viewFor(model)
    if (view?.reasoning_efforts?.length) return view.reasoning_efforts
    return model.thinking.reasoningEfforts ?? []
  }, [viewFor])

  // The effort belongs to the session's model, never to the row the pointer is
  // hovering: model and effort are two independent choices.
  const currentModel = useMemo(() => {
    const ref = selection
      ? `${selection.provider_id}:${selection.model_id}`
      : effective
        ? `${effective.provider_id}:${effective.model_id}`
        : ''
    if (!ref) return undefined
    return options.find((model) => `${model.providerId}:${model.id}` === ref)
  }, [effective, options, selection])
  const currentEfforts = effortsFor(currentModel)
  const showEfforts = currentEfforts.length >= 2
  const activeEffort = currentEfforts.includes(effort) ? effort : ''

  const place = useCallback(() => {
    const rect = triggerRef.current?.getBoundingClientRect()
    if (!rect) return
    const vh = window.innerHeight
    const vw = window.innerWidth
    const spaceBelow = vh - rect.bottom - ANCHOR_GAP - VIEWPORT_MARGIN
    const spaceAbove = rect.top - ANCHOR_GAP - VIEWPORT_MARGIN
    // Open below by default and flip only when above has more room, so the
    // panel always hugs the trigger instead of floating away from it.
    const openDown = spaceBelow >= spaceAbove
    const available = Math.max(PANEL_MIN_HEIGHT, openDown ? spaceBelow : spaceAbove)
    const left = Math.min(
      Math.max(VIEWPORT_MARGIN, rect.right - PANEL_WIDTH),
      Math.max(VIEWPORT_MARGIN, vw - PANEL_WIDTH - VIEWPORT_MARGIN),
    )
    setPos({
      top: openDown ? rect.bottom + ANCHOR_GAP : undefined,
      bottom: openDown ? undefined : vh - rect.top + ANCHOR_GAP,
      left,
      maxHeight: Math.min(PANEL_MAX_HEIGHT, available),
    })
  }, [])

  const close = useCallback(() => {
    setOpen(false)
    setReceipt('')
    triggerRef.current?.focus()
  }, [])

  const toggle = useCallback(() => {
    if (disabled) return
    if (open) {
      close()
      return
    }
    place()
    loadModels()
    pendingInitialIndex.current = true
    setReceipt('')
    setOpen(true)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [disabled, open, close, place])

  useEffect(() => {
    if (!open) return
    const update = () => place()
    window.addEventListener('resize', update)
    window.addEventListener('scroll', update, true)
    return () => {
      window.removeEventListener('resize', update)
      window.removeEventListener('scroll', update, true)
    }
  }, [open, place])

  useEffect(() => {
    if (!open) return
    listRef.current?.focus()
  }, [open])

  // A pointer outside the panel or its trigger dismisses it: relying on the
  // toggle alone leaves the panel stranded open.
  useEffect(() => {
    if (!open) return
    function onPointerDown(event: PointerEvent) {
      const target = event.target as Node | null
      if (!target) return
      if (panelRef.current?.contains(target)) return
      if (triggerRef.current?.contains(target)) return
      setOpen(false)
      setReceipt('')
    }
    document.addEventListener('pointerdown', onPointerDown, true)
    return () => document.removeEventListener('pointerdown', onPointerDown, true)
  }, [open])

  // Start on the selected model once the catalog is available; a click that
  // happens before the load keeps the caller's index.
  useEffect(() => {
    if (!open || loading || !pendingInitialIndex.current) return
    pendingInitialIndex.current = false
    const index = selectedRef ? options.findIndex((m) => `${m.providerId}:${m.id}` === selectedRef) : -1
    setActiveIndex(index >= 0 ? index + 1 : 0)
  }, [open, loading, options, selectedRef])

  useEffect(() => {
    if (!open) return
    const el = document.getElementById(`model-option-${activeIndex}`)
    if (typeof el?.scrollIntoView === 'function') el.scrollIntoView({ block: 'nearest' })
  }, [open, activeIndex])

  // ⌘⇧K opens the picker from anywhere in the composer; pressing it again closes.
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (!(e.metaKey || e.ctrlKey) || !e.shiftKey || e.key.toLowerCase() !== 'k') return
      e.preventDefault()
      toggle()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [toggle])

  async function commit(next: SessionModelSelection) {
    const accepted = await onSelect(next)
    if (accepted === false) {
      // The server rejected the change; the state prop still holds its value.
      setLive(t('chat.modelSelectorSaveFailed'))
      toast.error(t('chat.modelSelectorSaveFailed'))
      return
    }
    setLive('')
  }

  function selectModel(model: LLMModel) {
    const supported = effortsFor(model)
    const keep = effort && supported.includes(effort) ? effort : ''
    setReceipt(effort && !keep ? t('chat.modelSelectorEffortCleared', { effort: effortLabel(effort) }) : '')
    void commit({ provider_id: model.providerId, model_id: model.id, reasoning_effort: keep })
  }

  function selectEffort(next: string) {
    const providerId = currentModel?.providerId ?? (selection?.provider_id || effective?.provider_id || '')
    const modelId = currentModel?.id ?? (selection?.model_id || effective?.model_id || '')
    setReceipt('')
    void commit({ provider_id: providerId, model_id: modelId, reasoning_effort: next })
  }

  function activate(index: number) {
    const model = index === 0 ? undefined : options[index - 1]
    if (!model) {
      setReceipt('')
      void commit({ provider_id: '', model_id: '', reasoning_effort: '' })
      return
    }
    selectModel(model)
  }

  function move(delta: number) {
    setActiveIndex((prev) => Math.min(optionCount - 1, Math.max(0, prev + delta)))
  }

  function onTypeahead(char: string) {
    const now = Date.now()
    const buffer = now - typeahead.current.at > 500 ? char : typeahead.current.buffer + char
    typeahead.current = { buffer, at: now }
    const needle = buffer.toLowerCase()
    const labels = options.map((model) => (model.name || model.id).toLowerCase())
    const start = (activeIndex + 1) % optionCount
    for (let step = 0; step < optionCount; step++) {
      const index = (start + step) % optionCount
      const label = index === 0 ? t('chat.modelSelectorAuto').toLowerCase() : labels[index - 1]
      if (label.startsWith(needle)) {
        setActiveIndex(index)
        return
      }
    }
  }

  function onListKeyDown(e: React.KeyboardEvent<HTMLDivElement>) {
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        move(1)
        return
      case 'ArrowUp':
        e.preventDefault()
        move(-1)
        return
      case 'Home':
        e.preventDefault()
        setActiveIndex(0)
        return
      case 'End':
        e.preventDefault()
        setActiveIndex(optionCount - 1)
        return
      case 'PageDown':
        e.preventDefault()
        move(5)
        return
      case 'PageUp':
        e.preventDefault()
        move(-5)
        return
      case 'Enter':
      case ' ':
        e.preventDefault()
        activate(activeIndex)
        return
      case 'Escape':
        e.preventDefault()
        e.stopPropagation()
        close()
        return
      case 'Tab':
        return
      default:
        if (e.key.length === 1 && !e.metaKey && !e.ctrlKey && !e.altKey) onTypeahead(e.key)
    }
  }

  function onRadioKeyDown(e: React.KeyboardEvent<HTMLDivElement>) {
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      close()
      return
    }
    if (e.key === 'Tab') return
    const step = e.key === 'ArrowRight' || e.key === 'ArrowDown' ? 1 : e.key === 'ArrowLeft' || e.key === 'ArrowUp' ? -1 : 0
    const values = currentEfforts
    const current = values.indexOf(activeEffort)
    let next = -1
    if (step !== 0) next = (current + step + values.length) % values.length
    else if (e.key === 'Home') next = 0
    else if (e.key === 'End') next = values.length - 1
    if (next < 0) return
    e.preventDefault()
    selectEffort(values[next])
  }

  const nameWidth = NAME_WIDTH[tier]
  const showDot = tier !== 'C'
  const showEffortText = tier === 'A' && !!effort

  return (
    <div className="relative shrink-0">
      <button
        ref={triggerRef}
        type="button"
        onClick={toggle}
        disabled={disabled}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls="model-panel"
        aria-label={t('chat.modelSelectorTitle')}
        title={t('chat.modelSelectorTitle')}
        className={cn(
          'group/model relative flex h-5 items-center gap-1 rounded-[4px] px-1 text-[12px] font-medium leading-none text-[#C2C3C7] transition-colors',
          `after:absolute after:inset-x-[-2px] after:top-0 after:bottom-[-4px] after:content-['']`,
          disabled
            ? 'cursor-default opacity-50'
            : 'cursor-pointer hover:bg-white/[0.04] hover:text-[#EDEDF0] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#8AB4FF]',
          saving && 'opacity-60',
        )}
      >
        {showDot && (
          <span
            aria-hidden="true"
            className="h-1.5 w-1.5 shrink-0 rounded-full"
            style={{ background: getModelColorVar(effective?.provider_id || effective?.model_id || 'default') }}
          />
        )}
        <span className="truncate" style={{ maxWidth: `${nameWidth}px` }}>
          {triggerLabel}
        </span>
        {showEffortText && (
          <>
            <span aria-hidden="true" className="shrink-0 text-[11px] leading-none text-[rgba(156,163,175,0.4)]">
              ·
            </span>
            <span className="shrink-0 truncate text-[11px] leading-none text-[#9CA3AF]" style={{ maxWidth: '48px' }}>
              {effortLabel(effort)}
            </span>
          </>
        )}
        <ChevronDown
          aria-hidden="true"
          className="h-2.5 w-2.5 shrink-0 opacity-0 transition-opacity group-hover/model:opacity-60 group-focus-visible/model:opacity-60"
        />
      </button>

      {open &&
        pos &&
        createPortal(
          <div
            id="model-panel"
            ref={panelRef}
            role="dialog"
            aria-modal="false"
            aria-labelledby={showEfforts ? 'model-panel-models-title model-panel-efforts-title' : 'model-panel-models-title'}
            data-model-picker
            className="fixed z-50 flex flex-col rounded-xl border border-[#2E2F38] bg-popover p-1 shadow-lg"
            style={{
              width: `${PANEL_WIDTH}px`,
              left: `${pos.left}px`,
              top: pos.top !== undefined ? `${pos.top}px` : undefined,
              bottom: pos.bottom !== undefined ? `${pos.bottom}px` : undefined,
              maxHeight: `${pos.maxHeight}px`,
            }}
          >
            <div
              id="model-panel-models-title"
              className="flex h-6 shrink-0 items-center px-2 text-[10px] font-semibold uppercase leading-6 tracking-[0.04em] text-muted-foreground"
            >
              {t('chat.modelSelectorTitle')}
            </div>

            <div
              ref={listRef}
              role="listbox"
              aria-labelledby="model-panel-models-title"
              aria-activedescendant={`model-option-${activeIndex}`}
              tabIndex={0}
              onKeyDown={onListKeyDown}
              className="hig-menu-scroll min-h-0 flex-1 overflow-y-auto focus:outline-none"
            >
              <div
                id="model-option-0"
                role="option"
                aria-selected={!selectedRef}
                onClick={() => {
                  setActiveIndex(0)
                  activate(0)
                }}
                onMouseEnter={() => setActiveIndex(0)}
                className={cn(
                  'flex h-8 cursor-pointer items-center justify-between rounded-lg px-2 text-[12px] font-semibold leading-4 text-foreground transition-colors max-sm:h-[38px]',
                  activeIndex === 0 && 'bg-white/[0.06]',
                )}
              >
                <span className="truncate">{t('chat.modelSelectorAuto')}</span>
                {!selectedRef && <Check className="h-3 w-3 shrink-0 text-primary" aria-hidden="true" />}
              </div>

              <div role="presentation" className="my-1 h-px bg-[rgba(46,47,56,0.6)]" />

              {loading && (
                <p className="px-2 py-2 text-[11px] text-muted-foreground">{t('chat.modelSelectorLoading')}</p>
              )}
              {!loading && groups.length === 0 && (
                <p className="px-2 py-2 text-[11px] text-muted-foreground">{t('chat.modelSelectorEmpty')}</p>
              )}

              {groups.map(([providerId, providerModels]) => (
                <div key={providerId} role="group" aria-labelledby={`model-group-${providerId}`}>
                  <div
                    id={`model-group-${providerId}`}
                    role="presentation"
                    className="sticky top-0 z-10 flex h-[22px] items-center bg-popover px-2 text-[10px] font-semibold uppercase leading-[22px] tracking-[0.04em] text-muted-foreground"
                  >
                    {providers.find((p) => p.id === providerId)?.name || providerId}
                  </div>
                  {providerModels.map((model) => {
                    const index = options.indexOf(model) + 1
                    const ref = `${model.providerId}:${model.id}`
                    return (
                      <div
                        key={ref}
                        id={`model-option-${index}`}
                        role="option"
                        aria-selected={selectedRef === ref}
                        onClick={() => {
                          setActiveIndex(index)
                          selectModel(model)
                        }}
                        onMouseEnter={() => setActiveIndex(index)}
                        className={cn(
                          'flex h-8 cursor-pointer items-center justify-between gap-2 rounded-lg px-2 text-[12px] leading-4 text-foreground transition-colors max-sm:h-[38px]',
                          activeIndex === index && 'bg-white/[0.06]',
                        )}
                      >
                        <span className="min-w-0 flex-1 truncate">{model.name || model.id}</span>
                        <span className="flex shrink-0 items-center gap-1.5 text-[10px] text-muted-foreground">
                          {model.vision && <span aria-hidden="true">👁</span>}
                          {model.contextWindow ? <span>{Math.round(model.contextWindow / 1000)}K</span> : null}
                          {selectedRef === ref && <Check className="h-3 w-3 text-primary" aria-hidden="true" />}
                        </span>
                      </div>
                    )
                  })}
                </div>
              ))}
            </div>

            {showEfforts && (
              <>
                <div role="separator" className="my-1 h-px shrink-0 bg-[rgba(46,47,56,0.6)]" />
                <div
                  id="model-panel-efforts-title"
                  className="flex h-6 shrink-0 items-center px-2 text-[10px] font-semibold uppercase leading-6 tracking-[0.04em] text-muted-foreground"
                >
                  {t('chat.modelSelectorEffortsTitle')}
                </div>
                <div
                  role="radiogroup"
                  aria-labelledby="model-panel-efforts-title"
                  onKeyDown={onRadioKeyDown}
                  className="flex shrink-0 flex-wrap gap-1 px-1 pb-1"
                >
                  {currentEfforts.map((value) => {
                    const checked = activeEffort === value
                    return (
                      <button
                        key={value || 'auto'}
                        type="button"
                        role="radio"
                        aria-checked={checked}
                        tabIndex={checked ? 0 : -1}
                        onClick={() => selectEffort(value)}
                        className={cn(
                          'h-7 shrink-0 whitespace-nowrap rounded-md px-2 text-[12px] font-medium leading-4 transition-colors',
                          checked
                            ? 'bg-primary text-primary-foreground'
                            : 'text-muted-foreground hover:bg-white/[0.06] hover:text-[#EDEDF0]',
                        )}
                      >
                        {effortLabel(value)}
                      </button>
                    )
                  })}
                </div>
              </>
            )}

            <p className="flex h-5 shrink-0 items-center px-2 text-[10px] leading-[14px] text-muted-foreground">
              {receipt || t('chat.modelSelectorNextTurn')}
            </p>
            <p aria-live="polite" className="sr-only">
              {live}
            </p>
          </div>,
          document.body,
        )}
    </div>
  )
}
