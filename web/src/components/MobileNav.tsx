import { useLocation, useNavigate } from 'react-router-dom'
import { Bot, Kanban, FolderOpen, Settings } from 'lucide-react'

import { cn } from '@/lib/utils'

const tabs = [
  { to: '/', icon: Bot, label: 'Agents' },
  { to: '/kanban', icon: Kanban, label: 'Kanban' },
  { to: '/files', icon: FolderOpen, label: 'Files' },
  { to: '/settings', icon: Settings, label: 'Settings' },
]

export function MobileNav() {
  const location = useLocation()
  const navigate = useNavigate()

  return (
    <nav className="fixed bottom-0 inset-x-0 z-40 md:hidden border-t border-border bg-card/95 backdrop-blur-lg safe-area-pb">
      <div className="grid grid-cols-4 h-14">
        {tabs.map((tab) => {
          const isActive =
            tab.to === '/'
              ? location.pathname === '/' || location.pathname.startsWith('/agents')
              : location.pathname.startsWith(tab.to)
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
      </div>
    </nav>
  )
}
