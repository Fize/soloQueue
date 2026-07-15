import { useState } from 'react'
import { useTranslation } from '@/lib/i18n'
import { SkillsTab } from './SkillsTab'
import { MCPTab } from './MCPTab'

export function CapabilitiesTab() {
  const { t } = useTranslation()
  const [activeTab, setActiveTab] = useState<'skills' | 'mcp'>('skills')

  const subTabButtons = [
    { key: 'skills' as const, label: t('skills.title') || '智能体技能' },
    { key: 'mcp' as const, label: t('mcp.title') || 'MCP 服务' },
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
        {activeTab === 'skills' && <SkillsTab />}
        {activeTab === 'mcp' && <MCPTab />}
      </div>
    </div>
  )
}

export default CapabilitiesTab
