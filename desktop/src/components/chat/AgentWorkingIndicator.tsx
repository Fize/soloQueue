import { memo } from 'react'
import { Sparkles, Cpu } from 'lucide-react'
import { cn } from '@/lib/utils'

export interface AgentWorkingIndicatorProps {
  /** Display name of the active agent (e.g. "L1 Agent", "Engineering Team"). */
  agentName?: string
  /**
   * Model name to display, when known.
   *
   * Special value `""` (empty string) means "the agent is processing but
   * the router hasn't classified the prompt yet" — paired with the same
   * sentinel on `taskLevel`. In that case a pulsing "…" placeholder is
   * rendered so the chip is always present while working, and the real
   * value transitions in (in lockstep with the level) once the WebSocket
   * `state` push arrives.
   *
   * `undefined` means the agent is NOT processing — the chip is hidden.
   */
  modelName?: string | undefined
  /**
   * Task level key (e.g. "L0", "L1", "L1-universal", "L3-expert"). Only the
   * prefix is shown (e.g. "L1") since the suffix is an internal role label.
   *
   * Special value `""` (empty string) means "the agent is processing but
   * the router hasn't classified the prompt yet, and there's no
   * `last_level` to fall back on." In that case a pulsing "…" placeholder
   * is rendered so the chip is always present while working, and the real
   * value transitions in once the WebSocket `state` push arrives.
   *
   * `undefined` means the agent is NOT processing — the chip is hidden.
   */
  taskLevel?: string | undefined
  /** True while the team is mid-delegation — shows a different label. */
  delegating?: boolean
  /** Layout density — design mode uses tighter spacing. */
  compact?: boolean
}

/**
 * Bottom-of-stream working indicator.
 *
 * Renders a soft, full-width pill with a "breathing" pulse animation while
 * the agent is processing. Visually distinct from the input box (which is
 * disabled during work) and from the in-stream LoadingIndicator / worked
 * group, so the user always knows *something* is happening at the bottom
 * of the conversation.
 *
 * Visual recipe:
 *  - Avatar matches the assistant message avatar (Sparkles)
 *  - Label: "Working…" or "Delegating…" with three pulsing dots
 *  - Right side: model chip + task-level chip (placeholder "…" until
 *    the router classifies the prompt)
 *  - Whole pill is wrapped in a breathing scale/opacity animation
 *    (see `.agent-working-breathing` in index.css) that softly
 *    expands and contracts on a 2.4s cycle.
 *
 * Render this at the bottom of the message stream when
 * `isAgentProcessing || streaming || delegating` is true.
 */
