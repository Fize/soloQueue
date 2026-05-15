import { NavLink } from 'react-router-dom'
import { Button } from '@/components/ui/button'
import { Container } from '@/components/ui/Container'
import { cn } from '@/lib/utils'
import { RefreshCw, FolderOpen } from 'lucide-react'

interface HeaderProps {
  onRefresh?: () => void
  loading?: boolean
}

export function Header({ onRefresh, loading }: HeaderProps) {
  return (
    <header className="sticky top-0 z-40 border-b border-border bg-card/85 backdrop-blur-md">
      <Container className="flex h-14 items-center justify-between">
        {/* Logo */}
        <NavLink
          to="/"
          className="flex items-center gap-2.5 focus:outline-none"
        >
          <img src="/logo.png" alt="SoloQueue" className="h-8 w-8" />
          <h1 className="text-lg font-bold tracking-tight text-foreground">
            SoloQueue
          </h1>
        </NavLink>

        {/* Center: Nav Links */}
        <nav className="flex items-center gap-1">
          <NavLink
            to="/"
            className={({ isActive }) =>
              cn(
                'rounded-md px-3 py-1.5 text-sm font-medium transition-all inline-flex items-center gap-1.5',
                isActive
                  ? 'bg-primary text-primary-foreground shadow-sm'
                  : 'text-muted-foreground hover:bg-muted hover:text-foreground'
              )
            }
          >
            Home
          </NavLink>
          <NavLink
            to="/files"
            className={({ isActive }) =>
              cn(
                'rounded-md px-3 py-1.5 text-sm font-medium transition-all inline-flex items-center gap-1.5',
                isActive
                  ? 'bg-primary text-primary-foreground shadow-sm'
                  : 'text-muted-foreground hover:bg-muted hover:text-foreground'
              )
            }
          >
            <FolderOpen className="h-4 w-4" />
            Files
          </NavLink>
          <NavLink
            to="/settings"
            className={({ isActive }) =>
              cn(
                'rounded-md px-3 py-1.5 text-sm font-medium transition-all',
                isActive
                  ? 'bg-primary text-primary-foreground shadow-sm'
                  : 'text-muted-foreground hover:bg-muted hover:text-foreground'
              )
            }
          >
            Settings
          </NavLink>
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
