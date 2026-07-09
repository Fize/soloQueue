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

  // Config
  getAvailableModels: () => ipcRenderer.invoke('backend:get-available-models'),
  saveL1Config: (modelRef) => ipcRenderer.invoke('backend:save-l1-config', modelRef),

  // Connection config
  getConnectionConfig: () => ipcRenderer.invoke('connection:get-config'),
  saveConnectionConfig: (config) => ipcRenderer.invoke('connection:save-config', config),

  // Directory picker
  selectDirectory: () => ipcRenderer.invoke('dialog:select-directory'),
})
