import { app, BrowserWindow, dialog, ipcMain, Menu, safeStorage, session, shell, Tray, nativeImage } from 'electron'
import path from 'path'
import fs from 'fs'
import http from 'http'
import net from 'net'
import { spawn, execSync } from 'child_process'
import { fileURLToPath } from 'url'
import os from 'os'
import {
  buildBasicAuthHeader,
  hasAuthorizationHeader,
  normalizeRemoteOrigin,
  shouldInjectRemoteAuth,
} from './electron/remote-auth-policy.mjs'
import { isBackendReady } from './electron/backend-status.mjs'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

// Load user shell environment variables (critical for macOS GUI apps to inherit PATH, keys, etc.)
if (process.platform === 'darwin') {
  try {
    const shell = process.env.SHELL || '/bin/zsh'
    const shellName = path.basename(shell)
    const homeDir = os.homedir()
    let command = 'env'

    if (shellName === 'zsh') {
      const zprofile = path.join(homeDir, '.zprofile')
      const zshrc = path.join(homeDir, '.zshrc')
      let sourceCmds = []
      if (fs.existsSync(zprofile)) sourceCmds.push(`source "${zprofile}"`)
      if (fs.existsSync(zshrc)) sourceCmds.push(`source "${zshrc}"`)
      if (sourceCmds.length > 0) {
        command = `${sourceCmds.join(' && ')} && env`
      }
    } else if (shellName === 'bash') {
      const bashProfile = path.join(homeDir, '.bash_profile')
      const profile = path.join(homeDir, '.profile')
      const bashrc = path.join(homeDir, '.bashrc')
      let sourceCmds = []
      if (fs.existsSync(bashProfile)) sourceCmds.push(`source "${bashProfile}"`)
      else if (fs.existsSync(profile)) sourceCmds.push(`source "${profile}"`)
      if (fs.existsSync(bashrc)) sourceCmds.push(`source "${bashrc}"`)
      if (sourceCmds.length > 0) {
        command = `${sourceCmds.join(' && ')} && env`
      }
    } else if (shellName === 'fish') {
      const fishConfig = path.join(homeDir, '.config/fish/config.fish')
      if (fs.existsSync(fishConfig)) {
        command = `source "${fishConfig}"; and env`
      }
    }

    let stdout = ''
    try {
      stdout = execSync(command, {
        shell: shell,
        encoding: 'utf-8',
        timeout: 3000,
        maxBuffer: 1024 * 1024,
      })
    } catch (cmdErr) {
      console.warn('[Electron] Sourcing custom shell rc failed, falling back to basic env:', cmdErr)
      stdout = execSync('env', {
        shell: shell,
        encoding: 'utf-8',
        timeout: 2000,
        maxBuffer: 1024 * 1024,
      })
    }

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

    // Fallback: Ensure common macOS developer paths are present in PATH
    const standardPaths = ['/opt/homebrew/bin', '/opt/homebrew/sbin', '/usr/local/bin']
    const currentPaths = (process.env.PATH || '').split(':')
    standardPaths.forEach((p) => {
      if (p && !currentPaths.includes(p)) {
        currentPaths.unshift(p)
      }
    })
    process.env.PATH = currentPaths.join(':')
  } catch (err) {
    console.error('[Electron] Failed to load shell environment:', err)
  }
}

let mainWindow = null
let goProcess = null
let backendStartTime = null
let externalGoInstance = false // Flag to track if Go was already running
let backendStartPromise = null
let intentionalClose = false
let restartAttempts = 0
const MAX_RESTART_ATTEMPTS = 5
let restartTimer = null
const isDev = !app.isPackaged && !process.env.ELECTRON_PROD
const BACKEND_PORT = isDev ? 8765 : 57647

