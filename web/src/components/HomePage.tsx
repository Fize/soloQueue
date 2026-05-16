import { Board } from './Board'
import { AgentsPanel } from './AgentsPanel'

export function HomePage() {
  return (
    <div className="flex h-full gap-4">
      <div>
        <AgentsPanel />
      </div>

      <div className="flex-1 overflow-hidden h-full flex flex-col min-h-0">
        <Board />
      </div>
    </div>
  )
}
