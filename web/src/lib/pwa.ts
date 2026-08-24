export type PWAInstallStatus = 'checking' | 'available' | 'manual' | 'unsupported' | 'installed' | 'dismissed'

export interface BeforeInstallPromptEvent extends Event {
  prompt: () => Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed'; platform: string }>
}

export const PWA_DISMISSAL_KEY = 'soloqueue_pwa_install_dismissed'

type InstallPromptTarget = Pick<Window, 'addEventListener' | 'removeEventListener'>

type InstallPromptCapture = {
  event: BeforeInstallPromptEvent | null
}

const installPromptCaptures = new WeakMap<object, InstallPromptCapture>()

/** Start the one global listener early so bootstrap cannot miss the one-shot event. */
export function captureBeforeInstallPrompt(target: InstallPromptTarget = window): void {
  if (installPromptCaptures.has(target)) return

  const capture: InstallPromptCapture = { event: null }
  target.addEventListener('beforeinstallprompt', (event) => {
    event.preventDefault()
    if (!capture.event) capture.event = event as BeforeInstallPromptEvent
  })
  installPromptCaptures.set(target, capture)
}

export function consumeBeforeInstallPrompt(
  target: InstallPromptTarget = window
): BeforeInstallPromptEvent | null {
  const capture = installPromptCaptures.get(target)
  if (!capture) return null
  const event = capture.event
  capture.event = null
  return event
}

type PWAWindow = {
  matchMedia: (query: string) => MediaQueryList
  navigator: Navigator & { standalone?: boolean }
  localStorage?: Storage
}

export function isStandaloneDisplayMode(target: PWAWindow = window): boolean {
  return target.matchMedia('(display-mode: standalone)').matches || target.navigator.standalone === true
}

export function isInstallDismissed(target: PWAWindow = window): boolean {
  try {
    return target.localStorage?.getItem(PWA_DISMISSAL_KEY) === 'true'
  } catch {
    return false
  }
}

export function persistInstallDismissal(target: PWAWindow = window): void {
  try {
    target.localStorage?.setItem(PWA_DISMISSAL_KEY, 'true')
  } catch {
    // Private browsing can reject storage; install behavior remains usable.
  }
}

export function supportsServiceWorker(target: PWAWindow = window): boolean {
  return Boolean((target.navigator as Navigator & { serviceWorker?: ServiceWorkerContainer }).serviceWorker)
}

export async function registerServiceWorker(
  target: PWAWindow & { navigator: Navigator & { serviceWorker?: ServiceWorkerContainer } } = window
): Promise<ServiceWorkerRegistration | undefined> {
  if (!target.navigator.serviceWorker) return undefined
  try {
    return await target.navigator.serviceWorker.register('/sw.js')
  } catch {
    // A stale browser, private mode, or a blocked worker must not prevent app startup.
    return undefined
  }
}
