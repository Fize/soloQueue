import { Board } from './Board';
import { AgentsPanel } from './AgentsPanel';

export function HomePage() {
  return (
    <div className="flex h-full">
      {/* Left: Agent Panel (hidden on mobile) */}
      <div className="hidden md:block">
        <AgentsPanel />
      </div>
      {/* Right: Kanban Board */}
      <div className="flex-1 overflow-hidden">
        <Board />
      </div>
    </div>
  );
}
