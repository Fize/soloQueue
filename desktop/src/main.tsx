import { StrictMode } from 'react'
import ReactDOM from 'react-dom/client'
import App from '@/App'
import '@/index.css'

// Intercept fetch calls to target the Go backend port or remote backend dynamically.
const originalFetch = window.fetch
window.fetch = function (input, init) {
  let isRemote = false
  let baseUrl = ''
  let authHeader: string | null = null
  try {
    const connMode = localStorage.getItem('soloqueue_connection_mode')
    const remoteUrl = localStorage.getItem('soloqueue_remote_url')
    if (connMode === 'remote' && remoteUrl) {
      isRemote = true
      baseUrl = remoteUrl.replace(/\/+$/, '')
      const username = localStorage.getItem('soloqueue_remote_username')
      const password = localStorage.getItem('soloqueue_remote_password')
      if (username && password) {
        authHeader = 'Basic ' + btoa(`${username}:${password}`)
      }
    }
  } catch {
    // Ignore error reading localStorage
  }

  const isFileProto = window.location.protocol === 'file:'

  // We only rewrite the URL if we are in remote connection mode OR in Electron packaged mode (file:// protocol)
  if (isRemote || isFileProto) {
    if (!isRemote) {
      const port = (window as any).electronAPI?.backendPort || 57647
      baseUrl = `http://127.0.0.1:${port}`
    }

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
