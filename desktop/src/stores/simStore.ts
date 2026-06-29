import { create } from 'zustand'
import { sounds } from '../utils/audio'

export type AgentType = 'L1' | 'L2' | 'L3'
export type AgentStatus = 'idle' | 'walking_in' | 'working' | 'error' | 'walking_out'

export interface Agent {
  id: string
  name: string
  type: AgentType
  gender: 'male' | 'female'
  status: AgentStatus
  level: number
  workstationId: string
  currentTaskId: string | null
  // Movement coordinates
  x: number
  y: number
  targetX: number
  targetY: number
  path: { x: number; y: number }[]
  speedMultiplier: number
  frame: number
  frameTimer: number
  errorTimer: number | null
}

export interface Upgrade {
  id: string
  name: string
  description: string
  level: number
  maxLevel: number
  baseCost: number
  costMultiplier: number
}

interface SimState {
  tokens: number
  profile: {
    name: string
    gender: 'male' | 'female'
    style: string
    registered: boolean
  }
  agents: Agent[]
  upgrades: Record<string, Upgrade>
  logs: string[]
  
  // Connection states
  backendStatus: 'idle' | 'starting' | 'running' | 'error'
  backendError: string
  isConnected: boolean
  isConnecting: boolean
  backendUrl: string
  wsUrl: string
  sessionMessages: { role: string; content: string; timestamp: string }[]
  sessionBusy: boolean

  chatOpen: boolean
  abortRef: AbortController | null
  // Actions
  registerL1: (name: string, gender: 'male' | 'female', style: string) => void
  loadProfile: () => Promise<void>
  setBackendStatus: (status: 'idle' | 'starting' | 'running' | 'error', error?: string) => void
  resolveError: (agentId: string) => void
  buyUpgrade: (upgradeId: string) => void
  hireAgent: (name: string, type: AgentType, gender: 'male' | 'female') => void
  tickSimulation: (dt: number) => void
  addLog: (text: string) => void

  connectToBackend: (customUrl?: string) => void
  disconnectFromBackend: () => void
  fetchSessionStatus: () => Promise<void>
  sendSessionPrompt: (prompt: string) => Promise<void>
  cancelSessionTask: () => Promise<void>
  toggleChat: () => void

  clearSessionHistory: () => Promise<void>
  syncBackendState: (msg: any) => void
}

// Predefined workstations coordinates on grid
export const WORKSTATIONS: Record<string, { x: number; y: number; direction: 'left' | 'right' | 'up' | 'down' }> = {
  'desk-L1': { x: 7, y: 15, direction: 'down' },   // Secretary desk in Reception

  // Infra Room (Team A)
  'desk-A1': { x: 3, y: 4, direction: 'up' },
  'desk-A2': { x: 6, y: 4, direction: 'up' },
  'desk-A3': { x: 3, y: 7, direction: 'up' },
  'desk-A4': { x: 6, y: 7, direction: 'up' },

  // Logic Room (Team B)
  'desk-B1': { x: 13, y: 4, direction: 'up' },
  'desk-B2': { x: 16, y: 4, direction: 'up' },
  'desk-B3': { x: 13, y: 7, direction: 'up' },
  'desk-B4': { x: 16, y: 7, direction: 'up' },

  // Frontend Room (Team C)
  'desk-C1': { x: 23, y: 4, direction: 'up' },
  'desk-C2': { x: 26, y: 4, direction: 'up' },
  'desk-C3': { x: 23, y: 7, direction: 'up' },
  'desk-C4': { x: 26, y: 7, direction: 'up' },
}

const GRID_SIZE = 32
const ELEVATOR_SPAWN = { x: 28, y: 16 } // Bottom-right elevator spawn grid coord

