import { memo } from 'react'
import { Cpu } from 'lucide-react'
import { cn, getModelColorVar } from '@/lib/utils'

export interface AgentWorkingIndicatorProps {
  /** Display name of the active agent (e.g. "Assistant", "Engineering Team"). */
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
   * Internal routing metadata. It is intentionally not rendered in the
   * desktop UI because routing levels are implementation details.
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
 *  - Right side: model chip (placeholder "…" until the model is known)
 *  - Whole pill is wrapped in a breathing scale/opacity animation
 *    (see `.agent-working-breathing` in index.css) that softly
 *    expands and contracts on a 2.4s cycle.
 *
 * Render this at the bottom of the message stream when
 * `isAgentProcessing || streaming || delegating` is true.
 */
function AgentWorkingIndicatorInner({
  modelName,
  compact = false,
}: AgentWorkingIndicatorProps) {
  // `taskLevel` and `modelName` are intentionally string-or-undefined:
  //   - undefined  → not processing, no chip
  //   - ""         → processing but router hasn't classified yet
  //   - a known level / real model
  // Both fields share the same shape so the model chip can transition from
  // its pending state without changing the surrounding layout.
  const modelNameIsPending = modelName !== undefined && modelName === ''
  const modelColorVar = getModelColorVar(modelName)

  return (
    <div
      className={cn(
        'agent-working-breathing flex items-center gap-3 select-none',
        compact ? 'py-1.5' : 'py-2',
      )}
    >
      {/* Breathing Light (呼吸灯) */}
      <span className="relative flex h-2.5 w-2.5 mx-1.5 shrink-0 items-center justify-center">
        <span className="animate-led-breathing absolute h-2 w-2 rounded-full bg-signal" />
        <span className="animate-led-ping-1 absolute h-2 w-2 rounded-full bg-signal/40" />
        <span className="animate-led-ping-2 absolute h-2 w-2 rounded-full bg-signal/20" />
      </span>

      {/* Model chip. Routing levels remain internal metadata. */}
      {modelName !== undefined && (
        <div className="flex items-center gap-1.5 shrink-0">
          {modelNameIsPending ? (
            <div className="h-[22px] w-[80px] bg-foreground/5 border border-border/40 rounded-md animate-pulse shrink-0" />
          ) : (
            <span
              className="flex items-center gap-1 text-[10px] font-semibold px-1.5 py-0.5 rounded-md font-mono whitespace-nowrap border max-w-[220px] truncate"
              style={{
                backgroundColor: `color-mix(in srgb, ${modelColorVar} 15%, transparent)`,
                color: `var(--foreground)`,
                borderColor: `color-mix(in srgb, ${modelColorVar} 30%, transparent)`,
              }}
              title={modelName}
            >
              <Cpu
                className="h-2.5 w-2.5 shrink-0"
                style={{ color: `color-mix(in srgb, ${modelColorVar} 80%, var(--foreground))` }}
              />
              <span className="truncate opacity-90">{modelName}</span>
            </span>
          )}
        </div>
      )}
    </div>
  )
}

/**
 * Memoized: the only props that change during a single in-flight task are
 * `modelName` / `taskLevel` (via the request-scoped route state); other props
 * are stable for the lifetime of the working state. Wrapping in memo
 * keeps the bottom-of-stream indicator from re-rendering on every
 * chat_chunk.
 */
export const AgentWorkingIndicator = memo(AgentWorkingIndicatorInner)