// ── macOS Menu Bar Tray (Connection Status) ──────────────────
// Connection state lives in the renderer (Zustand store backed by localStorage +
// Electron backend status). The renderer pushes the current state to the main
// process via `tray:update-status` IPC; main process updates the tray icon,
// tooltip, and context menu accordingly.
//
// Why this design: HIG has no in-window "top status bar" component. macOS apps
// surface ambient state (network, sync, availability) in the system menu bar
// (see Slack/Discord/Spotify). This matches the platform idiom and removes
// the in-app 4px strip that was creating a 4px baseline misalignment with the
// sidebar's traffic-light spacer.

let tray = null
let pendingTrayStatus = null
let isQuitting = false

// 16x16 monochrome template PNGs (base64). macOS auto-recolors template images
// to match the menu-bar appearance (light/dark). Three states: ok / warn / err.
const TRAY_ICONS = {
  ok: 'iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAYAAAAf8/9hAAAAKklEQVR4nGNgGBHgP7macGGKNBNlyMAaQIxmqhgyiMOAGENIBmRpoh8AAJfUULCLnTuwAAAAAElFTkSuQmCC',
  warn: 'iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAYAAAAf8/9hAAAAKElEQVR4nGNgGLbgPw5MkWaiDMGnkChDCCkYSQaQHYjoCqmeFoYjAAAFQzfJHJQvAQAAAABJRU5ErkJggg==',
  err: 'iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAYAAAAf8/9hAAAAMElEQVR4nGNgGHbgP4XyeBURpRmXYpI0o2siSzNFNlPFBRSFAUWxQJV0QKklQw0AAGmyEe8LI08gAAAAAElFTkSuQmCC',
}

function makeTrayImage(name) {
  const buf = Buffer.from(TRAY_ICONS[name] || TRAY_ICONS.ok, 'base64')
  const img = nativeImage.createFromBuffer(buf)
  // Mark as template image so macOS handles dark/light mode recoloring
  // and so the icon does not "stick out" from the menu bar.
  if (process.platform === 'darwin') {
    img.setTemplateImage(true)
  }
  return img
}

function pickTrayIconKey(status) {
  if (!status) return 'ok'
  if (status.connectionError) return 'err'
  if (status.mode === 'remote' && !status.hasUrl) return 'warn'
  if (status.mode === 'local' && status.isChecking && !status.backendRunning) return 'warn'
  return 'ok'
}

function buildTrayTooltip(status) {
  if (!status) return 'SoloQueue'
  if (status.connectionError) {
    return `SoloQueue — Error: ${status.connectionError.slice(0, 50)}`
  }
  if (status.mode === 'remote') {
    if (status.hasUrl) return `SoloQueue — Remote: ${status.remoteUrl}`
    return 'SoloQueue — Remote (no URL configured)'
  }
  if (status.isChecking && !status.backendRunning) {
    return 'SoloQueue — Starting backend...'
  }
  if (status.backendRunning) {
    const sec = Math.floor((status.uptime || 0) / 1000)
    const fmt = (v) =>
      v >= 1_000_000
        ? `${(v / 1_000_000).toFixed(1)}M`
        : v >= 1_000
          ? `${(v / 1_000).toFixed(1)}k`
          : String(v)
    const prompt = fmt(status.promptTokens || 0)
    const output = fmt(status.outputTokens || 0)
    return `SoloQueue — ${sec}s | P:${prompt} O:${output}`
  }
  return 'SoloQueue — Backend not running'
}

function focusOrCreateWindow() {
  if (mainWindow) {
    if (mainWindow.isMinimized()) mainWindow.restore()
    if (!mainWindow.isVisible()) mainWindow.show()
    mainWindow.focus()
  } else {
    createWindow()
    loadWindowContent()
  }
}

