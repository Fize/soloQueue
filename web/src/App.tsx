import { useEffect } from 'react'
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
import { TooltipProvider } from '@/components/ui/tooltip'
import { wsManager } from '@/lib/websocket'

function App() {
  return (
    <TooltipProvider>
      <div className="flex h-screen bg-background">
        <Sidebar />
        <main className="flex flex-1 flex-col min-w-0 overflow-hidden">
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
