import { StrictMode } from 'react'
import ReactDOM from 'react-dom/client'
import App from '@/App'
import '@/index.css'

// Intercept all API/WS requests from SoloQueue UI to add a custom header
// This ensures the Go backend knows the requests are native, bypassing proxies.
const originalFetch = window.fetch
window.fetch = function (input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  let url = ''
  if (typeof input === 'string') {
    url = input
  } else if (input instanceof URL) {
    url = input.toString()
  } else if (input instanceof Request) {
    url = input.url
  }

  const isBackend =
    url.startsWith('/api') ||
    url.startsWith(window.location.origin + '/api') ||
    url.startsWith('/ws') ||
    url.startsWith(window.location.origin + '/ws')

  if (isBackend) {
    init = init || {}
    if (input instanceof Request) {
      try {
        input.headers.set('X-SoloQueue-Request', 'true')
      } catch {
        // Fallback if headers are immutable
        const newHeaders = new Headers(input.headers)
        newHeaders.set('X-SoloQueue-Request', 'true')
        init.headers = newHeaders
      }
    } else {
      if (!init.headers) {
        init.headers = { 'X-SoloQueue-Request': 'true' }
      } else if (init.headers instanceof Headers) {
        init.headers.set('X-SoloQueue-Request', 'true')
      } else if (Array.isArray(init.headers)) {
        init.headers.push(['X-SoloQueue-Request', 'true'])
      } else {
        init.headers = {
          ...init.headers,
          'X-SoloQueue-Request': 'true',
        }
      }
    }
  }

  return originalFetch.call(this, input, init)
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