function buildTrayMenu(status) {
  const lines = []
  if (status?.connectionError) {
    lines.push({ label: `⚠ ${status.connectionError.slice(0, 60)}`, enabled: false })
  } else if (status?.mode === 'remote') {
    if (status.hasUrl) {
      lines.push({ label: `🔗 ${status.remoteUrl}`, enabled: false })
    } else {
      lines.push({ label: '⚠ Remote mode — no URL configured', enabled: false })
    }
  } else if (status?.isChecking && !status.backendRunning) {
    lines.push({ label: '⏳ Starting local backend...', enabled: false })
  } else if (status?.backendRunning) {
    const fmt = (v) =>
      v >= 1_000_000
        ? `${(v / 1_000_000).toFixed(1)}M`
        : v >= 1_000
          ? `${(v / 1_000).toFixed(1)}k`
          : String(v)
    const prompt = fmt(status.promptTokens || 0)
    const output = fmt(status.outputTokens || 0)
    const cache = fmt(status.cacheHitTokens || 0)
    lines.push({ label: `✓ Running  P:${prompt}  O:${output}`, enabled: false })
    if (Number(status.cacheHitTokens || 0) > 0) {
      lines.push({ label: `  Cache hit: ${cache}`, enabled: false })
    }
  } else {
    lines.push({ label: '○ Backend not running', enabled: false })
  }

  return Menu.buildFromTemplate([
    ...lines,
    { type: 'separator' },
    { label: 'Show SoloQueue', click: focusOrCreateWindow },
    { type: 'separator' },
    {
      // Custom click handler instead of { role: 'quit' } so we can set
      // isQuitting=true before calling app.quit(). On Windows/Linux the
      // close-event interceptor in createWindow() checks this flag to
      // decide whether to hide the window (minimize-to-tray) or actually
      // allow the close to proceed.
      label: process.platform === 'darwin' ? 'Quit SoloQueue' : 'Exit',
      click: () => {
        isQuitting = true
        app.quit()
      },
    },
  ])
}

function updateTray(status) {
  if (!tray) return
  try {
    tray.setImage(makeTrayImage(pickTrayIconKey(status)))
    tray.setToolTip(buildTrayTooltip(status))
    tray.setContextMenu(buildTrayMenu(status))
  } catch (err) {
    console.error('[Electron] Failed to update tray:', err)
  }
}

function createTray() {
  if (tray) return
  try {
    tray = new Tray(makeTrayImage('ok'))
    // macOS: left-click focuses window, right-click shows menu (default)
    // Windows/Linux: left-click shows menu
    tray.on('click', focusOrCreateWindow)
    if (pendingTrayStatus) {
      updateTray(pendingTrayStatus)
      pendingTrayStatus = null
    }
  } catch (err) {
    console.error('[Electron] Failed to create tray icon:', err)
    tray = null
  }
}

function destroyTray() {
  if (tray) {
    try {
      tray.destroy()
    } catch {
      /* ignore */
    }
    tray = null
  }
}

// Renderer pushes connection state here. We buffer the latest status if the
// tray hasn't been created yet (e.g., early messages during app startup).
ipcMain.on('tray:update-status', (_event, status) => {
  if (tray) {
    updateTray(status)
  } else {
    pendingTrayStatus = status
  }
})

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
      res.on('data', (chunk) => {
        data += chunk
      })
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

function waitForProcessExit(child, timeoutMs = 5000) {
  if (child.exitCode !== null || child.signalCode !== null) return Promise.resolve()
  return new Promise((resolve) => {
    let timer = null
    const done = () => {
      if (timer) clearTimeout(timer)
      child.removeListener('exit', done)
      child.removeListener('error', done)
      resolve()
    }
    child.once('exit', done)
    child.once('error', done)
    timer = setTimeout(done, timeoutMs)
  })
}

// ── Backend lifecycle ──────────────────────────────────────
function spawnGoBackend() {
  // Startup can be requested by the renderer and by the app lifecycle at the
  // same time. Share one promise so a second request waits for the same health
  // check instead of returning success while the first process is still
  // starting.
  if (backendStartPromise) return backendStartPromise

  const promise = startGoBackend()
  const wrappedPromise = promise.finally(() => {
    if (backendStartPromise === wrappedPromise) backendStartPromise = null
  })
  backendStartPromise = wrappedPromise
  return wrappedPromise
}

