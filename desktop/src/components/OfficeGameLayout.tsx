import { useEffect, useState, useCallback, useRef } from 'react'
import { useSimStore } from '../stores/simStore'
import RegistrationScene from './RegistrationScene'
import OfficeScene from './OfficeScene'
import ShopMenu from './ShopMenu'
import { sounds } from '../utils/audio'

// Extend global window interface for Electron API
declare global {
  interface Window {
    electronAPI?: {
      startBackend: () => Promise<{ success: boolean; error?: string }>
      stopBackend: () => Promise<{ success: boolean }>
      restartBackend: () => Promise<{ success: boolean; error?: string }>
      getBackendStatus: () => Promise<{ running: boolean; pid: number | null }>
      onBackendStatusChanged: (callback: (status: { running: boolean; pid: number | string | null }) => void) => () => void
      onBackendLog: (callback: (line: string) => void) => () => void
    }
  }
}

export default function OfficeGameLayout() {
  const { profile, loadProfile, tickSimulation, setBackendStatus, connectToBackend } = useSimStore()
  const backendStatus = useSimStore(s => s.backendStatus)
  const isConnected = useSimStore(s => s.isConnected)

  const [backendLoading, setBackendLoading] = useState(false)
  const [backendError, setBackendError] = useState('')
  const [showShop, setShowShop] = useState(false)
  const backendSpawnedRef = useRef(false)

  // Load profile on mount
  useEffect(() => {
    loadProfile()
  }, [loadProfile])

  // Listen for backend status changes from main process
  useEffect(() => {
    if (typeof window.electronAPI?.onBackendStatusChanged !== 'function') return
    const unsub = window.electronAPI.onBackendStatusChanged((status) => {
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

  // Spawn Go backend asynchronously — called once in background, no waiting screen
  const spawnBackend = useCallback(async () => {
    if (typeof window.electronAPI?.startBackend !== 'function') {
      setBackendStatus('running')
      connectToBackend()
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

  // Auto-start backend on mount (after registration) — fire and forget
  useEffect(() => {
    if (!profile.registered) return
    if (backendSpawnedRef.current) return
    backendSpawnedRef.current = true
    spawnBackend()
  }, [profile.registered, spawnBackend])

  // Simulation tick loop (~60fps)
  useEffect(() => {
    if (!profile.registered) return
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
  }, [profile.registered, tickSimulation])

  // After first-time registration completes: spawn backend immediately
  const handleRegistrationComplete = () => {
    try { sounds.playSelect() } catch {}
    if (typeof window.electronAPI?.startBackend === 'function') {
      setBackendLoading(true)
      setBackendError('')
      setBackendStatus('starting')
      window.electronAPI.startBackend()
        .then((result) => {
          if (result.success) {
            setBackendStatus('running')
            connectToBackend()
          } else {
            setBackendError(result.error || 'Backend failed to start')
            setBackendStatus('error', result.error)
          }
        })
        .catch((err) => {
          setBackendError((err as Error).message)
          setBackendStatus('error', (err as Error).message)
        })
        .finally(() => setBackendLoading(false))
    }
  }

  return (
    <div className="relative w-full h-full flex flex-col overflow-hidden">
      {/* OfficeScene is always the base — no more TitleScene waiting screen */}
      <OfficeScene
        onOpenShop={() => { setShowShop(true); try { sounds.playSelect() } catch {} }}
        backendLoading={backendLoading}
        backendError={backendError}
        onRetryBackend={spawnBackend}
        isConnected={isConnected}
        backendStatus={backendStatus}
      />

      {/* Registration dialog — overlays OfficeScene on first launch */}
      {!profile.registered && (
        <RegistrationScene onComplete={handleRegistrationComplete} />
      )}



      {/* Shop Overlay */}
      {showShop && (
        <ShopMenu onClose={() => { setShowShop(false); try { sounds.playSelect() } catch {} }} />
      )}
    </div>
  )
}