function AgentWorkingIndicatorInner({
  agentName = 'Assistant',
  modelName,
  taskLevel,
  delegating = false,
  compact = false,
}: AgentWorkingIndicatorProps) {
  // `taskLevel` and `modelName` are intentionally string-or-undefined:
  //   - undefined  → not processing, no chip
  //   - ""         → processing but router hasn't classified yet
  //   - "L0"|"L1"… → known level / real model
  // Both fields share the same shape so the level + model chips render
  // as a coherent pair (see useInputBadges coupling note).
  const levelIsPending = taskLevel !== undefined && taskLevel === ''
  const modelNameIsPending = modelName !== undefined && modelName === ''
  const levelLabel = taskLevel && taskLevel.length > 0 ? taskLevel.split('-')[0] : null

  return (
    <div
      className={cn(
        'agent-working-breathing flex items-center gap-3 select-none',
        compact ? 'py-1.5' : 'py-2',
      )}
    >
      {/* Avatar (matches the assistant message avatar) */}
      <div
        className={cn(
          'shrink-0 rounded-full bg-gradient-to-br from-primary/20 to-purple-500/20 flex items-center justify-center',
          compact ? 'h-5 w-5' : 'h-7 w-7',
        )}
      >
        <Sparkles
          className={cn(
            'text-primary agent-working-spark',
            compact ? 'h-3 w-3' : 'h-3.5 w-3.5',
          )}
        />
      </div>

      {/* Label + animated dots */}
      <div className="flex items-center gap-1.5 min-w-0">
        <span
          className={cn(
            'font-semibold text-foreground/80 truncate',
            compact ? 'text-[11px]' : 'text-xs',
          )}
        >
          {delegating ? `${agentName} delegating` : `${agentName} working`}
        </span>
        <span className="inline-flex items-end gap-[2px] shrink-0" aria-hidden="true">
          <span
            className={cn(
              'agent-working-dot rounded-full bg-primary',
              compact ? 'h-1 w-1' : 'h-1.5 w-1.5',
            )}
            style={{ animationDelay: '0ms' }}
          />
          <span
            className={cn(
              'agent-working-dot rounded-full bg-primary',
              compact ? 'h-1 w-1' : 'h-1.5 w-1.5',
            )}
            style={{ animationDelay: '180ms' }}
          />
          <span
            className={cn(
              'agent-working-dot rounded-full bg-primary',
              compact ? 'h-1 w-1' : 'h-1.5 w-1.5',
            )}
            style={{ animationDelay: '360ms' }}
          />
        </span>
      </div>

      {/* Right-aligned context chips: model + task level.
          Both chips share the same visibility/shape contract:
          - `undefined`  → chip hidden
          - `""`         → pulsing "…" placeholder (router not classified)
          - real value   → normal chip with the value
          They transition together when the WebSocket `state` push
          arrives, so the user never sees a level chip with a stale or
          pre-classification model. */}
      {/* Right-side group is shown if either chip would render. Since
          taskLevel/modelName are coupled, levelIsPending implies
          modelNameIsPending and vice versa, so the level term is
          sufficient to gate the placeholder case. */}
      {(levelLabel || levelIsPending || (modelName !== undefined && modelName.length > 0)) && (
        <div className="ml-auto flex items-center gap-1.5 shrink-0">
          {/* Task level chip — show real value, or a pulsing "…" while
              the router is still classifying the prompt. */}
          {(levelLabel || levelIsPending) && (
            <span
              className={cn(
                'text-[10px] font-semibold px-1.5 py-0.5 rounded-md font-mono whitespace-nowrap border',
                levelIsPending
                  ? // Subdued, dimmer styling while waiting — clearly
                    // "not yet known" rather than "L0 is the level".
                    'text-muted-foreground/60 bg-muted/20 border-border/25 animate-pulse tracking-widest'
                  : 'text-primary bg-primary/10 border-primary/20',
              )}
            >
              {levelIsPending ? '…' : levelLabel}
            </span>
          )}
          {/* Model chip — also uses the `""` placeholder when the level
              isn't classified yet, so the two chips stay in lockstep. */}
          {modelName !== undefined && (
            <span
              className={cn(
                'flex items-center gap-1 text-[10px] font-semibold px-1.5 py-0.5 rounded-md font-mono whitespace-nowrap border max-w-[220px] truncate',
                modelNameIsPending
                  ? 'text-muted-foreground/60 bg-muted/20 border-border/25 animate-pulse tracking-widest'
                  : 'text-muted-foreground/70 bg-muted/30 border-border/20',
              )}
              title={modelNameIsPending ? 'Router is classifying…' : modelName}
            >
              <Cpu
                className={cn(
                  'h-2.5 w-2.5 shrink-0',
                  modelNameIsPending ? 'text-muted-foreground/40' : 'text-muted-foreground/50',
                )}
              />
              <span className="truncate">{modelNameIsPending ? '…' : modelName}</span>
            </span>
          )}
        </div>
      )}
    </div>
  )
}

/**
 * Memoized: the only props that change during a single in-flight task are
 * `modelName` / `taskLevel` (via useInputBadges' sticky ref); other props
 * are stable for the lifetime of the working state. Wrapping in memo
 * keeps the bottom-of-stream indicator from re-rendering on every
 * chat_chunk.
 */
export const AgentWorkingIndicator = memo(AgentWorkingIndicatorInner)
