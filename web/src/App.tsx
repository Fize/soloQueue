import { useState } from 'react';
import { Header } from '@/components/Header';
import { RuntimeStatusBar } from '@/components/RuntimeStatusBar';
import { HomePage } from '@/components/HomePage';
import { SettingsView } from '@/components/SettingsView';
import { TooltipProvider } from '@/components/ui/tooltip';

export type AppTab = 'home' | 'settings';

function App() {
  const [activeTab, setActiveTab] = useState<AppTab>('home');

  return (
    <TooltipProvider>
      <div className="flex h-screen flex-col bg-background">
        <Header activeTab={activeTab} onTabChange={setActiveTab} />
        <RuntimeStatusBar />
        <main className="flex-1 overflow-hidden">
          {activeTab === 'home' && <HomePage />}
          {activeTab === 'settings' && <SettingsView />}
        </main>
      </div>
    </TooltipProvider>
  );
}

export default App;
