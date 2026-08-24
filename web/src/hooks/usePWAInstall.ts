import { useCallback, useEffect, useRef, useState } from 'react'
import {
  captureBeforeInstallPrompt,
  consumeBeforeInstallPrompt,
  isInstallDismissed,
  isStandaloneDisplayMode,
  persistInstallDismissal,
  supportsServiceWorker,
  type BeforeInstallPromptEvent,
  type PWAInstallStatus,
} from '@/lib/pwa'

export interface PWAInstallState {
  status: PWAInstallStatus
  guideOpen: boolean
  install: () => Promise<boolean>
  dismiss: () => void
  showGuide: () => void
  closeGuide: () => void
}

function getInitialStatus(): PWAInstallStatus {
  if (isStandaloneDisplayMode()) return 'installed'
  if (isInstallDismissed()) return 'dismissed'
  if (!supportsServiceWorker()) return 'unsupported'
  return 'manual'
}

export function usePWAInstall(): PWAInstallState {
  const [status, setStatus] = useState<PWAInstallStatus>('checking')
  const [guideOpen, setGuideOpen] = useState(false)
  const deferredPrompt = useRef<BeforeInstallPromptEvent | null>(null)
  const statusRef = useRef<PWAInstallStatus>('checking')

  useEffect(() => {
    captureBeforeInstallPrompt()

    const handleBeforeInstallPrompt = (event: Event) => {
      if (
        isInstallDismissed() ||
        statusRef.current === 'installed' ||
        statusRef.current === 'dismissed'
      ) {
        return
      }
      deferredPrompt.current = event as BeforeInstallPromptEvent
      statusRef.current = 'available'
      setStatus('available')
    }
    const onBeforeInstallPrompt = (event: Event) => {
      // The early module-level listener owns preventDefault; this listener only updates React state.
      handleBeforeInstallPrompt(consumeBeforeInstallPrompt() ?? event)
    }
    const onAppInstalled = () => {
      deferredPrompt.current = null
      statusRef.current = 'installed'
      setStatus('installed')
      setGuideOpen(false)
    }

    window.addEventListener('beforeinstallprompt', onBeforeInstallPrompt)
    window.addEventListener('appinstalled', onAppInstalled)
    const initialStatus = getInitialStatus()
    statusRef.current = initialStatus
    setStatus(initialStatus)
    const bufferedPrompt = consumeBeforeInstallPrompt()
    if (bufferedPrompt) handleBeforeInstallPrompt(bufferedPrompt)

    return () => {
      window.removeEventListener('beforeinstallprompt', onBeforeInstallPrompt)
      window.removeEventListener('appinstalled', onAppInstalled)
    }
  }, [])

  const install = useCallback(async () => {
    const prompt = deferredPrompt.current
    if (!prompt) return false
    try {
      await prompt.prompt()
      const choice = await prompt.userChoice
      deferredPrompt.current = null
      if (choice.outcome === 'accepted') {
        statusRef.current = 'installed'
        setStatus('installed')
        setGuideOpen(false)
        return true
      }
    } catch {
      // Browsers can reject a prompt after its activation window closes.
    }
    deferredPrompt.current = null
    statusRef.current = 'manual'
    setStatus('manual')
    return false
  }, [])

  const dismiss = useCallback(() => {
    persistInstallDismissal()
    deferredPrompt.current = null
    setGuideOpen(false)
    statusRef.current = 'dismissed'
    setStatus('dismissed')
  }, [])

  const showGuide = useCallback(() => setGuideOpen(true), [])
  const closeGuide = useCallback(() => setGuideOpen(false), [])

  return { status, guideOpen, install, dismiss, showGuide, closeGuide }
}
