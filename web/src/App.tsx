import { useEffect } from 'react'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { Header } from '@/components/Header'
import { RuntimeStatusBar } from '@/components/RuntimeStatusBar'
import { Container } from '@/components/ui/Container'
import { HomePage } from '@/components/HomePage'
import { SettingsView } from '@/components/SettingsView'
import { FilesPage } from '@/components/FilesPage'
import { TooltipProvider } from '@/components/ui/tooltip'
import { wsManager } from '@/lib/websocket'

export type AppTab = 'home' | 'files' | 'settings'

function App() {
  return (
    <TooltipProvider>
      <div className="flex h-screen flex-col bg-background">
        <Header />
        <RuntimeStatusBar />
        <main className="flex-1 overflow-hidden">
          <Container className="h-full pb-4 sm:pb-6">
            <Routes>
              <Route path="/" element={<HomePage />} />
              <Route path="/files" element={<FilesPage />} />
              <Route path="/settings/*" element={<SettingsView />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </Container>
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
