import { StrictMode } from 'react'
import ReactDOM from 'react-dom/client'
import App from '@/App'
import '@/index.css'
import { useConnectionStore } from '@/stores/connectionStore'

// Intercept fetch calls to target the Go backend port or remote backend dynamically.
// Reads connection config from the Zustand connectionStore (single source of truth).

const originalFetch = window.fetch
window.fetch = function (input, init) {
  const state = useConnectionStore.getState()
  const baseUrl = state.getEffectiveBaseUrl()
  const authHeaders = state.getAuthHeaders()

  if (!baseUrl) return originalFetch(input, init)

  let reqUrl = ''
  if (typeof input === 'string') {
    reqUrl = input
  } else if (input instanceof Request) {
    reqUrl = input.url
  } else if (input instanceof URL) {
    reqUrl = input.pathname
  }
  const isBackendCall = reqUrl.startsWith('/api') || reqUrl === '/healthz'

  if (isBackendCall) {
    let req: RequestInfo | URL = input
    if (typeof input === 'string') {
      req = `${baseUrl}${input}`
    } else if (input instanceof Request) {
      req = new Request(`${baseUrl}${input.url}`, input)
    } else if (input instanceof URL) {
      req = new URL(`${baseUrl}${input.pathname}${input.search}`)
    }

    const authHeader = authHeaders['Authorization']
    if (authHeader) {
      if (req instanceof Request) {
        if (!req.headers.has('Authorization')) {
          const headers = new Headers(req.headers)
          headers.set('Authorization', authHeader)
          req = new Request(req, { headers })
        }
      } else {
        const newInit = { ...init }
        const headers = new Headers(newInit.headers)
        if (!headers.has('Authorization')) {
          headers.set('Authorization', authHeader)
        }
        newInit.headers = headers
        init = newInit
      }
    }
    return originalFetch(req, init)
  }

  return originalFetch(input, init)
}

// Theme initialization — runs before React render to prevent flash
import { getStoredTheme, applyTheme, listenSystemTheme } from '@/lib/theme'

const initial = getStoredTheme()
applyTheme(initial)

// Keep listening even after initial load (handles system-toggle while app is open)
listenSystemTheme()

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <StrictMode>
    <App />
  </StrictMode>
)
