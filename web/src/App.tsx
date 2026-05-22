import { useEffect, useState } from 'react'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { Sidebar } from '@/components/Sidebar'
import { MobileNav } from '@/components/MobileNav'
import { AgentListPage } from '@/components/AgentListPage'
import { AgentDetailPage } from '@/components/AgentDetailPage'
import { PlansPage } from '@/components/PlansPage'
import { PlanDetail } from '@/components/PlanDetail'
import { FilesPage } from '@/components/FilesPage'
import { SettingsLayout } from '@/components/SettingsLayout'
import { ConfigTab } from '@/components/settings/ConfigTab'
import { ProfileTab } from '@/components/settings/ProfileTab'
import { SkillsTab } from '@/components/settings/SkillsTab'
import { MCPTab } from '@/components/settings/MCPTab'
import TeamsTab from '@/components/settings/TeamsTab'
import { TooltipProvider } from '@/components/ui/tooltip'
import { wsManager } from '@/lib/websocket'
import { useAuthStore } from '@/stores/authStore'

function App() {
  const { isAuthenticated, isLoading } = useAuthStore()

  if (isLoading) {
    return (
      <div className="flex h-screen items-center justify-center bg-background">
        <div className="text-sm text-muted-foreground font-mono">Loading...</div>
      </div>
    )
  }

  if (!isAuthenticated) {
    return (
      <div className="flex h-screen flex-col items-center justify-center bg-background text-foreground gap-4">
        <div className="text-sm font-semibold">Authentication required</div>
        <button
          onClick={() => window.location.reload()}
          className="px-4 py-2 bg-primary text-primary-foreground rounded-lg text-sm font-medium hover:bg-primary/90 transition-colors"
        >
          Log In / Refresh
        </button>
      </div>
    )
  }

  return (
    <TooltipProvider>
      <div className="flex h-screen bg-background overflow-hidden pb-14 md:pb-0">
        {/* Desktop sidebar — always rendered for layout on md+ */}
        <div className="hidden md:block shrink-0 h-full border-r border-border bg-card">
          <Sidebar />
        </div>

        <main className="flex flex-1 flex-col min-w-0 overflow-hidden h-full">
          <div className="flex-1 overflow-hidden">
            <Routes>
              <Route path="/" element={<AgentListPage />} />
              <Route path="/agents" element={<AgentListPage />} />
              <Route path="/agents/:id" element={<AgentDetailPage />} />
              <Route path="/plans" element={<PlansPage />} />
              <Route path="/plans/:id" element={<PlanDetail />} />
              <Route path="/files" element={<FilesPage />} />
              <Route path="/settings" element={<SettingsLayout />}>
                <Route index element={<Navigate to="config" replace />} />
                <Route path="config" element={<ConfigTab />} />
                <Route path="profile" element={<ProfileTab />} />
                <Route path="skills" element={<SkillsTab />} />
                <Route path="mcp" element={<MCPTab />} />
                <Route path="teams" element={<TeamsTab />} />
              </Route>
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </div>
        </main>

        {/* Mobile bottom navigation tab bar */}
        <MobileNav />
      </div>
    </TooltipProvider>
  )
}

export default function AppWithRouter() {
  const checkAuth = useAuthStore((s) => s.checkAuth)
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated)
  const [ready, setReady] = useState(false)

  useEffect(() => {
    checkAuth().finally(() => setReady(true))
  }, [checkAuth])

  useEffect(() => {
    if (!ready) return
    if (isAuthenticated) {
      wsManager.connect()
    } else {
      wsManager.disconnect()
    }
    return () => {
      wsManager.disconnect()
    }
  }, [ready, isAuthenticated])

  if (!ready) {
    return (
      <div className="flex h-screen items-center justify-center bg-background">
        <div className="text-sm text-muted-foreground font-mono">Loading...</div>
      </div>
    )
  }

  return (
    <BrowserRouter>
      <App />
    </BrowserRouter>
  )
}
