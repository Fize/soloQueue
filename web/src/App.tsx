import { useState, useEffect, useCallback } from 'react';
import { Header } from '@/components/Header';
import { RuntimeStatusBar } from '@/components/RuntimeStatusBar';
import { HomePage } from '@/components/HomePage';
import { SettingsView } from '@/components/SettingsView';
import { TooltipProvider } from '@/components/ui/tooltip';
import { wsManager } from '@/lib/websocket';

export type AppTab = 'home' | 'settings';

type SettingsSubtab = string | null;

function getTabFromHash(): { tab: AppTab; subtab: SettingsSubtab } {
  const hash = window.location.hash.replace('#', '');
  if (hash.startsWith('settings')) {
    const parts = hash.split('/');
    return { tab: 'settings', subtab: parts[1] || null };
  }
  return { tab: 'home', subtab: null };
}

function App() {
  const initial = getTabFromHash();
  const [activeTab, setActiveTab] = useState<AppTab>(initial.tab);
  const [settingsSubtab, setSettingsSubtab] = useState<SettingsSubtab>(initial.subtab);

  const handleTabChange = useCallback((tab: AppTab) => {
    setActiveTab(tab);
    window.location.hash = tab;
  }, []);

  useEffect(() => {
    const onHashChange = () => {
      const { tab, subtab } = getTabFromHash();
      setActiveTab(tab);
      setSettingsSubtab(subtab);
    };
    window.addEventListener('hashchange', onHashChange);
    return () => window.removeEventListener('hashchange', onHashChange);
  }, []);

  useEffect(() => {
    wsManager.connect();
    return () => {
      wsManager.disconnect();
    };
  }, []);

  return (
    <TooltipProvider>
      <div className="flex h-screen flex-col bg-background">
        <Header activeTab={activeTab} onTabChange={handleTabChange} />
        <RuntimeStatusBar />
        <main className="flex-1 overflow-hidden">
          <div className={activeTab === 'home' ? 'h-full' : 'hidden h-0'}>
            <HomePage />
          </div>
          <div className={activeTab === 'settings' ? 'h-full' : 'hidden h-0'}>
            <SettingsView initialTab={settingsSubtab} />
          </div>
        </main>
      </div>
    </TooltipProvider>
  );
}

export default App;
