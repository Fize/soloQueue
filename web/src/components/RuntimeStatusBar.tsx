import { useRuntime } from '@/hooks/useRuntime'
import { Container } from '@/components/ui/Container'

export function RuntimeStatusBar() {
  const status = useRuntime()

  if (!status) return null

  return (
    <div className="border-t border-[#EEEEEE] bg-muted">
      <Container className="flex items-center gap-4 py-1.5 text-xs text-muted-foreground">
        <span className="font-medium">
          Agents: <span className="text-foreground">{status.total_agents}</span>
        </span>
        <span>
          Running: <span className="text-foreground">{status.running_agents}</span>
          {' / '}
          Idle: <span className="text-foreground">{status.idle_agents}</span>
        </span>
        <span>
          Tokens:{' '}
          <span className="text-foreground">
            {(status.prompt_tokens + status.output_tokens).toLocaleString()}
          </span>
        </span>
        <span>
          Context: <span className="text-foreground">{status.context_pct}%</span>
        </span>
        <span>
          Phase: <span className="text-foreground">{status.phase}</span>
        </span>
        {status.total_errors > 0 && (
          <span className="text-destructive-foreground font-medium">
            Errors: {status.total_errors}
          </span>
        )}
      </Container>
    </div>
  )
}
