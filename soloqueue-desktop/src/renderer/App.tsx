import { useEffect, useState, useCallback, useRef } from 'react'
import { useSimStore } from './store/simStore'
import TitleScene from './components/TitleScene'
import RegistrationScene from './components/RegistrationScene'
import OfficeScene from './components/OfficeScene'
import KanbanBoard from './components/KanbanBoard'
import ShopMenu from './components/ShopMenu'
import { sounds } from './utils/audio'

export default function App() {
  const { profile, loadProfile, tickSimulation, setBackendStatus, connectToBackend } = useSimStore()
  const [scene, setScene] = useState<'title' | 'office'>('title')
  const [backendReady, setBackendReady] = useState(false)
  const [backendLoading, setBackendLoading] = useState(false)
  const [backendError, setBackendError] = useState('')
  const backendSpawnedRef = useRef(false)
  const [showKanban, setShowKanban] = useState(false)
  const [showShop, setShowShop] = useState(false)

  // Load profile on mount
  useEffect(() => {
    loadProfile()
  }, [loadProfile])

  // Listen for backend status changes from main process
  useEffect(() => {
    if (typeof window.electronAPI?.onBackendStatusChanged !== 'function') return
    const unsub = window.electronAPI.onBackendStatusChanged((status) => {
      setBackendReady(status.running)
      setBackendLoading(false)
      if (status.running) {
        setBackendError('')
        setBackendStatus('running')
        connectToBackend()
      } else {
        setBackendStatus('error', 'Backend process exited')
      }
    })
    return unsub
  }, [setBackendStatus, connectToBackend])

  // Spawn Go backend asynchronously (used on subsequent launches)
  const spawnBackend = useCallback(async () => {
    if (typeof window.electronAPI?.startBackend !== 'function') {
      setBackendReady(true)
      return true
    }
    setBackendLoading(true)
    setBackendError('')
    setBackendStatus('starting')
    try {
      const result = await window.electronAPI.startBackend()
      if (!result.success) {
        setBackendError(result.error || 'Unknown error')
        setBackendStatus('error', result.error)
        setBackendLoading(false)
        return false
      }
      setBackendStatus('running')
      connectToBackend()
      return true
    } catch (err) {
      const msg = (err as Error).message || 'Unknown error'
      setBackendError(msg)
      setBackendStatus('error', msg)
      setBackendLoading(false)
      return false
    }
  }, [setBackendStatus, connectToBackend])

  // On subsequent launch (registered), start backend ONCE in background
  useEffect(() => {
    if (!profile.registered || scene !== 'title') return
    if (backendSpawnedRef.current) return
    backendSpawnedRef.current = true
    spawnBackend()
  }, [profile.registered, scene, spawnBackend])

  // Simulation tick loop (runs at ~60fps)
  useEffect(() => {
    if (scene !== 'office' || !profile.registered) return

    let lastTime = performance.now()
    let frameId: number

    const loop = (time: number) => {
      const dt = Math.min((time - lastTime) / 1000, 0.1)
      lastTime = time

      tickSimulation(dt)
      frameId = requestAnimationFrame(loop)
    }

    frameId = requestAnimationFrame(loop)
    return () => cancelAnimationFrame(frameId)
  }, [scene, profile.registered, tickSimulation])

  // Click handler: enter office (first launch, after registration)
  const handleStart = () => {
    sounds.playSelect()
    setScene('office')
  }

  // Registration complete: save L1 config, spawn backend, enter office
  const handleRegistrationComplete = async (modelRef?: string) => {
    sounds.playSelect()

    if (typeof window.electronAPI?.saveL1Config === 'function' && modelRef) {
      try {
        await window.electronAPI.saveL1Config(modelRef)
      } catch { /* ignore */ }
    }

    if (typeof window.electronAPI?.startBackend === 'function') {
      setBackendLoading(true)
      setBackendError('')
      setBackendStatus('starting')
      try {
        const result = await window.electronAPI.startBackend()
        if (result.success) {
          setBackendStatus('running')
          connectToBackend()
        } else {
          setBackendError(result.error || 'Backend failed to start')
          setBackendStatus('error', result.error)
        }
      } catch (err) {
        setBackendError((err as Error).message)
        setBackendStatus('error', (err as Error).message)
      }
      setBackendLoading(false)
    }

    setScene('office')
  }

  return (
    <div className="relative w-screen h-screen flex flex-col overflow-hidden bg-wood-pine">
      {/* Invisible macOS drag region at the top */}
      <div className="electron-drag-region" />

      {/* Main viewport area */}
      <div className="flex-1 relative w-full h-full min-h-0">
        {!profile.registered ? (
          <RegistrationScene onComplete={handleRegistrationComplete} />
        ) : scene === 'title' ? (
          <TitleScene
            onStart={handleStart}
            backendStatus={backendReady ? 'ready' : backendLoading ? 'loading' : 'error'}
            backendError={backendError}
            onRetryBackend={spawnBackend}
          />
        ) : (
          <>
            <OfficeScene
              onOpenKanban={() => { sounds.playSelect(); setShowKanban(true); }}
              onOpenShop={() => { sounds.playSelect(); setShowShop(true); }}
            />
            {!backendReady && (
              <div className="absolute bottom-14 left-0 right-0 z-40 flex justify-center pointer-events-none">
                <div className="bg-berry-red text-parchment px-4 py-2 pixel-border-paper text-[12px] font-retro font-bold flex items-center gap-2 pointer-events-auto">
                  <span>⚠ BACKEND DISCONNECTED</span>
                  <button
                    onClick={async () => {
                      setBackendLoading(true)
                      setBackendError('')
                      if (typeof window.electronAPI?.restartBackend === 'function') {
                        const result = await window.electronAPI.restartBackend()
                        if (result.success) {
                          connectToBackend()
                        } else {
                          setBackendError(result.error || '')
                        }
                      }
                      setBackendLoading(false)
                    }}
                    disabled={backendLoading}
                    className="ml-2 px-3 py-1 bg-crop-green text-charcoal-brown border-2 border-charcoal-brown font-bold hover:brightness-110 disabled:opacity-50"
                  >
                    {backendLoading ? '...' : 'RESTART'}
                  </button>
                </div>
              </div>
            )}
          </>
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
