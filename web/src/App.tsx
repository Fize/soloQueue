import { useEffect, useState, useCallback } from 'react'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { Sidebar } from '@/components/Sidebar'
import { Dashboard } from '@/components/Dashboard'
import { PlansPage } from '@/components/PlansPage'
import { FilesPage } from '@/components/FilesPage'
import { SettingsLayout } from '@/components/SettingsLayout'
import { ConfigTab } from '@/components/settings/ConfigTab'
import { ProfileTab } from '@/components/settings/ProfileTab'
import { SkillsTab } from '@/components/settings/SkillsTab'
import { MCPTab } from '@/components/settings/MCPTab'
import { User, Menu } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { TooltipProvider } from '@/components/ui/tooltip'
import { wsManager } from '@/lib/websocket'

function App() {
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const closeSidebar = useCallback(() => setSidebarOpen(false), [])

  return (
    <TooltipProvider>
      <div className="flex h-screen bg-background">
        {/* Desktop sidebar — always rendered for layout on md+ */}
        <div className="hidden md:block shrink-0">
          <Sidebar />
        </div>

        {/* Mobile sidebar overlay */}
        {sidebarOpen && (
          <div className="fixed inset-0 z-50 md:hidden">
            <div
              className="absolute inset-0 bg-black/30 animate-in fade-in-0"
              onClick={closeSidebar}
            />
            <Sidebar mobile onClose={closeSidebar} />
          </div>
        )}

        <main className="flex flex-1 flex-col min-w-0 overflow-hidden">
          <header className="flex h-14 shrink-0 items-center justify-between border-b border-border px-4 bg-card">
            <button
              className="md:hidden inline-flex items-center justify-center rounded-md h-11 w-11 min-h-11 min-w-11 text-foreground hover:bg-muted active:bg-muted/80 transition-colors"
              onClick={() => setSidebarOpen(true)}
              aria-label="Open menu"
            >
              <Menu className="h-5 w-5" />
            </button>
            <Button variant="ghost" size="icon" className="h-8 w-8" title="Logout">
              <User className="h-4 w-4" />
            </Button>
          </header>
          <div className="flex-1 overflow-hidden">
            <Routes>
              <Route path="/" element={<Dashboard />} />
              <Route path="/plans" element={<PlansPage />} />
              <Route path="/files" element={<FilesPage />} />
              <Route path="/settings" element={<SettingsLayout />}>
                <Route index element={<Navigate to="config" replace />} />
                <Route path="config" element={<ConfigTab />} />
                <Route path="profile" element={<ProfileTab />} />
                <Route path="skills" element={<SkillsTab />} />
                <Route path="mcp" element={<MCPTab />} />
              </Route>
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </div>
        </main>
      </div>
    </TooltipProvider>
  )
}

export default function AppWithRouter() {
  useEffect(() => {
    wsManager.connect()
    return () => {
      wsManager.disconnect()
    }
  }, [])

  return (
    <BrowserRouter>
      <App />
    </BrowserRouter>
  )
}
