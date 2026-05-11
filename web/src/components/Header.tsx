import { RefreshCw, FolderOpen, Kanban } from 'lucide-react'
import { Button } from '@/components/ui/button'
import type { AppTab } from '@/App'
import { cn } from '@/lib/utils'

interface HeaderProps {
  activeTab: AppTab
  onTabChange: (tab: AppTab) => void
  onRefresh?: () => void
  loading?: boolean
}

const tabs: { id: AppTab; label: string; icon?: React.ReactNode }[] = [
  { id: 'home', label: 'Home' },
  { id: 'files', label: 'Files', icon: <FolderOpen className="h-4 w-4" /> },
  { id: 'settings', label: 'Settings' },
]

export function Header({ activeTab, onTabChange, onRefresh, loading }: HeaderProps) {
  return (
    <header className="sticky top-0 z-40 flex h-14 items-center justify-between border-b-2 border-border bg-card px-4 md:px-6">
      {/* Logo */}
      <div className="flex items-center gap-2.5">
        <div className="flex h-8 w-8 items-center justify-center rounded-lg border-2 border-border bg-primary nb-shadow-xs">
          <Kanban className="h-4.5 w-4.5 text-primary-foreground" />
        </div>
        <h1 className="text-lg font-bold tracking-tight text-foreground">SoloQueue</h1>
      </div>

      {/* Center: Tab Buttons */}
      <nav className="flex items-center gap-1">
        {tabs.map((tab) => (
          <button
            key={tab.id}
            onClick={() => onTabChange(tab.id)}
            className={cn(
              'rounded-md px-3 py-1.5 text-sm font-medium transition-all inline-flex items-center gap-1.5',
              activeTab === tab.id
                ? 'border-2 border-border bg-primary nb-shadow-xs font-bold text-primary-foreground'
                : 'border-2 border-transparent text-muted-foreground hover:text-foreground hover:bg-muted'
            )}
          >
            {tab.icon}
            {tab.label}
          </button>
        ))}
      </nav>

      {/* Right: Refresh */}
      <div>
        {onRefresh ? (
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={onRefresh}
            disabled={loading}
            className="text-muted-foreground hover:text-foreground"
          >
            <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
          </Button>
        ) : (
          <div className="w-7" />
        )}
      </div>
    </header>
  )
}
