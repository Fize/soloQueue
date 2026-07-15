import { memo } from 'react'
import { Cpu } from 'lucide-react'
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
  modelName,
  taskLevel,
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
      {/* Breathing Light (呼吸灯) */}
      <span className="relative flex h-2 w-2 mx-1 shrink-0">
        <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-primary opacity-75"></span>
        <span className="relative inline-flex rounded-full h-2 w-2 bg-primary"></span>
      </span>

      {/* Left-aligned context chips: model + task level. */}
      {(levelLabel || levelIsPending || (modelName !== undefined && modelName.length > 0)) && (
        <div className="flex items-center gap-1.5 shrink-0">
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
