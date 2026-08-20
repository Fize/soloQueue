interface BackendUnavailableProps {
  onRetry?: () => void
}

/**
 * Shared placeholder for "backend not connected" states. Rendered by pages
 * when the backend readiness gate (`connectionStore.backendReady`) is false,
 * replacing bespoke spinners/empty states that previously spun forever while
 * the backend was down.
 */
export function BackendUnavailable({ onRetry }: BackendUnavailableProps) {
  return (
    <div
      className="flex flex-col gap-1.5 pl-3 pr-2 py-2 text-[11px] text-muted-foreground/70"
      role="status"
    >
      <div className="flex items-center gap-1.5">
        <span className="h-1.5 w-1.5 rounded-full bg-amber-500 shrink-0" />
        <span>Backend not connected</span>
      </div>
      {onRetry && (
        <button
          type="button"
          onClick={onRetry}
          className="self-start text-foreground/80 hover:text-foreground underline-offset-2 hover:underline cursor-pointer"
        >
          Retry
        </button>
      )}
    </div>
  )
}