// Simple BFS grid pathfinder
export function findPath(
  startX: number,
  startY: number,
  endX: number,
  endY: number
): { x: number; y: number }[] {
  const isWalkable = (gx: number, gy: number): boolean => {
    if (gx < 1 || gx > 30 || gy < 1 || gy > 19) return false;

    // Allow walking on destination cell
    if (gx === endX && gy === endY) return true;

    // Walls
    if (gy === 9) { // Top Inner Wall
      if (![5,6, 15,16, 25,26].includes(gx)) return false;
    }
    if (gy === 12) { // Bottom Inner Wall
      if (![5,6, 15,16, 25,26].includes(gx)) return false;
    }
    if (gx === 10) { // Vertical Wall 1
      if (gy !== 10 && gy !== 11) return false;
    }
    if (gx === 20) { // Vertical Wall 2
      if (gy !== 10 && gy !== 11) return false;
    }

    // Block other workstations
    for (const desk of Object.values(WORKSTATIONS)) {
      if (gx === desk.x && gy === desk.y) return false;
    }

    // Secretary desk physical area (center at 7,15)
    if (gx >= 6 && gx <= 8 && gy >= 14 && gy <= 15) return false;

    // Elevator shaft
    if (gx >= 27 && gx <= 29 && gy >= 9 && gy <= 12) return false;

    // Sofa in breakroom
    if (gx >= 14 && gx <= 16 && gy === 16) return false;
    
    // Coffee table
    if ((gx === 15 || gx === 16) && gy === 17) return false;

    // Water cooler
    if (gx === 11 && gy === 14) return false;

    // Potted plants
    const plants = [
      [2, 3], [9, 3], [11, 3], [18, 3], [21, 3], [28, 3],
      [2, 14], [2, 18], [19, 14], [22, 18], [28, 18]
    ]
    for (const [px, py] of plants) {
      if (gx === px && gy === py) return false;
    }

    return true;
  }

  // BFS Queue
  const queue: [number, number, { x: number; y: number }[]][] = [[startX, startY, [{ x: startX, y: startY }]]]
  const visited = new Set<string>()
  visited.add(`${startX},${startY}`)

  const directions = [
    [0, -1], // Up
    [0, 1],  // Down
    [-1, 0], // Left
    [1, 0]   // Right
  ]

  while (queue.length > 0) {
    const [cx, cy, path] = queue.shift()!

    if (cx === endX && cy === endY) {
      return path.map(p => ({ x: p.x * GRID_SIZE, y: p.y * GRID_SIZE }))
    }

    for (const [dx, dy] of directions) {
      const nx = cx + dx
      const ny = cy + dy
      const key = `${nx},${ny}`

      if (!visited.has(key) && (isWalkable(nx, ny) || (nx === endX && ny === endY))) {
        visited.add(key)
        queue.push([nx, ny, [...path, { x: nx, y: ny }]])
      }
    }
  }

  // Fallback: direct path if BFS fails
  return [{ x: startX * GRID_SIZE, y: startY * GRID_SIZE }, { x: endX * GRID_SIZE, y: endY * GRID_SIZE }]
}

export type BackendConnectionStatus = 'idle' | 'starting' | 'running' | 'error'

