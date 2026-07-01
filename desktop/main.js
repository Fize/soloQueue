import { app, BrowserWindow, ipcMain } from 'electron'
import path from 'path'
import fs from 'fs'
import http from 'http'
import net from 'net'
import { spawn, execSync } from 'child_process'
import { fileURLToPath } from 'url'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

// Load user shell environment variables (critical for macOS GUI apps to inherit PATH, keys, etc.)
if (process.platform === 'darwin') {
  try {
    const shell = process.env.SHELL || '/bin/zsh'
    const stdout = execSync(`${shell} -l -c "env"`, {
      encoding: 'utf-8',
      timeout: 2000,
      maxBuffer: 1024 * 1024,
    })
    const env = {}
    stdout.split('\n').forEach((line) => {
      const parts = line.split('=')
      if (parts.length >= 2) {
        const key = parts[0]
        const val = parts.slice(1).join('=')
        env[key] = val
      }
    })
    Object.assign(process.env, env)
  } catch (err) {
    console.error('[Electron] Failed to load shell environment:', err)
  }
}

let mainWindow = null
let goProcess = null
let backendStartTime = null
let externalGoInstance = false // Flag to track if Go was already running
const isDev = !app.isPackaged && !process.env.ELECTRON_PROD
const BACKEND_PORT = isDev ? 8765 : 57647

// ── Go binary path resolution ──────────────────────────────
function getGoBinaryPath() {
  const isDev = !app.isPackaged
  if (isDev) {
    // Dev: binary at repo root (../ from desktop/)
    return path.resolve(__dirname, '../soloqueue')
  }
  // Prod: bundled via electron-builder extraResources
  return path.join(process.resourcesPath, 'soloqueue')
}

function getWorkDir() {
  if (process.env.SOLOQUEUE_WORK_DIR) return process.env.SOLOQUEUE_WORK_DIR
  if (!app.isPackaged) {
    // Dev: use project-relative directory, no pollution of production data
    return path.resolve(__dirname, '../.soloqueue-dev')
  }
  return path.join(app.getPath('home'), '.soloqueue')
}

// ── Health check ───────────────────────────────────────────
function checkHealth() {
  return new Promise((resolve) => {
    const req = http.get(`http://127.0.0.1:${BACKEND_PORT}/healthz`, (res) => {
      let data = ''
      res.on('data', (chunk) => { data += chunk })
      res.on('end', () => {
        try {
          const parsed = JSON.parse(data)
          resolve(parsed.status === 'ok')
        } catch {
          resolve(false)
        }
      })
    })
    req.on('error', () => resolve(false))
    req.setTimeout(1000, () => {
      req.destroy()
      resolve(false)
    })
  })
}

// ── TCP Port Check ─────────────────────────────────────────
function checkPortActive(port) {
  return new Promise((resolve) => {
    const socket = new net.Socket()
    socket.setTimeout(1000)
    socket.on('connect', () => {
      socket.destroy()
      resolve(true) // Port is open & listening
    })
    socket.on('timeout', () => {
      socket.destroy()
      resolve(false)
    })
    socket.on('error', () => {
      socket.destroy()
      resolve(false) // Port is closed
    })
    socket.connect(port, '127.0.0.1')
  })
}

