import { useState } from 'react';
import { cn } from '@/lib/utils';
import { GeneralTab } from './settings/GeneralTab';
import { ModelsTab } from './settings/ModelsTab';
import { ProfileTab } from './settings/ProfileTab';
import { ToolsTab } from './settings/ToolsTab';
import { SkillsTab } from './settings/SkillsTab';
import { IntegrationsTab } from './settings/IntegrationsTab';
import { Settings, User, Cpu, Wrench, Sparkles, Plug } from 'lucide-react';

type SettingsTab = 'general' | 'profile' | 'models' | 'tools' | 'skills' | 'integrations';

const settingsTabs: { id: SettingsTab; label: string; icon: typeof Settings }[] = [
  { id: 'general', label: 'General', icon: Settings },
  { id: 'profile', label: 'Profile', icon: User },
  { id: 'models', label: 'Models', icon: Cpu },
  { id: 'tools', label: 'Tools', icon: Wrench },
  { id: 'skills', label: 'Skills', icon: Sparkles },
  { id: 'integrations', label: 'Integrations', icon: Plug },
];

interface SettingsViewProps {
  initialTab?: string | null;
}

export function SettingsView({ initialTab }: SettingsViewProps) {
  const [activeTab, setActiveTab] = useState<SettingsTab>(
    (initialTab as SettingsTab) || 'general',
  );

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
          {activeTab === 'profile' && <ProfileTab />}
          {activeTab === 'models' && <ModelsTab />}
          {activeTab === 'tools' && <ToolsTab />}
          {activeTab === 'skills' && <SkillsTab />}
          {activeTab === 'integrations' && <IntegrationsTab />}
        </div>
      </div>
    </div>
  );
}
