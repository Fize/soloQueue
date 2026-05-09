import { useState } from 'react';
import { cn } from '@/lib/utils';
import { GeneralTab } from './settings/GeneralTab';
import { ModelsTab } from './settings/ModelsTab';
import { SkillsTab } from './settings/SkillsTab';
import { McpTab } from './settings/McpTab';
import { Settings, Cpu, Zap, Server } from 'lucide-react';

type SettingsTab = 'general' | 'models' | 'skills' | 'mcp';

const settingsTabs: { id: SettingsTab; label: string; icon: typeof Settings }[] = [
  { id: 'general', label: 'General', icon: Settings },
  { id: 'models', label: 'Models', icon: Cpu },
  { id: 'skills', label: 'Skills', icon: Zap },
  { id: 'mcp', label: 'MCP', icon: Server },
];

export function SettingsView() {
  const [activeTab, setActiveTab] = useState<SettingsTab>('general');

  return (
    <div className="flex h-full">
      {/* Left sidebar */}
      <aside className="w-48 shrink-0 border-r-2 border-[#EEEEEE] bg-card p-4">
        <nav className="flex flex-col gap-1">
          {settingsTabs.map((tab) => {
            const Icon = tab.icon;
            return (
              <button
                key={tab.id}
                onClick={() => setActiveTab(tab.id)}
                className={cn(
                  'flex items-center gap-2 rounded-md px-3 py-2 text-left text-sm font-medium transition-colors',
                  activeTab === tab.id
                    ? 'nb-border bg-primary nb-shadow-xs font-bold text-primary-foreground'
                    : 'border-2 border-transparent text-muted-foreground hover:text-foreground hover:bg-muted',
                )}
              >
                <Icon className="h-4 w-4" />
                {tab.label}
              </button>
            );
          })}
        </nav>
      </aside>

      {/* Right content */}
      <div className="flex-1 overflow-y-auto p-6">
        <div className="mx-auto max-w-3xl">
          {activeTab === 'general' && <GeneralTab />}
          {activeTab === 'models' && <ModelsTab />}
          {activeTab === 'skills' && <SkillsTab />}
          {activeTab === 'mcp' && <McpTab />}
        </div>
      </div>
    </div>
  );
}
