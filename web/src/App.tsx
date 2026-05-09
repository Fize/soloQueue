import { useState } from 'react';
import { Board } from '@/components/Board';
import { Header } from '@/components/Header';
import { RuntimeStatusBar } from '@/components/RuntimeStatusBar';
import { AgentsView } from '@/components/AgentsView';
import { SettingsView } from '@/components/SettingsView';
import { TooltipProvider } from '@/components/ui/tooltip';

export type AppTab = 'board' | 'agents' | 'settings';

function App() {
  const [activeTab, setActiveTab] = useState<AppTab>('board');

  return (
    <TooltipProvider>
      <div className="flex h-screen flex-col bg-background">
        <Header activeTab={activeTab} onTabChange={setActiveTab} />
        <RuntimeStatusBar />
        <main className="flex-1 overflow-hidden">
          {activeTab === 'board' && <Board />}
          {activeTab === 'agents' && <AgentsView />}
          {activeTab === 'settings' && <SettingsView />}
        </main>
      </div>
    </TooltipProvider>
  );
}

export default App;
