import { useEffect, useState } from 'react'
import { useSimStore } from './store/simStore'
import TitleScene from './components/TitleScene'
import RegistrationScene from './components/RegistrationScene'
import OfficeScene from './components/OfficeScene'
import KanbanBoard from './components/KanbanBoard'
import ShopMenu from './components/ShopMenu'
import { sounds } from './utils/audio'

export default function App() {
  const { profile, loadProfile, tickSimulation } = useSimStore()
  const [scene, setScene] = useState<'title' | 'office'>('title')
  const [showKanban, setShowKanban] = useState(false)
  const [showShop, setShowShop] = useState(false)

  // Load profile on mount
  useEffect(() => {
    loadProfile()
  }, [loadProfile])

  // Simulation tick loop (runs at ~60fps)
  useEffect(() => {
    if (scene !== 'office' || !profile.registered) return

    let lastTime = performance.now()
    let frameId: number

    const loop = (time: number) => {
      const dt = Math.min((time - lastTime) / 1000, 0.1) // cap delta-time to avoid huge leaps
      lastTime = time
      
      tickSimulation(dt)
      frameId = requestAnimationFrame(loop)
    }

    frameId = requestAnimationFrame(loop)
    return () => cancelAnimationFrame(frameId)
  }, [scene, profile.registered, tickSimulation])

  // Click handler wrapper that plays retro sound
  const handleStart = () => {
    sounds.playSelect()
    setScene('office')
  }

  return (
    <div className="relative w-screen h-screen flex flex-col overflow-hidden bg-wood-pine">
      {/* Invisible macOS drag region at the top */}
      <div className="electron-drag-region" />

      {/* Main viewport area */}
      <div className="flex-1 relative w-full h-full min-h-0">
        {!profile.registered ? (
          <RegistrationScene onComplete={handleStart} />
        ) : scene === 'title' ? (
          <TitleScene onStart={handleStart} />
        ) : (
          <OfficeScene 
            onOpenKanban={() => { sounds.playSelect(); setShowKanban(true); }}
            onOpenShop={() => { sounds.playSelect(); setShowShop(true); }}
          />
        )}
      </div>

      {/* Kanban Overlay Modals */}
      {showKanban && (
        <KanbanBoard onClose={() => { sounds.playSelect(); setShowKanban(false); }} />
      )}

      {/* Shop Overlay Modals */}
      {showShop && (
        <ShopMenu onClose={() => { sounds.playSelect(); setShowShop(false); }} />
      )}
    </div>
  )
}
