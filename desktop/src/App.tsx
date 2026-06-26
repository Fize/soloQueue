import { useEffect, useState, useRef, useCallback } from 'react'
import { HashRouter, Routes, Route, Navigate } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { PanelLeftClose, PanelRightOpen } from 'lucide-react'
import { Sidebar } from '@/components/Sidebar'
import { AgentDetailPage } from '@/components/AgentDetailPage'
import { CronPage } from '@/components/CronPage'
import { SimulationListPage } from '@/components/SimulationListPage'
import { SimulationDetailPage } from '@/components/SimulationDetailPage'
import { SettingsLayout } from '@/components/SettingsLayout'
import { ConfigTab } from '@/components/settings/ConfigTab/index'
import { ProfileTab } from '@/components/settings/ProfileTab'
import { SkillsTab } from '@/components/settings/SkillsTab/index'
import { MCPTab } from '@/components/settings/MCPTab'
import TeamsTab from '@/components/settings/TeamsTab'
import { ProjectsTab } from '@/components/settings/ProjectsTab'
import { ProxiesTab } from '@/components/settings/ProxiesTab'
import { IframePageView } from '@/components/IframePageView'
import { ChatPage } from '@/components/ChatPage'
import OfficeGameLayout from '@/components/OfficeGameLayout'
import { TooltipProvider } from '@/components/ui/tooltip'
import { Toaster } from 'sonner'
import { wsManager } from '@/lib/websocket'
import { useAuthStore } from '@/stores/authStore'
import { useRuntimeStore } from '@/stores/runtimeStore'

function App() {
  const { isAuthenticated, isLoading } = useAuthStore()
  const sidebarCollapsed = useRuntimeStore((s) => s.sidebarCollapsed)
  const setSidebarCollapsed = useRuntimeStore((s) => s.setSidebarCollapsed)
  const inspectorPanelWidth = useRuntimeStore((s) => s.inspectorPanelWidth)
  const [isHovered, setIsHovered] = useState(false)
  const hoverTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const effectivelyCollapsed = sidebarCollapsed && !isHovered
  const floating = sidebarCollapsed && isHovered

  const handleHoverEnter = useCallback(() => {
    if (hoverTimeoutRef.current) {
      clearTimeout(hoverTimeoutRef.current)
      hoverTimeoutRef.current = null
    }
    if (sidebarCollapsed) setIsHovered(true)
  }, [sidebarCollapsed])

  const handleHoverLeave = useCallback(() => {
    if (hoverTimeoutRef.current) clearTimeout(hoverTimeoutRef.current)
    hoverTimeoutRef.current = setTimeout(() => setIsHovered(false), 200)
  }, [])

  const toggleCollapse = useCallback(() => {
    setSidebarCollapsed(!sidebarCollapsed)
    setIsHovered(false)
  }, [sidebarCollapsed, setSidebarCollapsed])

  if (isLoading) {
    return (
      <div className="flex h-screen items-center justify-center bg-background">
        <div className="text-sm text-muted-foreground font-mono">Loading SoloQueue...</div>
      </div>
    )
  }

  if (!isAuthenticated) {
    return (
      <div className="flex h-screen flex-col items-center justify-center bg-background text-foreground gap-4">
        <div className="text-sm font-semibold font-mono">Authentication Required</div>
        <button
          onClick={() => window.location.reload()}
          className="px-4 py-2 bg-primary text-primary-foreground rounded-lg text-sm font-medium hover:bg-primary/90 transition-colors"
        >
          Login / Re-authenticate
        </button>
      </div>
    )
  }

  return (
    <TooltipProvider>
      <Toaster
        position="top-center"
        toastOptions={{
          className: 'text-sm font-medium bg-card border border-border text-foreground rounded-lg shadow-lg',
        }}
      />
      <div className="flex h-screen w-screen bg-background overflow-hidden select-none relative">
        {/* Independent collapse toggle button: lives in its own fixed wrapper so it
             stays above all other drag regions. Button itself has electron-no-drag to stay clickable. */}
        <div className="absolute left-[70px] top-0 z-[100] h-12 w-[45px] flex items-center justify-center electron-no-drag">
          <button
            onClick={toggleCollapse}
            onMouseEnter={handleHoverEnter}
            onMouseLeave={handleHoverLeave}
            className="flex items-center justify-center rounded-md p-1.5 transition-colors duration-150 hover:bg-foreground/10 text-muted-foreground hover:text-foreground shrink-0 cursor-pointer"
            title={sidebarCollapsed ? '展开侧边栏' : '收起侧边栏'}
          >
            {sidebarCollapsed ? (
              <PanelRightOpen className="h-4 w-4 pointer-events-none" />
            ) : (
              <PanelLeftClose className="h-4 w-4 pointer-events-none" />
            )}
          </button>
        </div>

        {/* Main layout: Sidebar (translucent, height 100vh) + Content Pane */}
        <div className="flex flex-1 min-h-0 overflow-hidden">
          {/* macOS Style Sidebar wrapper - hover zone triggers popup expand */}
          <div
            className={cn(
              'shrink-0 h-full border-r transition-all duration-300 ease-out relative',
              sidebarCollapsed
                ? 'w-0 border-transparent bg-transparent'
                : 'w-[220px] border-border/40 bg-card/40 backdrop-blur-md overflow-hidden'
            )}
            onMouseEnter={handleHoverEnter}
            onMouseLeave={handleHoverLeave}
          >
            {sidebarCollapsed && (
              <div
                className="absolute left-0 top-0 bottom-0 w-3 z-30"
                onMouseEnter={handleHoverEnter}
              />
            )}
            <Sidebar narrow={effectivelyCollapsed} floating={floating} />
          </div>

          {/* Main content pane */}
          <main className="flex flex-1 flex-col min-w-0 overflow-hidden h-full bg-background relative">
            {/* Title Bar drag region overlay */}
            <div className="absolute top-0 left-0 right-0 h-12 z-50 pointer-events-none">
              {sidebarCollapsed ? (
                <>
                  <div className="absolute left-0 top-0 w-[70px] h-full electron-drag-region" />
                  <div
                    className="absolute left-[115px] top-0 h-full electron-drag-region"
                    style={{ right: inspectorPanelWidth }}
                  />
                </>
              ) : (
                <div
                  className="absolute left-0 top-0 h-full electron-drag-region"
                  style={{ right: inspectorPanelWidth }}
                />
              )}
            </div>

            {/* Routes */}
            <div className="flex-1 overflow-hidden h-full">
              <Routes>
                <Route path="/" element={<Navigate to="/office" replace />} />
                <Route path="/office" element={<OfficeGameLayout />} />
                <Route path="/chat" element={<ChatPage />} />
                <Route path="/chat/:sessionId" element={<ChatPage />} />
                <Route path="/agents/:id" element={<AgentDetailPage />} />
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
                <Route path="*" element={<Navigate to="/" replace />} />
              </Routes>
            </div>
          </main>
        </div>
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
        <div className="text-sm text-muted-foreground font-mono">Initializing Application...</div>
      </div>
    )
  }

  // Use HashRouter for native Electron file:// compatibility
  return (
    <HashRouter>
      <App />
    </HashRouter>
  )
}
