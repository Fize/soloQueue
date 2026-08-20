import { StrictMode } from 'react'
import ReactDOM from 'react-dom/client'
import App from '@/App'
import '@/index.css'

// Theme initialization — runs before React render to prevent flash
import { getStoredTheme, applyTheme, listenSystemTheme } from '@/lib/theme'
import { useConnectionStore } from '@/stores/connectionStore'

const initial = getStoredTheme()
applyTheme(initial)

// Keep listening even after initial load (handles system-toggle while app is open)
listenSystemTheme()

async function bootstrap() {
  await useConnectionStore.getState().loadConfig()
  ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
    <StrictMode>
      <App />
    </StrictMode>
  )
}

void bootstrap()
