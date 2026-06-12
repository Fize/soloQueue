import { useEffect, useState } from 'react'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { Sidebar } from '@/components/Sidebar'
import { MobileNav } from '@/components/MobileNav'
import { AgentDetailPage } from '@/components/AgentDetailPage'
import { FilesPage } from '@/components/FilesPage'
import { CronPage } from '@/components/CronPage'
import { SimulationListPage } from '@/components/SimulationListPage'
import { SimulationDetailPage } from '@/components/SimulationDetailPage'
import { SettingsLayout } from '@/components/SettingsLayout'
import { ConfigTab } from '@/components/settings/ConfigTab'
import { ProfileTab } from '@/components/settings/ProfileTab'
import { SkillsTab } from '@/components/settings/SkillsTab'
import { MCPTab } from '@/components/settings/MCPTab'
import TeamsTab from '@/components/settings/TeamsTab'
import { ProjectsTab } from '@/components/settings/ProjectsTab'
import { ProxiesTab } from '@/components/settings/ProxiesTab'
import { IframePageView } from '@/components/IframePageView'
import { ChatPage } from '@/components/ChatPage'
import { TooltipProvider } from '@/components/ui/tooltip'
import { wsManager } from '@/lib/websocket'
import { useAuthStore } from '@/stores/authStore'

function App() {
  const { isAuthenticated, isLoading } = useAuthStore()
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false)

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
              <Route path="/" element={<ChatPage />} />
              <Route path="/agents" element={<Navigate to="/" replace />} />
              <Route path="/agents/:id" element={<AgentDetailPage />} />
              <Route path="/files" element={<FilesPage />} />
              <Route path="/cron" element={<CronPage />} />
              <Route path="/simulations" element={<SimulationListPage />} />
              <Route path="/simulations/:id" element={<SimulationDetailPage />} />
              <Route path="/settings" element={<SettingsLayout />}>
                <Route index element={<Navigate to="config" replace />} />
                <Route path="config" element={<ConfigTab />} />
                <Route path="profile" element={<ProfileTab />} />
                <Route path="skills" element={<SkillsTab />} />
                <Route path="mcp" element={<MCPTab />} />
                <Route path="teams" element={<TeamsTab />} />
                <Route path="projects" element={<ProjectsTab />} />
                <Route path="proxies" element={<ProxiesTab />} />
              </Route>
              <Route path="/iframe/:id" element={<IframePageView />} />
              <Route path="/chat" element={<ChatPage />} />
              <Route path="/chat/:sessionId" element={<ChatPage />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </div>
        </main>

        {/* Mobile bottom navigation tab bar */}
        <MobileNav onMenuClick={() => setMobileMenuOpen(true)} />

        {/* Mobile sidebar (drawer) overlay */}
        {mobileMenuOpen && (
          <>
            <div
              className="fixed inset-0 z-40 bg-background/80 backdrop-blur-sm md:hidden animate-in fade-in duration-200"
              onClick={() => setMobileMenuOpen(false)}
            />
            <div className="fixed inset-y-0 left-0 z-50 md:hidden animate-in slide-in-from-left duration-200">
              <Sidebar mobile onClose={() => setMobileMenuOpen(false)} />
            </div>
          </>
        )}
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