export const useSimStore = create<SimState>((set, get) => ({
  tokens: 1500,
  profile: {
    name: '',
    gender: 'male',
    style: 'friendly',
    registered: false,
  },
  agents: [],
  upgrades: {
    'desk': { id: 'desk', name: 'Oak Desks Upgrades', description: 'Increases agent working speed by 25% per level.', level: 1, maxLevel: 5, baseCost: 300, costMultiplier: 1.8 },
    'coffee': { id: 'coffee', name: 'Premium Coffee Maker', description: 'Increases walking speeds and lowers crash rates by 20%.', level: 1, maxLevel: 5, baseCost: 200, costMultiplier: 1.5 },
    'network': { id: 'network', name: 'Gigabit Fiber Router', description: 'Unlocks Teams. Level 1: Infra | Level 2: Logic | Level 3: Frontend', level: 1, maxLevel: 3, baseCost: 500, costMultiplier: 2.2 },
  },
  logs: [
    'Welcome to SoloQueue cozy office simulation!',
    'Hire agents, and accumulate Token profits.'
  ],
  backendStatus: 'idle' as BackendConnectionStatus,
  backendError: '',
  isConnected: false,
  isConnecting: false,
  backendUrl: 'http://localhost:8765',
  wsUrl: 'ws://localhost:8765/ws',
  sessionMessages: [],
  sessionBusy: false,
  chatOpen: false,
  abortRef: null as AbortController | null,

  registerL1: (name, gender, style) => {
    const l1Agent: Agent = {
      id: 'agent-L1',
      name: name,
      type: 'L1',
      gender: gender,
      status: 'idle',
      level: 1,
      workstationId: 'desk-L1',
      currentTaskId: null,
      x: WORKSTATIONS['desk-L1'].x * GRID_SIZE,
      y: WORKSTATIONS['desk-L1'].y * GRID_SIZE,
      targetX: WORKSTATIONS['desk-L1'].x * GRID_SIZE,
      targetY: WORKSTATIONS['desk-L1'].y * GRID_SIZE,
      path: [],
      speedMultiplier: 1.0,
      frame: 0,
      frameTimer: 0,
      errorTimer: null
    }

    const newProfile = { name, gender, style, registered: true }
    localStorage.setItem('soloqueue_profile', JSON.stringify(newProfile))
    
    set({
      profile: newProfile,
      agents: [l1Agent],
      logs: [...get().logs, `Registered L1 Chief Secretary: ${name} (Style: ${style})`]
    })
  },

  loadProfile: async () => {
    const saved = localStorage.getItem('soloqueue_profile')
    if (saved) {
      try {
        const parsed = JSON.parse(saved)
        const l1Agent: Agent = {
          id: 'agent-L1',
          name: parsed.name,
          type: 'L1',
          gender: parsed.gender,
          status: 'idle',
          level: 1,
          workstationId: 'desk-L1',
          currentTaskId: null,
          x: WORKSTATIONS['desk-L1'].x * GRID_SIZE,
          y: WORKSTATIONS['desk-L1'].y * GRID_SIZE,
          targetX: WORKSTATIONS['desk-L1'].x * GRID_SIZE,
          targetY: WORKSTATIONS['desk-L1'].y * GRID_SIZE,
          path: [],
          speedMultiplier: 1.0,
          frame: 0,
          frameTimer: 0,
          errorTimer: null
        }
        set({
          profile: parsed,
          agents: [l1Agent]
        })
        return
      } catch {
        // ignore
      }
    }

    // Try fetching from backend soul.md profile
    try {
      const backendUrl = get().backendUrl || ''
      const res = await fetch(backendUrl + '/api/agents/secretary/profile')
      if (res.ok) {
        const data = await res.json()
        if (data && data.soul && data.soul.trim()) {
          const soul = data.soul
          
          let name = 'Alex'
          let gender: 'male' | 'female' = 'female'
          let style = 'friendly'

          const nameMatch = soul.match(/- Name:\s*([^\n\r]+)/i)
          if (nameMatch && nameMatch[1].trim()) {
            name = nameMatch[1].trim()
          }

          const genderMatch = soul.match(/- Gender:\s*([^\n\r.]+)/i)
          if (genderMatch) {
            const g = genderMatch[1].toLowerCase()
            if (g.includes('female') || g.includes('woman') || g.includes('girl')) {
              gender = 'female'
            } else if (g.includes('male') || g.includes('man') || g.includes('boy')) {
              gender = 'male'
            }
          }

          const styleMatch = soul.match(/- Communication style:\s*([^\n\r.]+)/i)
          if (styleMatch) {
            const s = styleMatch[1].toLowerCase()
            if (s.includes('professional') || s.includes('direct')) {
              style = 'professional'
            } else if (s.includes('sarcastic') || s.includes('witty') || s.includes('humor')) {
              style = 'sarcastic'
            } else if (s.includes('cold') || s.includes('efficient')) {
              style = 'cold'
            }
          }

          const newProfile = { name, gender, style, registered: true }
          localStorage.setItem('soloqueue_profile', JSON.stringify(newProfile))
          
          const l1Agent: Agent = {
            id: 'agent-L1',
            name,
            type: 'L1',
            gender,
            status: 'idle',
            level: 1,
            workstationId: 'desk-L1',
            currentTaskId: null,
            x: WORKSTATIONS['desk-L1'].x * GRID_SIZE,
            y: WORKSTATIONS['desk-L1'].y * GRID_SIZE,
            targetX: WORKSTATIONS['desk-L1'].x * GRID_SIZE,
            targetY: WORKSTATIONS['desk-L1'].y * GRID_SIZE,
            path: [],
            speedMultiplier: 1.0,
            frame: 0,
            frameTimer: 0,
            errorTimer: null
          }

          set({
            profile: newProfile,
            agents: [l1Agent],
            logs: [...get().logs, `Loaded L1 Chief Secretary from disk: ${name} (Style: ${style})`]
          })
        }
      }
    } catch (e) {
      console.warn('Failed to load profile from backend:', e)
    }
  },

  resolveError: (agentId) => {
    const agent = get().agents.find(a => a.id === agentId)
    if (!agent || agent.status !== 'error') return

    sounds.playSelect()
    set({
      agents: get().agents.map(a => 
        a.id === agentId ? { ...a, status: 'working', errorTimer: null } : a
      ),
      logs: [...get().logs, `Resolved compilation panic for ${agent.name}. Resuming work.`]
    })
  },

  buyUpgrade: (upgradeId) => {
    const upgrade = get().upgrades[upgradeId]
    if (!upgrade || upgrade.level >= upgrade.maxLevel) return

    const cost = Math.floor(upgrade.baseCost * Math.pow(upgrade.costMultiplier, upgrade.level - 1))
    if (get().tokens < cost) {
      set({ logs: [...get().logs, `Insufficient tokens to upgrade ${upgrade.name}! Required: ${cost}`] })
      return
    }

    sounds.playUpgrade()
    set({
      tokens: get().tokens - cost,
      upgrades: {
        ...get().upgrades,
        [upgradeId]: { ...upgrade, level: upgrade.level + 1 }
      },
      logs: [...get().logs, `Upgraded ${upgrade.name} to Level ${upgrade.level + 1}!`]
    })
  },

  hireAgent: (name, type, gender) => {
    const cost = type === 'L2' ? 600 : 300
    if (get().tokens < cost) {
      set({ logs: [...get().logs, `Insufficient tokens to hire ${name}! Cost: ${cost}`] })
      return
    }

    // Find first available workstation that is unoccupied
    const occupiedDesks = new Set(get().agents.map(a => a.workstationId))
    let deskId = ''
    
    // Look for desks matching type
    const deskPrefixes = type === 'L2' ? ['desk-B'] : ['desk-A', 'desk-C']
    for (const d of Object.keys(WORKSTATIONS)) {
      if (deskPrefixes.some(prefix => d.startsWith(prefix)) && !occupiedDesks.has(d)) {
        deskId = d
        break
      }
    }

    if (!deskId) {
      // General fallback
      for (const d of Object.keys(WORKSTATIONS)) {
        if (d !== 'desk-L1' && !occupiedDesks.has(d)) {
          deskId = d
          break
        }
      }
    }

    if (!deskId) {
      // Dynamically create a new desk to support infinite growth
      // We will look for a slot starting from x = 25 (beyond elevator lobby), y = 4 or 6.
      // Every 2 grids horizontally we can place a desk (e.g. 25, 27, 29, 31, 33...)
      let nextX = 25
      let nextY = 4
      
      while (true) {
        // Check if there is already a desk at (nextX, nextY)
        const exists = Object.values(WORKSTATIONS).some(w => w.x === nextX && w.y === nextY)
        if (!exists) {
          break
        }
        // Move to the next slot
        nextX += 2
        if (nextX > 200) { // Wrap to next row if we reach a limit
          nextX = 25
          nextY = nextY === 4 ? 6 : 4
        }
      }
      
      deskId = `desk-dynamic-${nextX}-${nextY}`
      WORKSTATIONS[deskId] = { x: nextX, y: nextY, direction: 'up' }
      set({ logs: [...get().logs, `Created new workstation ${deskId} at (${nextX}, ${nextY}) for infinite expansion!`] })
    }

    const dest = WORKSTATIONS[deskId]
    const path = findPath(
      ELEVATOR_SPAWN.x,
      ELEVATOR_SPAWN.y,
      dest.x,
      dest.y
    )

    const newAgent: Agent = {
      id: `agent-${Date.now()}`,
      name,
      type,
      gender,
      status: 'walking_in',
      level: 1,
      workstationId: deskId,
      currentTaskId: null,
      x: ELEVATOR_SPAWN.x * GRID_SIZE,
      y: ELEVATOR_SPAWN.y * GRID_SIZE,
      targetX: dest.x * GRID_SIZE,
      targetY: dest.y * GRID_SIZE,
      path,
      speedMultiplier: 1.0,
      frame: 0,
      frameTimer: 0,
      errorTimer: null
    }

    sounds.playUpgrade()
    set({
      tokens: get().tokens - cost,
      agents: [...get().agents, newAgent],
      logs: [...get().logs, `Hired new ${type} worker: ${name}! Stationed at ${deskId}.`]
    })
  },

  setBackendStatus: (status, error) => set({ backendStatus: status, backendError: error || '' }),

  addLog: (text) => set({ logs: [...get().logs, text] }),

  toggleChat: () => set(s => ({ chatOpen: !s.chatOpen })),

  connectToBackend: (customUrl?: string) => {
    const base = customUrl || 'http://localhost:8765'
    set({ backendUrl: base, wsUrl: base.replace('http', 'ws') + '/ws', isConnecting: true, backendStatus: 'starting' })
    try {
      const ws = new WebSocket(base.replace('http', 'ws') + '/ws')
      ws.onopen = () => set({ isConnected: true, isConnecting: false, backendStatus: 'running', backendError: '' })
      ws.onmessage = (event) => {
        try {
          const msg = JSON.parse(event.data)
          if (msg.type === 'state') get().syncBackendState(msg)
        } catch {}
      }
      ws.onclose = () => set({ isConnected: false, isConnecting: false, backendStatus: 'idle' })
      ws.onerror = () => set({ isConnected: false, isConnecting: false })
    } catch {
      set({ isConnecting: false })
    }
  },

  disconnectFromBackend: () => {
    set({ isConnected: false, isConnecting: false })
  },

  fetchSessionStatus: async () => {
    const { backendUrl } = get()
    try {
      const res = await fetch(backendUrl + '/api/session')
      const data = await res.json()
      set({ sessionMessages: data.messages || [], sessionBusy: data.busy })
    } catch {}
  },

  sendSessionPrompt: async (prompt: string) => {
    const { backendUrl, sessionMessages, abortRef } = get()
    if (abortRef) abortRef.abort()
    const controller = new AbortController()
    set({ sessionBusy: true, abortRef: controller })
    
    const userMsg = { role: 'user' as const, content: prompt, timestamp: new Date().toISOString() }
    const updated = [...sessionMessages, userMsg]
    set({ sessionMessages: updated })
    
    try {
      const res = await fetch(backendUrl + '/api/session/ask', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ prompt }),
        signal: controller.signal,
      })
      if (!res.ok) throw new Error('Request failed')
      set({ sessionBusy: false, abortRef: null })
      await get().fetchSessionStatus()
    } catch (err) {
      if ((err as Error).name !== 'AbortError') {
        const errMsg = { role: 'assistant' as const, content: 'Error: ' + (err as Error).message, timestamp: new Date().toISOString() }
        set({ sessionMessages: [...get().sessionMessages, errMsg] })
      }
      set({ sessionBusy: false, abortRef: null })
    }
  },

  cancelSessionTask: async () => {
    const { backendUrl, abortRef } = get()
    if (abortRef) {
      abortRef.abort()
      set({ abortRef: null, sessionBusy: false })
    }
    try {
      await fetch(backendUrl + '/api/session/cancel', { method: 'POST' })
    } catch {}
  },

  clearSessionHistory: async () => {
    const { backendUrl } = get()
    try {
      await fetch(backendUrl + '/api/session/clear', { method: 'POST' })
      set({ sessionMessages: [] })
    } catch {}
  },

  syncBackendState: (msg: any) => {
    if (!msg?.agents) return
    const { agents } = get()
    const l1Agent = msg.agents.agents?.find((a: any) => a.id === 'l1-agent' || a.name?.toLowerCase().includes('secretary'))
    if (l1Agent) {
      const l1 = agents.find(a => a.type === 'L1')
      if (l1) {
        const stateMsg = l1Agent.state === 'processing' ? 'working' : l1Agent.state === 'error' ? 'error' : 'idle'
        set({ agents: agents.map(a => a.type === 'L1' ? { ...a, status: stateMsg as any } : a) })
      }
    }
  },

  tickSimulation: (dt) => {
    const state = get()
    const coffeeLvl = state.upgrades['coffee'].level

    // Speed constants
    const baseWalkSpeed = 64 // pixels per second
    const walkSpeed = baseWalkSpeed * (1 + (coffeeLvl - 1) * 0.2)

    const logsAdded: string[] = []

    const updatedAgents = state.agents.map(agent => {
      let status = agent.status
      let x = agent.x
      let y = agent.y
      let path = [...agent.path]
      let frameTimer = agent.frameTimer + dt
      let frame = agent.frame
      let errorTimer = agent.errorTimer
      let currentTaskId = agent.currentTaskId

      const isL1 = agent.type === 'L1'

      // Special slow animation frame timer for stationary L1 Chief Secretary (1500ms per frame)
      if (isL1) {
        if (frameTimer >= 1.5) {
          frame = (frame + 1) % 4
          frameTimer = 0
        }
      }

      // 1. Walking logic
      if (status === 'walking_in' || status === 'walking_out') {
        if (!isL1 && frameTimer >= 0.15) {
          frame = (frame + 1) % 4
          frameTimer = 0
        }

        if (path.length > 0) {
          const nextNode = path[0]
          const dx = nextNode.x - x
          const dy = nextNode.y - y
          const distance = Math.sqrt(dx * dx + dy * dy)
          const step = walkSpeed * dt

          if (distance > step) {
            x += (dx / distance) * step
            y += (dy / distance) * step
          } else {
            x = nextNode.x
            y = nextNode.y
            path.shift()
          }
        } else {
          // Arrived at destination
          if (status === 'walking_in') {
            status = 'working'
          } else {
            status = 'idle'
            currentTaskId = null
          }
        }
      } 
      // 2. Sitting down working logic
      else if (status === 'working') {
        if (!isL1 && frameTimer >= 0.12) {
          frame = (frame + 1) % 4
          frameTimer = 0
        }

        // Random chance of triggering compilation error (crashes code)
        // Crash rate lowers with coffee upgrades
        const crashProbability = 0.002 * (1 - (coffeeLvl - 1) * 0.15)
        if (Math.random() < crashProbability && agent.type !== 'L1') {
          status = 'error'
          errorTimer = 8 // auto-resolve in 8 seconds or player click
          sounds.playError()
          logsAdded.push(`[WARN] ${agent.name} encountered compilation crash: RefCell multiple borrow panic!`)
        }
      } 
      // 3. Error recovery logic
      else if (status === 'error') {
        if (!isL1 && frameTimer >= 0.2) {
          frame = (frame + 1) % 2 // slower err frame
          frameTimer = 0
        }

        if (errorTimer !== null) {
          errorTimer -= dt
          if (errorTimer <= 0) {
            status = 'working'
            errorTimer = null
            logsAdded.push(`[SYSTEM] ${agent.name} auto-resolved error after cooldown.`)
          }
        }
      } 
      // 4. Idle logic
      else {
        if (!isL1 && frameTimer >= 0.25) {
          frame = (frame + 1) % 2
          frameTimer = 0
        }
      }

      return {
        ...agent,
        status,
        x,
        y,
        path,
        frame,
        frameTimer,
        errorTimer,
        currentTaskId
      }
    })

    set({
      agents: updatedAgents,
      logs: logsAdded.length > 0 ? [...state.logs, ...logsAdded] : state.logs
    })
  }
}))