// ── Backend lifecycle ──────────────────────────────────────
async function spawnGoBackend() {
  // 1. Check if Go backend is already running on the port
  const isActive = await checkPortActive(BACKEND_PORT)
  if (isActive) {
    const isHealthy = await checkHealth()
    if (isHealthy) {
      console.log(`[Electron] Go backend is already running on port ${BACKEND_PORT}. Bypassing launch.`)
      externalGoInstance = true
      backendStartTime = Date.now() // Estimate uptime start from now
      sendBackendStatus(true)
      return { success: true, external: true }
    }
  }

  // 2. Spawn local child process if not already running
  if (goProcess) return { success: true }
  externalGoInstance = false

  const binary = getGoBinaryPath()
  const workDir = getWorkDir()

  if (!fs.existsSync(binary)) {
    return { success: false, error: `Go binary not found at ${binary}. Run 'make build' first.` }
  }

  // Ensure workDir exists
  fs.mkdirSync(workDir, { recursive: true })

  return new Promise((resolve) => {
    // Bind to 127.0.0.1 via default flag in serve command
    goProcess = spawn(binary, ['serve', '--port', String(BACKEND_PORT), '--host', '127.0.0.1', '--verbose'], {
      cwd: workDir,
      stdio: ['ignore', 'pipe', 'pipe'],
      env: { ...process.env, SOLOQUEUE_WORK_DIR: workDir },
    })

    if (goProcess.stdout) {
      goProcess.stdout.on('data', (data) => {
        const lines = data.toString().split('\n').filter(Boolean)
        lines.forEach((line) => {
          mainWindow?.webContents.send('backend:log', line)
        })
      })
    }
    if (goProcess.stderr) {
      goProcess.stderr.on('data', (data) => {
        const lines = data.toString().split('\n').filter(Boolean)
        lines.forEach((line) => {
          mainWindow?.webContents.send('backend:log', line)
        })
      })
    }

    goProcess.on('exit', () => {
      goProcess = null
      backendStartTime = null
      sendBackendStatus(false)
    })
    goProcess.on('error', () => {
      goProcess = null
      backendStartTime = null
      sendBackendStatus(false)
    })

    // Poll health until ready (max ~10s, 500ms interval)
    let attempts = 0
    const maxAttempts = 20
    const poll = setInterval(async () => {
      attempts++
      const healthy = await checkHealth()
      if (healthy) {
        clearInterval(poll)
        backendStartTime = Date.now()
        sendBackendStatus(true)
        resolve({ success: true })
      } else if (attempts >= maxAttempts) {
        clearInterval(poll)
        killGoProcess()
        sendBackendStatus(false)
        resolve({ success: false, error: 'Backend failed to start within 10 seconds' })
      }
    }, 500)
  })
}

function killGoProcess() {
  if (externalGoInstance) {
    console.log('[Electron] External Go instance detected. Skipping termination.')
    return
  }
  if (!goProcess) return
  try {
    goProcess.kill('SIGTERM')
    setTimeout(() => {
      if (goProcess) {
        try { goProcess.kill('SIGKILL') } catch { /* ignore */ }
      }
    }, 5000)
  } catch { /* ignore */ }
}

function sendBackendStatus(running) {
  mainWindow?.webContents.send('backend:status-changed', {
    running,
    pid: externalGoInstance ? 'EXTERNAL' : (goProcess?.pid || null),
    uptime: running && backendStartTime ? Date.now() - backendStartTime : null,
  })
}

function getBackendStatus() {
  return {
    running: externalGoInstance || goProcess !== null,
    pid: externalGoInstance ? 'EXTERNAL' : (goProcess?.pid || null),
    uptime: (externalGoInstance || goProcess) && backendStartTime ? Date.now() - backendStartTime : null,
  }
}

// ── Minimal TOML parser (for settings.toml model list) ─────
function parseSimpleTOML(text) {
  const lines = text.split('\n')
  const result = {}
  let currentSection = null
  let expectArray = false

  for (const raw of lines) {
    const line = raw.trim()
    if (!line || line.startsWith('#')) continue

    const sectionMatch = line.match(/^\[{1,2}(.+?)]{1,2}$/)
    if (sectionMatch) {
      currentSection = sectionMatch[1].trim()
      expectArray = line.startsWith('[[')
      if (expectArray) {
        if (!result[currentSection]) result[currentSection] = []
        result[currentSection].push({})
      } else {
        result[currentSection] = result[currentSection] || {}
      }
      continue
    }

    if (!currentSection) continue

    const kvMatch = line.match(/^(\w+)\s*=\s*(?:"([^"]*)"|'([^']*)'|(\S+))$/)
    if (kvMatch) {
      const key = kvMatch[1]
      const value = kvMatch[2] ?? kvMatch[3] ?? kvMatch[4]
      if (expectArray && Array.isArray(result[currentSection])) {
        const arr = result[currentSection]
        arr[arr.length - 1][key] = value
      } else if (typeof result[currentSection] === 'object' && !Array.isArray(result[currentSection])) {
        result[currentSection][key] = value
      }
    }
  }
  return result
}

function readAvailableModels() {
  const workDir = getWorkDir()
  const settingsPath = path.join(workDir, 'settings.toml')

  if (!fs.existsSync(settingsPath)) {
    return { found: false, providers: [], models: [] }
  }

  try {
    const text = fs.readFileSync(settingsPath, 'utf-8')
    const parsed = parseSimpleTOML(text)

    const providers = (parsed.providers || []).map((p) => ({
      providerId: p.id || '',
      providerName: p.name || p.id || '',
    }))
    const models = (parsed.models || []).map((m) => ({
      modelId: m.id || '',
      modelName: m.name || m.id || '',
      providerId: m.provider_id || '',
      apiModel: m.api_model || m.id || '',
      contextWindow: parseInt(m.context_window || '0', 10) || 0,
    }))

    return { found: true, providers, models }
  } catch (err) {
    return { found: false, providers: [], models: [], error: err.message }
  }
}