async function startGoBackend() {
  intentionalClose = false
  // 1. Check if Go backend is already running on the port
  const isActive = await checkPortActive(BACKEND_PORT)
  if (isActive) {
    const isHealthy = await checkHealth()
    if (isHealthy) {
      console.log(
        `[Electron] Go backend is already running on port ${BACKEND_PORT}. Bypassing launch.`
      )
      externalGoInstance = true
      backendStartTime = Date.now() // Estimate uptime start from now
      sendBackendStatus(true)
      return { success: true, external: true }
    }
  }

  // 2. Spawn local child process if not already running
  if (goProcess) {
    const healthy = await checkHealth()
    if (healthy) {
      backendStartTime ||= Date.now()
      sendBackendStatus(true)
      return { success: true }
    }
    if (goProcess.killed || goProcess.exitCode !== null || goProcess.signalCode !== null) {
      const stoppingProcess = goProcess
      await waitForProcessExit(stoppingProcess)
      if (goProcess === stoppingProcess) goProcess = null
    } else {
      return { success: false, error: 'Backend is already starting' }
    }
  }
  externalGoInstance = false

  const binary = getGoBinaryPath()
  const workDir = getWorkDir()

  if (!fs.existsSync(binary)) {
    return { success: false, error: `Go binary not found at ${binary}. Run 'make build' first.` }
  }

  // Ensure workDir exists
  fs.mkdirSync(workDir, { recursive: true })

  // Create logs directory for stderr capture (crash diagnostics)
  const logsDir = path.join(workDir, 'logs')
  fs.mkdirSync(logsDir, { recursive: true })
  const stderrPath = path.join(logsDir, 'stderr.log')
  let stderrFd
  try {
    stderrFd = fs.openSync(stderrPath, 'a')
  } catch {
    console.warn(`[Electron] Cannot open stderr log: ${stderrPath}`)
  }

  return new Promise((resolve) => {
    let poll = null
    let settled = false
    const finish = (result) => {
      if (settled) return
      settled = true
      if (poll) clearInterval(poll)
      resolve(result)
    }

    backendStartTime = null
    sendBackendStatus(false)

    // Bind to 127.0.0.1 via default flag in serve command
    goProcess = spawn(
      binary,
      ['serve', '--port', String(BACKEND_PORT), '--host', '127.0.0.1', '--verbose'],
      {
        cwd: workDir,
        stdio: ['ignore', 'ignore', stderrFd || 'ignore'],
        env: { ...process.env, SOLOQUEUE_WORK_DIR: workDir, GOTRACEBACK: 'crash' },
      }
    )

    const handleUnexpectedExit = (code, signal) => {
      goProcess = null
      backendStartTime = null
      sendBackendStatus(false)

      if (!intentionalClose && !externalGoInstance) {
        if (restartAttempts < MAX_RESTART_ATTEMPTS) {
          restartAttempts++
          const delay = Math.min(1000 * Math.pow(2, restartAttempts - 1), 10000)
          console.log(
            `[Electron] Go backend exited unexpectedly (code: ${code}, signal: ${signal}). Auto-restarting in ${delay}ms... (attempt ${restartAttempts}/${MAX_RESTART_ATTEMPTS})`
          )

          if (restartTimer) clearTimeout(restartTimer)
          restartTimer = setTimeout(async () => {
            const res = await spawnGoBackend()
            if (res.success) {
              console.log(`[Electron] Go backend successfully restarted by watchdog.`)
              restartAttempts = 0
            }
          }, delay)
        } else {
          console.error(
            `[Electron] Go backend failed to restart after ${MAX_RESTART_ATTEMPTS} attempts. Auto-restart disabled.`
          )
        }
      }
    }

    goProcess.on('exit', (code, signal) => {
      handleUnexpectedExit(code, signal)
      if (!backendStartTime) {
        finish({
          success: false,
          error: `Backend exited before becoming ready${code === null ? '' : ` (code ${code})`}`,
        })
      }
    })
    goProcess.on('error', (err) => {
      console.error(`[Electron] Go backend process error:`, err)
      handleUnexpectedExit(null, null)
      finish({ success: false, error: `Failed to start backend: ${err.message}` })
    })

    // Poll health until ready (max ~10s, 500ms interval)
    let attempts = 0
    const maxAttempts = 20
    poll = setInterval(async () => {
      attempts++
      const healthy = await checkHealth()
      if (healthy) {
        backendStartTime = Date.now()
        sendBackendStatus(true)
        finish({ success: true })
      } else if (attempts >= maxAttempts) {
        // A timed-out startup is an intentional stop. Do not let the crash
        // watchdog immediately race the user's next explicit Start action.
        intentionalClose = true
        killGoProcess()
        backendStartTime = null
        sendBackendStatus(false)
        finish({ success: false, error: 'Backend failed to start within 10 seconds' })
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
        try {
          goProcess.kill('SIGKILL')
        } catch {
          /* ignore */
        }
      }
    }, 5000)
  } catch {
    /* ignore */
  }
}

function sendBackendStatus(running) {
  mainWindow?.webContents.send('backend:status-changed', {
    running,
    pid: externalGoInstance ? 'EXTERNAL' : goProcess?.pid || null,
    uptime: running && backendStartTime ? Date.now() - backendStartTime : null,
  })
}

function getBackendStatus() {
  const running = isBackendReady({
    externalGoInstance,
    backendStartTime,
  })
  return {
    running,
    pid: running ? (externalGoInstance ? 'EXTERNAL' : goProcess?.pid || null) : null,
    uptime:
      running && backendStartTime ? Date.now() - backendStartTime : null,
  }
}

// ── Window creation ────────────────────────────────────────
function createWindow() {
  mainWindow = new BrowserWindow({
    width: 1440,
    height: 960,
    minWidth: 1024,
    minHeight: 768,
    titleBarStyle: 'hiddenInset',
    trafficLightPosition: { x: 16, y: 16 },
    icon: fs.existsSync(path.join(__dirname, 'dist/logo.png'))
      ? path.join(__dirname, 'dist/logo.png')
      : path.join(__dirname, 'public/logo.png'),
    backgroundColor: '#0F1117',
    vibrancy: 'under-window',
    webPreferences: {
      nodeIntegration: false,
      contextIsolation: true,
      preload: path.join(__dirname, 'preload.js'),
    },
  })

  // Register shortcut to toggle DevTools (Cmd+Option+I on Mac, Ctrl+Shift+I on other platforms)
  mainWindow.webContents.on('before-input-event', (event, input) => {
    const isMac = process.platform === 'darwin'
    const toggleDevTools = isMac
      ? input.meta && input.alt && input.code.toLowerCase() === 'keyi'
      : input.control && input.shift && input.code.toLowerCase() === 'keyi'

    if (toggleDevTools && input.type === 'keyDown') {
      mainWindow.webContents.toggleDevTools()
      event.preventDefault()
    }
  })

  mainWindow.on('closed', () => {
    mainWindow = null
  })

  // Windows/Linux: minimize to tray instead of closing the window.
  // On macOS, the app stays alive after all windows are closed by convention;
  // the close event is not intercepted, so the window closes normally and
  // the tray persists.
  mainWindow.on('close', (event) => {
    if (process.platform !== 'darwin' && !isQuitting) {
      event.preventDefault()
      mainWindow.hide()
    }
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
  restartAttempts = 0
  return await spawnGoBackend()
})

ipcMain.handle('backend:stop', async () => {
  intentionalClose = true
  if (restartTimer) {
    clearTimeout(restartTimer)
    restartTimer = null
  }
  killGoProcess()
  return { success: true }
})

ipcMain.handle('backend:restart', async () => {
  intentionalClose = false
  restartAttempts = 0
  if (restartTimer) {
    clearTimeout(restartTimer)
    restartTimer = null
  }
  killGoProcess()
  await new Promise((r) => setTimeout(r, 1000))
  return await spawnGoBackend()
})

ipcMain.handle('backend:status', () => {
  return getBackendStatus()
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

// Directory picker (native OS dialog)
ipcMain.handle('dialog:select-directory', async () => {
  if (!mainWindow) return null
  const result = await dialog.showOpenDialog(mainWindow, {
    properties: ['openDirectory', 'createDirectory'],
    title: 'Select Project Working Directory',
  })
  if (result.canceled || result.filePaths.length === 0) return null
  return result.filePaths[0]
})

ipcMain.handle('shell:open-path', async (_event, filePath) => {
  if (
    typeof filePath !== 'string' ||
    !path.isAbsolute(filePath) ||
    filePath.includes('\0') ||
    path.normalize(filePath) !== filePath
  ) {
    return { success: false, error: 'invalid file path' }
  }

  const error = await shell.openPath(filePath)
  return error ? { success: false, error: 'failed to open file' } : { success: true }
})

function createMenu() {
  const isMac = process.platform === 'darwin'
  const template = [
    ...(isMac
      ? [
          {
            label: app.name,
            submenu: [
              { role: 'about' },
              { type: 'separator' },
              { role: 'services' },
              { type: 'separator' },
              { role: 'hide' },
              { role: 'hideOthers' },
              { role: 'unhide' },
              { type: 'separator' },
              { role: 'quit' },
            ],
          },
        ]
      : []),
    {
      label: 'Edit',
      submenu: [
        { role: 'undo' },
        { role: 'redo' },
        { type: 'separator' },
        { role: 'cut' },
        { role: 'copy' },
        { role: 'paste' },
        { role: 'selectAll' },
      ],
    },
    {
      label: 'View',
      submenu: [
        { role: 'reload' },
        { role: 'forceReload' },
        { role: 'toggleDevTools' },
        { type: 'separator' },
        { role: 'resetZoom' },
        { role: 'zoomIn' },
        { role: 'zoomOut' },
        { type: 'separator' },
        { role: 'togglefullscreen' },
      ],
    },
    {
      label: 'Window',
      submenu: [
        { role: 'close' }, // Cmd+W
        { role: 'minimize' }, // Cmd+M
        { role: 'zoom' },
        { type: 'separator' },
        { role: 'front' }, // Bring All to Front
      ],
    },
  ]

  const menu = Menu.buildFromTemplate(template)
  Menu.setApplicationMenu(menu)
}

// ── App lifecycle ──────────────────────────────────────────

// ── Remote Auth Header Injection ──────────────────────────
// Chromium strips the Authorization header from fetch requests originating
// from file:// (null origin). The main process supplies the header, but only
// for the configured remote backend's API and health paths.

const CONNECTION_CONFIG_VERSION = 1
let remoteAuthHeader = null // "Basic <base64>" or null
let remoteAuthOrigin = null

function getConnectionConfigPath() {
  return path.join(app.getPath('userData'), 'connection.json')
}

function readStoredConnectionConfig() {
  try {
    const raw = fs.readFileSync(getConnectionConfigPath(), 'utf8')
    const stored = JSON.parse(raw)
    const mode = stored.mode === 'remote' ? 'remote' : 'local'
    let password = ''
    if (mode === 'remote' && stored.encryptedPassword && safeStorage.isEncryptionAvailable()) {
      try {
        password = safeStorage.decryptString(Buffer.from(stored.encryptedPassword, 'base64'))
      } catch (err) {
        console.error('[Electron] Failed to decrypt stored password:', err.message)
      }
    } else if (mode === 'remote' && stored.encryptedPassword) {
      console.error('[Electron] safeStorage unavailable — stored password cannot be read (isEncryptionAvailable=false)')
    }
    return {
      version: stored.version,
      mode,
      remoteUrl: typeof stored.remoteUrl === 'string' ? stored.remoteUrl : '',
      username: typeof stored.username === 'string' ? stored.username : '',
      password,
      encryptedPassword: typeof stored.encryptedPassword === 'string' ? stored.encryptedPassword : '',
    }
  } catch {
    console.error('[Electron] Failed to read connection config at', getConnectionConfigPath())
    return null
  }
}

function validateConnectionConfig(input) {
  if (!input || (input.mode !== 'local' && input.mode !== 'remote')) {
    throw new Error('invalid connection mode')
  }

  const remoteUrl = typeof input.remoteUrl === 'string' ? input.remoteUrl.trim() : ''
  const username = typeof input.username === 'string' ? input.username.trim() : ''
  const password = typeof input.password === 'string' ? input.password : ''
  const origin = remoteUrl ? normalizeRemoteOrigin(remoteUrl) : null

  if (input.mode === 'remote' && !origin) {
    throw new Error('remote URL must be an HTTP(S) origin without a path')
  }

  return { mode: input.mode, remoteUrl: origin || '', username, password }
}

function persistConnectionConfig(input, { preservePassword = true } = {}) {
  const next = validateConnectionConfig(input)
  const previous = readStoredConnectionConfig()
  let encryptedPassword =
    next.mode === 'remote' && preservePassword ? previous?.encryptedPassword || '' : ''

  if (next.password) {
    if (safeStorage.isEncryptionAvailable()) {
      encryptedPassword = safeStorage.encryptString(next.password).toString('base64')
    }
  }

  if (next.mode === 'remote' && (!next.username || !next.password) && !encryptedPassword) {
    throw new Error('remote mode requires a username and password')
  }

  const config = {
    version: CONNECTION_CONFIG_VERSION,
    mode: next.mode,
    remoteUrl: next.remoteUrl,
    username: next.username,
    encryptedPassword,
  }
  const configPath = getConnectionConfigPath()
  const tempPath = `${configPath}.tmp`
  fs.mkdirSync(path.dirname(configPath), { recursive: true })
  fs.writeFileSync(tempPath, JSON.stringify(config, null, 2), { mode: 0o600 })
  fs.renameSync(tempPath, configPath)
  return { ...next, encryptedPassword }
}

function applyRemoteAuthConfig(config) {
  remoteAuthHeader = null
  remoteAuthOrigin = null
  if (!config || config.mode !== 'remote') {
    console.log('[Electron] Remote auth policy: not remote (mode=' + (config && config.mode) + ')')
    return
  }

  const origin = normalizeRemoteOrigin(config.remoteUrl)
  const header = buildBasicAuthHeader(config.username, config.password)
  if (origin && header) {
    remoteAuthOrigin = origin
    remoteAuthHeader = header
    console.log('[Electron] Remote auth policy configured for', origin, '(user=' + config.username + ', hasPassword=' + !!config.password + ')')
  } else {
    console.error('[Electron] Remote auth policy FAILED: origin=' + origin + ' header=' + (header ? 'set' : 'null') + ' user=' + JSON.stringify(config.username) + ' hasPassword=' + !!config.password)
  }
}

async function readLegacyConnectionConfig() {
  if (!mainWindow || mainWindow.isDestroyed()) return null
  return mainWindow.webContents.executeJavaScript(`({
    mode: localStorage.getItem('soloqueue_connection_mode') === 'remote' ? 'remote' : 'local',
    remoteUrl: localStorage.getItem('soloqueue_remote_url') || '',
    username: localStorage.getItem('soloqueue_remote_username') || '',
    password: localStorage.getItem('soloqueue_remote_password') || ''
  })`)
}

async function clearLegacyConnectionConfig() {
  if (!mainWindow || mainWindow.isDestroyed()) return
  await mainWindow.webContents.executeJavaScript(`(() => {
    localStorage.removeItem('soloqueue_remote_password')
  })()`)
}

async function refreshRemoteAuthConfig() {
  try {
    let stored = readStoredConnectionConfig()
    if (!stored) {
      const legacy = await readLegacyConnectionConfig()
      if (legacy?.mode === 'remote' && legacy.remoteUrl && legacy.username && legacy.password) {
        stored = persistConnectionConfig(legacy)
      } else if (legacy) {
        const normalizedLegacy = validateConnectionConfig({ ...legacy, password: '' })
        stored = { ...normalizedLegacy, encryptedPassword: '' }
      }
    }

    // Keep the legacy public fields only as a migration fallback. The
    // Renderer receives the authoritative non-secret values through IPC;
    // only the legacy plaintext password is removed here.
    await clearLegacyConnectionConfig()

    applyRemoteAuthConfig(stored)
    return stored || { mode: 'local', remoteUrl: '', username: '', password: '' }
  } catch (err) {
    remoteAuthHeader = null
    remoteAuthOrigin = null
    console.error('[Electron] Failed to refresh remote auth config:', err.message)
    return { mode: 'local', remoteUrl: '', username: '', password: '' }
  }
}

ipcMain.handle('remote:save-config', async (_event, input) => {
  try {
    const current = readStoredConnectionConfig()
    const candidate = validateConnectionConfig({ ...input, password: input?.password || '' })
    const canReusePassword =
      current?.mode === 'remote' &&
      current.remoteUrl === candidate.remoteUrl &&
      current.username === candidate.username
    const next = persistConnectionConfig({
      ...candidate,
      // An empty password means keep the existing encrypted credential.
      password: candidate.password || (canReusePassword ? current.password : ''),
    }, { preservePassword: canReusePassword })
    applyRemoteAuthConfig(next)
    return { success: true }
  } catch (err) {
    return { success: false, error: err.message || 'failed to save connection config' }
  }
})

ipcMain.handle('remote:get-config', async () => {
  const stored = readStoredConnectionConfig() || (await refreshRemoteAuthConfig())
  applyRemoteAuthConfig(stored)
  return {
    mode: stored.mode === 'remote' ? 'remote' : 'local',
    remoteUrl: stored.remoteUrl || '',
    username: stored.username || '',
  }
})

// The webRequest filter is registered inside app.whenReady()
// because session is only available after the app is ready.

app.whenReady().then(async () => {
  // Register the webRequest filter for remote auth header injection.
  // Must be inside app.whenReady() because session is only available then.
  session.defaultSession.webRequest.onBeforeSendHeaders(
    { urls: ['https://*/*', 'http://*/*'] },
    (details, callback) => {
      if (
        remoteAuthHeader &&
        shouldInjectRemoteAuth(details.url, remoteAuthOrigin) &&
        !hasAuthorizationHeader(details.requestHeaders)
      ) {
        details.requestHeaders['Authorization'] = remoteAuthHeader
      }
      callback({ requestHeaders: details.requestHeaders })
    }
  )

  createWindow()
  createMenu()
  loadWindowContent()
  createTray()

  mainWindow.webContents.on('did-finish-load', async () => {
    try {
      const connection = await refreshRemoteAuthConfig()
      if (connection.mode !== 'remote') {
        await spawnGoBackend()
      } else {
        console.log('[Electron] Remote mode detected. Skipping local backend startup.')
      }
    } catch (err) {
      console.error('[Electron] Failed to read connection mode, starting backend anyway:', err)
      try {
        await spawnGoBackend()
      } catch (e2) {
        console.error('[Electron] Failed to start backend on startup:', e2)
      }
    }
  })

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
  destroyTray()
  killGoProcess()
})
