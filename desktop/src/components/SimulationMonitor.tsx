import { useEffect, useRef } from 'react'
import { Terminal } from 'lucide-react'

interface SimulationMonitorProps {
  logs: string[]
}

export function SimulationMonitor({ logs }: SimulationMonitorProps) {
  const endRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [logs])

  return (
    <div className="rounded-xl border border-border/60 bg-[#0d1117] overflow-hidden">
      <div className="flex items-center gap-2 border-b border-border/20 px-3 py-2">
        <Terminal className="h-3.5 w-3.5 text-emerald-400" />
        <span className="text-[10px] font-mono font-semibold text-emerald-400/80 uppercase tracking-wider">
          Simulation Monitor
        </span>
        <span className="text-[8px] font-mono text-muted-foreground/50 ml-auto">
          {logs.length} lines
        </span>
      </div>
      <div className="h-40 overflow-y-auto p-3 font-mono text-[10px] leading-relaxed">
        {logs.length === 0 ? (
          <div className="flex h-full items-center justify-center text-muted-foreground/40">
            Waiting for logs...
          </div>
        ) : (
          logs.map((line, i) => (
            <div
              key={i}
              className="whitespace-pre-wrap text-emerald-300/80 hover:bg-white/[0.02] px-1 rounded"
            >
              <span className="text-muted-foreground/50 mr-2 select-none">
                {String(i + 1).padStart(3, ' ')}
              </span>
              {line}
            </div>
          ))
        )}
        <div ref={endRef} />
      </div>
    </div>
  )
}
