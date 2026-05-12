import { useState, useEffect, useRef } from 'react'
import { cn } from '@/lib/utils'
import { GeneralTab } from './settings/GeneralTab'
import { ModelsTab } from './settings/ModelsTab'
import { ProfileTab } from './settings/ProfileTab'
import { ToolsTab } from './settings/ToolsTab'
import { SkillsTab } from './settings/SkillsTab'
import { IntegrationsTab } from './settings/IntegrationsTab'
import { MCPTab } from './settings/MCPTab'
import { AgentTab } from './settings/AgentTab'
import { Settings, User, Cpu, Wrench, Sparkles, Plug, Server, Bot } from 'lucide-react'

type SettingsTab =
  | 'general'
  | 'profile'
  | 'models'
  | 'tools'
  | 'skills'
  | 'integrations'
  | 'mcp'
  | 'agent'

const settingsTabs: { id: SettingsTab; label: string; icon: typeof Settings }[] = [
  { id: 'general', label: 'General', icon: Settings },
  { id: 'profile', label: 'Profile', icon: User },
  { id: 'models', label: 'Models', icon: Cpu },
  { id: 'tools', label: 'Tools', icon: Wrench },
  { id: 'skills', label: 'Skills', icon: Sparkles },
  { id: 'integrations', label: 'Integrations', icon: Plug },
  { id: 'agent', label: 'Agent', icon: Bot },
  { id: 'mcp', label: 'MCP', icon: Server },
]

interface SettingsViewProps {
  initialTab?: string | null
}

export function SettingsView({ initialTab }: SettingsViewProps) {
  const [activeTab, setActiveTab] = useState<SettingsTab>((initialTab as SettingsTab) || 'general')
  const scrollRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (initialTab && initialTab !== activeTab) {
      setActiveTab(initialTab as SettingsTab)
    }
  }, [initialTab])

  const handleTabClick = (tab: SettingsTab) => {
    setActiveTab(tab)
    window.location.hash = `settings/${tab}`
  }

  return (
    <div className="flex h-full flex-col md:flex-row">
      {/* Mobile: Horizontal tab strip */}
      <div
        ref={scrollRef}
        className="flex gap-1 overflow-x-auto border-b-2 border-border px-4 py-2 md:hidden"
      >
        {settingsTabs.map((tab) => {
          const Icon = tab.icon
          return (
            <button
              key={tab.id}
              onClick={() => handleTabClick(tab.id)}
              className={cn(
                'flex shrink-0 items-center gap-1.5 rounded-md px-3 py-1.5 text-sm font-medium whitespace-nowrap transition-colors',
                activeTab === tab.id
                  ? 'nb-border bg-primary nb-shadow-xs font-bold text-primary-foreground'
                  : 'border-2 border-transparent text-muted-foreground hover:text-foreground hover:bg-muted'
              )}
            >
              <Icon className="h-4 w-4" />
              {tab.label}
            </button>
          )
        })}
      </div>

      {/* Desktop: Sidebar */}
      <aside className="hidden w-48 shrink-0 border-r-2 border-border bg-card p-4 md:block">
        <nav className="flex flex-col gap-1">
          {settingsTabs.map((tab) => {
            const Icon = tab.icon
            return (
              <button
                key={tab.id}
                onClick={() => handleTabClick(tab.id)}
                className={cn(
                  'flex items-center gap-2 rounded-md px-3 py-2 text-left text-sm font-medium transition-colors',
                  activeTab === tab.id
                    ? 'nb-border bg-primary nb-shadow-xs font-bold text-primary-foreground'
                    : 'border-2 border-transparent text-muted-foreground hover:text-foreground hover:bg-muted'
                )}
              >
                <Icon className="h-4 w-4" />
                {tab.label}
              </button>
            )
          })}
        </nav>
      </aside>

      {/* Right content */}
      <div className="flex-1 overflow-y-auto py-6">
        <div className="mx-auto max-w-3xl">
          {activeTab === 'general' && <GeneralTab />}
          {activeTab === 'profile' && <ProfileTab />}
          {activeTab === 'models' && <ModelsTab />}
          {activeTab === 'tools' && <ToolsTab />}
          {activeTab === 'skills' && <SkillsTab />}
          {activeTab === 'integrations' && <IntegrationsTab />}
          {activeTab === 'agent' && <AgentTab />}
          {activeTab === 'mcp' && <MCPTab />}
        </div>
      </div>
    </div>
  )
}
