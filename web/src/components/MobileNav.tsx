import { useLocation, useNavigate } from 'react-router-dom'
import { Bot, FolderOpen, Clock, Menu } from 'lucide-react'

import { cn } from '@/lib/utils'

interface MobileNavProps {
  onMenuClick: () => void
}

const tabs = [
  { to: '/', icon: Bot, label: 'Agents' },
  { to: '/files', icon: FolderOpen, label: 'Files' },
  { to: '/cron', icon: Clock, label: 'Cron' },
]

export function MobileNav({ onMenuClick }: MobileNavProps) {
  const location = useLocation()
  const navigate = useNavigate()

  const isTabActive = (to: string) => {
    return to === '/'
      ? location.pathname === '/' || location.pathname.startsWith('/agents')
      : location.pathname.startsWith(to)
  }

  const anyTabActive = tabs.some((tab) => isTabActive(tab.to))
  const isMenuButtonActive = !anyTabActive

  return (
    <nav className="fixed bottom-0 inset-x-0 z-40 md:hidden border-t border-border bg-card/95 backdrop-blur-lg safe-area-pb">
      <div className="grid grid-cols-5 h-14">
        {tabs.map((tab) => {
          const isActive = isTabActive(tab.to)
          return (
            <button
              key={tab.to}
              onClick={() => navigate(tab.to)}
              className={cn(
                'flex flex-col items-center justify-center gap-0.5 text-[10px] font-medium transition-colors',
                isActive ? 'text-primary' : 'text-muted-foreground active:text-foreground'
              )}
            >
              <tab.icon className={cn('h-5 w-5', isActive && 'text-primary')} />
              {tab.label}
            </button>
          )
        })}

        <button
          onClick={onMenuClick}
          className={cn(
            'flex flex-col items-center justify-center gap-0.5 text-[10px] font-medium transition-colors',
            isMenuButtonActive ? 'text-primary' : 'text-muted-foreground active:text-foreground'
          )}
        >
          <Menu className={cn('h-5 w-5', isMenuButtonActive && 'text-primary')} />
          Menu
        </button>
      </div>
    </nav>
  )
}
