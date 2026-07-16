const { contextBridge, ipcRenderer } = require('electron')

contextBridge.exposeInMainWorld('electronAPI', {
  backendPort: ipcRenderer.sendSync('backend:get-port-sync'),
  // Window controls
  closeWindow: () => ipcRenderer.invoke('close-window'),
  minimizeWindow: () => ipcRenderer.invoke('minimize-window'),
  maximizeWindow: () => ipcRenderer.invoke('maximize-window'),

  // Backend lifecycle
  startBackend: () => ipcRenderer.invoke('backend:start'),
  stopBackend: () => ipcRenderer.invoke('backend:stop'),
  restartBackend: () => ipcRenderer.invoke('backend:restart'),
  getBackendStatus: () => ipcRenderer.invoke('backend:status'),

  // Backend status push events
  onBackendStatusChanged: (callback) => {
    const handler = (_event, status) => callback(status)
    ipcRenderer.on('backend:status-changed', handler)
    return () => ipcRenderer.removeListener('backend:status-changed', handler)
  },

  // Native directory picker
  selectDirectory: () => ipcRenderer.invoke('dialog:select-directory'),

  // Notify main process when remote connection config changes
  // so it can update webRequest auth header injection.
  notifyRemoteConfigChanged: () => ipcRenderer.send('remote:config-changed'),

  // Push the current connection state to the macOS menu bar Tray.
  // See main.js: `tray:update-status` handler. Safe to call before the
  // tray is created — main process will buffer the latest status.
  notifyTrayStatus: (status) => ipcRenderer.send('tray:update-status', status),
})
