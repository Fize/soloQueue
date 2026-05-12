import { RefreshCw, FolderOpen } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Container } from '@/components/ui/Container'
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
    <header className="sticky top-0 z-40 border-b-2 border-border bg-card">
      <Container className="flex h-14 items-center justify-between">
        {/* Logo */}
        <button
          onClick={() => onTabChange('home')}
          className="flex items-center gap-2.5 cursor-pointer focus:outline-none"
        >
          <img src="/logo.png" alt="SoloQueue" className="h-8 w-8" />
          <h1 className="hidden sm:inline text-lg font-bold tracking-tight text-foreground">SoloQueue</h1>
        </button>

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
      </Container>
    </header>
  )
}
