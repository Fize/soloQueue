import { useState, useEffect } from 'react'
import { cn } from '@/lib/utils'
import { ConfigTab } from './settings/ConfigTab'
import { ProfileTab } from './settings/ProfileTab'
import { SkillsTab } from './settings/SkillsTab'
import { MCPTab } from './settings/MCPTab'
import { FileText, User, Sparkles, Server } from 'lucide-react'

type SettingsTab =
  | 'config'
  | 'profile'
  | 'skills'
  | 'mcp'

const settingsTabs: { id: SettingsTab; label: string; icon: typeof FileText }[] = [
  { id: 'config', label: 'Config', icon: FileText },
  { id: 'profile', label: 'Profile', icon: User },
  { id: 'skills', label: 'Skills', icon: Sparkles },
  { id: 'mcp', label: 'MCP', icon: Server },
]

interface SettingsViewProps {
  initialTab?: string | null
}

export function SettingsView({ initialTab }: SettingsViewProps) {
  const [activeTab, setActiveTab] = useState<SettingsTab>((initialTab as SettingsTab) || 'config')

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
    <div className="flex h-full flex-row">
      <aside className="w-48 shrink-0 border-r border-border bg-card p-4 block">
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
                    ? 'border bg-primary font-bold text-primary-foreground shadow-xs'
                    : 'border border-transparent text-muted-foreground hover:text-foreground hover:bg-muted'
                )}
              >
                <Icon className="h-4 w-4" />
                {tab.label}
              </button>
            )
          })}
        </nav>
      </aside>

      <div className="flex-1 overflow-y-auto py-6">
        <div className="mx-auto max-w-3xl">
          {activeTab === 'config' && <ConfigTab />}
          {activeTab === 'profile' && <ProfileTab />}
          {activeTab === 'skills' && <SkillsTab />}
          {activeTab === 'mcp' && <MCPTab />}
        </div>
      </div>
    </div>
  )
}