function writeL1Config(modelRef) {
  const workDir = getWorkDir()
  const localPath = path.join(workDir, 'settings.local.toml')

  let existing = ''
  if (fs.existsSync(localPath)) {
    existing = fs.readFileSync(localPath, 'utf-8')
  }

  const lines = existing.split('\n')
  let inDefaultModels = false
  let foundUniversal = false
  const newLines = []

  for (const line of lines) {
    const trimmed = line.trim()
    if (/^\[default_models\]/.test(trimmed)) {
      inDefaultModels = true
      newLines.push(line)
      continue
    }
    if (inDefaultModels && /^\[/.test(trimmed)) {
      inDefaultModels = false
      newLines.push(line)
      continue
    }
    if (inDefaultModels && /^universal\s*=/.test(trimmed)) {
      newLines.push(`universal = '${modelRef}'`)
      foundUniversal = true
      continue
    }
    newLines.push(line)
  }

  if (!foundUniversal) {
    newLines.push('', '[default_models]', `universal = '${modelRef}'`)
  }

  fs.mkdirSync(workDir, { recursive: true })
  fs.writeFileSync(localPath, newLines.join('\n').replace(/^\n+/, '').trim() + '\n')
}

// ── Window creation ────────────────────────────────────────
function createWindow() {
  mainWindow = new BrowserWindow({
    width: 1280,
    height: 800,
    minWidth: 960,
    minHeight: 640,
    titleBarStyle: 'hiddenInset',
    trafficLightPosition: { x: 16, y: 16 },
    icon: fs.existsSync(path.join(__dirname, 'dist/logo.png'))
      ? path.join(__dirname, 'dist/logo.png')
      : path.join(__dirname, 'public/logo.png'),
    backgroundColor: '#5a2800',
    vibrancy: 'under-window',
    webPreferences: {
      nodeIntegration: false,
      contextIsolation: true,
      preload: path.join(__dirname, 'preload.js'),
    },
  })

  mainWindow.on('closed', () => {
    mainWindow = null
  })
}

function loadWindowContent() {
  if (!mainWindow) return
  const isDev = !app.isPackaged && !process.env.ELECTRON_PROD
  if (isDev) {
    mainWindow.loadURL('http://localhost:5173?platform=electron')
    mainWindow.webContents.openDevTools()
  } else {
    mainWindow.loadFile(path.join(__dirname, 'dist/index.html'), {
      query: { platform: 'electron' },
    })
  }
}

ipcMain.on('backend:get-port-sync', (event) => {
  event.returnValue = BACKEND_PORT
})

// ── IPC handlers ───────────────────────────────────────────
ipcMain.handle('backend:start', async () => {
  return await spawnGoBackend()
})

ipcMain.handle('backend:stop', async () => {
  killGoProcess()
  return { success: true }
})

ipcMain.handle('backend:restart', async () => {
  killGoProcess()
  await new Promise((r) => setTimeout(r, 1000))
  return await spawnGoBackend()
})

ipcMain.handle('backend:status', () => {
  return getBackendStatus()
})

// Config
ipcMain.handle('backend:get-available-models', () => {
  return readAvailableModels()
})

ipcMain.handle('backend:save-l1-config', async (_event, modelRef) => {
  writeL1Config(modelRef)
  return { success: true }
})

// Window controls
ipcMain.handle('close-window', () => {
  mainWindow?.close()
})

ipcMain.handle('minimize-window', () => {
  mainWindow?.minimize()
})

ipcMain.handle('maximize-window', () => {
  if (mainWindow?.isMaximized()) {
    mainWindow.unmaximize()
  } else {
    mainWindow?.maximize()
  }
})

// ── App lifecycle ──────────────────────────────────────────
app.whenReady().then(async () => {
  createWindow()

  // Start backend on startup
  try {
    await spawnGoBackend()
  } catch (err) {
    console.error('[Electron] Failed to start backend on startup:', err)
  }

  loadWindowContent()

  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) {
      createWindow()
      loadWindowContent()
    }
  })
})

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') {
    app.quit()
  }
})

app.on('before-quit', () => {
  killGoProcess()
})
