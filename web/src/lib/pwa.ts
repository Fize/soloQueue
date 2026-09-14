type ServiceWorkerTarget = {
  navigator: Navigator & { serviceWorker?: ServiceWorkerContainer }
}

export async function registerServiceWorker(
  target: ServiceWorkerTarget = window
): Promise<ServiceWorkerRegistration | undefined> {
  if (!target.navigator.serviceWorker) return undefined
  try {
    return await target.navigator.serviceWorker.register('/sw.js')
  } catch {
    // A stale browser, private mode, or a blocked worker must not prevent app startup.
    return undefined
  }
}
