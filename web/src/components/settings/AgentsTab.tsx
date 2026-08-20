import { useState } from 'react'
import { useTranslation } from '@/lib/i18n'
import { ProfileTab } from './ProfileTab'
import TeamsTab from './TeamsTab'

export function AgentsTab() {
  const { t } = useTranslation()
  const [activeTab, setActiveTab] = useState<'profile' | 'teams'>('profile')

  const subTabButtons = [
    { key: 'profile' as const, label: t('profile.title') || '助手画像' },
    { key: 'teams' as const, label: t('teams.title') || '团队管理' },
  ]

  return (
    <div className="space-y-6">
      {/* Sub-tab Switcher */}
      <div className="flex flex-wrap gap-2 p-1 bg-muted rounded-lg w-fit">
        {subTabButtons.map((btn) => (
          <button
            key={btn.key}
            type="button"
            onClick={() => setActiveTab(btn.key)}
            className={`px-3 py-1.5 text-xs font-semibold rounded-md transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50 ${
              activeTab === btn.key
                ? 'bg-background text-foreground shadow-sm'
                : 'text-muted-foreground hover:text-foreground'
            }`}
          >
            {btn.label}
          </button>
        ))}
      </div>

      {/* Sub-tab Content */}
      <div className="space-y-6">
        {activeTab === 'profile' && <ProfileTab />}
        {activeTab === 'teams' && <TeamsTab />}
      </div>
    </div>
  )
}

export default AgentsTab
