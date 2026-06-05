import { create } from 'zustand'
import { sounds } from '../utils/audio'

export type AgentType = 'L1' | 'L2' | 'L3'
export type AgentStatus = 'idle' | 'walking_in' | 'working' | 'error' | 'walking_out'
export type TaskStatus = 'backlog' | 'todo' | 'running' | 'done'

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

export interface Task {
  id: string
  title: string
  team: 'infra' | 'logic' | 'frontend'
  reward: number
  progress: number // 0 to 100
  status: TaskStatus
  duration: number // seconds
  logs: string[]
  assignedAgentId: string | null
  completedAt?: string
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
  tasks: Task[]
  upgrades: Record<string, Upgrade>
  logs: string[]
  activeTaskId: string | null
  
  // Connection states
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
  loadProfile: () => void
  addTask: (title: string, team: 'infra' | 'logic' | 'frontend', reward: number) => void
  assignTask: (taskId: string, agentId: string) => void
  resolveError: (agentId: string) => void
  buyUpgrade: (upgradeId: string) => void
  hireAgent: (name: string, type: AgentType, gender: 'male' | 'female') => void
  setActiveTaskId: (taskId: string | null) => void
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
  'desk-L1': { x: 3, y: 12, direction: 'left' },   // Bottom-left secretary desk
  'desk-A1': { x: 3, y: 4, direction: 'up' },      // Team A (Infra) desk 1
  'desk-A2': { x: 5, y: 4, direction: 'up' },      // Team A (Infra) desk 2
  'desk-C2': { x: 3, y: 6, direction: 'up' },      // Team A (Infra) desk 3 (repurposed from C2)
  'desk-B1': { x: 11, y: 4, direction: 'up' },     // Team B (Logic) desk 1
  'desk-B2': { x: 13, y: 4, direction: 'up' },     // Team B (Logic) desk 2
  'desk-C1': { x: 19, y: 4, direction: 'up' },     // Team C (Frontend) desk 1
}

const GRID_SIZE = 32
const ELEVATOR_SPAWN = { x: 18, y: 14 } // Bottom-right elevator spawn grid coord

// Simple BFS grid pathfinder
export function findPath(
  startX: number,
  startY: number,
  endX: number,
  endY: number
): { x: number; y: number }[] {
  const maxX = Math.max(24, ...Object.values(WORKSTATIONS).map(w => w.x))
  const cols = maxX + 8
  const rows = 18

  // Open floor pathing: walkable everywhere inside outer walls except physical obstacles
  const isWalkable = (gx: number, gy: number): boolean => {
    if (gx <= 0 || gx >= cols - 1 || gy <= 0 || gy >= rows - 1) return false
    
    // Allow walking on destination cell
    if (gx === endX && gy === endY) return true

    // Block other workstations
    for (const desk of Object.values(WORKSTATIONS)) {
      if (gx === desk.x && gy === desk.y) {
        return false
      }
    }

    // Block secretary desk physical area (columns 3-4, row 13)
    if ((gx === 3 || gx === 4) && gy === 13) {
      return false
    }

    // Block elevator walls (columns 18-21, rows 11-13, except the door at 18,13)
    if (gx >= 18 && gx <= 21 && gy >= 11 && gy <= 13) {
      if (gx === 18 && gy === 13) {
        return true
      }
      return false
    }

    // Block elevator lobby partition walls (column 16, rows 10-16, except the gate at 16,13)
    if (gx === 16 && gy >= 10 && gy <= 16) {
      if (gx === 16 && gy === 13) {
        return true
      }
      return false
    }

    return true
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

// Generate realistic LLM stream messages for simulator
function generateLogsForTask(taskTitle: string, agentName: string): string[] {
  return [
    `[INFO] Starting execution context for task: "${taskTitle}"`,
    `[SYSTEM] Allocating expert worker model: ${agentName}`,
    `[THINK] Analyzing requirements for "${taskTitle}"...`,
    `[THINK] The codebase structure contains router.go and main_test.go. I should search for relevant structures.`,
    `[TOOL_CALL] call_id="c1" name="grep_search" args={"Query": "Optimize", "SearchPath": "."}`,
    `[TOOL_RESULT] call_id="c1" output="Found matches in internal/db/query.go (Line 45, Line 89)"`,
    `[THINK] Excellent. The query lacks index parameters. Let's write a database patch file to optimize performance.`,
    `[TOOL_CALL] call_id="c2" name="replace_file_content" args={"TargetFile": "internal/db/query.go", "Instruction": "Add optimized composite index to query" }`,
    `[TOOL_RESULT] call_id="c2" output="File successfully modified (1 diff block applied)"`,
    `[THINK] Running verify tests to ensure no regressions.`,
    `[TOOL_CALL] call_id="c3" name="run_command" args={"CommandLine": "go test ./internal/db/..."}`,
    `[TOOL_RESULT] call_id="c3" output="PASS: TestQueryOptimization (0.45s)"`,
    `[INFO] Optimization complete. Token overhead analyzed.`,
    `[SYSTEM] Task complete! Releasing allocation lock.`
  ]
}

export const useSimStore = create<SimState>((set, get) => ({
  tokens: 1500, // Pre-seed currency so player has capital
  profile: {
    name: '',
    gender: 'male',
    style: 'friendly',
    registered: false,
  },
  agents: [],
  tasks: [
    { id: 'task-1', title: 'Initialize Vector Store DB Schema', team: 'infra', reward: 200, progress: 0, status: 'todo', duration: 8, logs: [], assignedAgentId: null },
    { id: 'task-2', title: 'Fix WebSocket ErrSessionBusy Race Condition', team: 'logic', reward: 350, progress: 0, status: 'todo', duration: 12, logs: [], assignedAgentId: null },
    { id: 'task-3', title: 'Design Glassmorphism Side Panel Layout', team: 'frontend', reward: 150, progress: 0, status: 'todo', duration: 6, logs: [], assignedAgentId: null },
    { id: 'task-4', title: 'Profile Context Window Token Compression', team: 'infra', reward: 400, progress: 0, status: 'todo', duration: 15, logs: [], assignedAgentId: null },
  ],
  upgrades: {
    'desk': { id: 'desk', name: 'Oak Desks Upgrades', description: 'Increases agent working speed by 25% per level.', level: 1, maxLevel: 5, baseCost: 300, costMultiplier: 1.8 },
    'coffee': { id: 'coffee', name: 'Premium Coffee Maker', description: 'Increases walking speeds and lowers crash rates by 20%.', level: 1, maxLevel: 5, baseCost: 200, costMultiplier: 1.5 },
    'network': { id: 'network', name: 'Gigabit Fiber Router', description: 'Unlocks Teams. Level 1: Infra | Level 2: Logic | Level 3: Frontend', level: 1, maxLevel: 3, baseCost: 500, costMultiplier: 2.2 },
  },
  logs: [
    'Welcome to SoloQueue cozy office simulation!',
    'Hire agents, delegate tasks, and accumulate Token profits.'
  ],
  activeTaskId: null,
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

  loadProfile: () => {
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
      } catch {
        // ignore
      }
    }
  },

  addTask: (title, team, reward) => {
    const newTask: Task = {
      id: `task-${Date.now()}`,
      title,
      team,
      reward,
      progress: 0,
      status: 'todo',
      duration: Math.floor(Math.random() * 8) + 6,
      logs: [],
      assignedAgentId: null
    }
    set({
      tasks: [...get().tasks, newTask],
      logs: [...get().logs, `New issue added to backlog: "${title}"`]
    })
  },

  assignTask: (taskId, agentId) => {
    const task = get().tasks.find(t => t.id === taskId)
    const agent = get().agents.find(a => a.id === agentId)

    if (!task || !agent || agent.status !== 'idle') return

    // Pathfind from current location to their workstation
    const dest = WORKSTATIONS[agent.workstationId]
    const path = findPath(
      Math.floor(agent.x / GRID_SIZE),
      Math.floor(agent.y / GRID_SIZE),
      dest.x,
      dest.y
    )

    // Play select sound
    sounds.playSelect()

    // Update agent & task state
    set({
      agents: get().agents.map(a => 
        a.id === agentId 
          ? { 
              ...a, 
              status: 'walking_in', 
              currentTaskId: taskId,
              targetX: dest.x * GRID_SIZE,
              targetY: dest.y * GRID_SIZE,
              path: path
            } 
          : a
      ),
      tasks: get().tasks.map(t => 
        t.id === taskId 
          ? { ...t, status: 'running', assignedAgentId: agentId, logs: generateLogsForTask(t.title, agent.name) } 
          : t
      ),
      logs: [...get().logs, `Assigned "${task.title}" to ${agent.name}. Agent is walking to workstation.`]
    })
  },

  resolveError: (agentId) => {
    const agent = get().agents.find(a => a.id === agentId)
    if (!agent || agent.status !== 'error') return

    sounds.playSelect()
    set({
      agents: get().agents.map(a => 
        a.id === agentId ? { ...a, status: 'working', errorTimer: null } : a
      ),
      logs: [...get().logs, `Resolved compilation panic for ${agent.name}. Resuming task.`]
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
    const deskPrefixes = type === 'L2' ? ['desk-B1', 'desk-B2'] : ['desk-A1', 'desk-A2', 'desk-C1', 'desk-C2']
    for (const d of deskPrefixes) {
      if (!occupiedDesks.has(d)) {
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

    const newAgent: Agent = {
      id: `agent-${Date.now()}`,
      name,
      type,
      gender,
      status: 'idle',
      level: 1,
      workstationId: deskId,
      currentTaskId: null,
      x: ELEVATOR_SPAWN.x * GRID_SIZE,
      y: ELEVATOR_SPAWN.y * GRID_SIZE,
      targetX: ELEVATOR_SPAWN.x * GRID_SIZE,
      targetY: ELEVATOR_SPAWN.y * GRID_SIZE,
      path: [],
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

  setActiveTaskId: (taskId) => set({ activeTaskId: taskId }),

  addLog: (text) => set({ logs: [...get().logs, text] }),

  toggleChat: () => set(s => ({ chatOpen: !s.chatOpen })),

  connectToBackend: (customUrl?: string) => {
    const base = customUrl || 'http://localhost:8765'
    set({ backendUrl: base, wsUrl: base.replace('http', 'ws') + '/ws', isConnecting: true })
    try {
      const ws = new WebSocket(base.replace('http', 'ws') + '/ws')
      ws.onopen = () => set({ isConnected: true, isConnecting: false })
      ws.onmessage = (event) => {
        try {
          const msg = JSON.parse(event.data)
          if (msg.type === 'state') get().syncBackendState(msg)
        } catch {}
      }
      ws.onclose = () => set({ isConnected: false, isConnecting: false })
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
    const deskLvl = state.upgrades['desk'].level

    // Speed constants
    const baseWalkSpeed = 64 // pixels per second
    const walkSpeed = baseWalkSpeed * (1 + (coffeeLvl - 1) * 0.2)
    const workSpeedMultiplier = 1 + (deskLvl - 1) * 0.25

    let tokensGained = 0
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

    // Update tasks progress
    const updatedTasks = state.tasks.map(task => {
      if (task.status !== 'running') return task

      const assignedAgent = updatedAgents.find(a => a.id === task.assignedAgentId)
      if (!assignedAgent) return task

      // If agent is in error, progress pauses
      if (assignedAgent.status === 'error') return task

      // Increment progress if agent is working at desk
      if (assignedAgent.status === 'working') {
        const progressIncrement = (100 / task.duration) * dt * workSpeedMultiplier
        const newProgress = Math.min(task.progress + progressIncrement, 100)
        
        if (newProgress >= 100) {
          tokensGained += task.reward
          sounds.playSuccess()
          logsAdded.push(`[x] Task completed successfully: "${task.title}"! Gained +${task.reward} Tokens.`)
          
          // Send agent back to elevator (despawn)
          const agentIdx = updatedAgents.findIndex(a => a.id === task.assignedAgentId)
          if (agentIdx !== -1) {
            const path = findPath(
              Math.floor(updatedAgents[agentIdx].x / GRID_SIZE),
              Math.floor(updatedAgents[agentIdx].y / GRID_SIZE),
              ELEVATOR_SPAWN.x,
              ELEVATOR_SPAWN.y
            )
            updatedAgents[agentIdx].status = 'walking_out'
            updatedAgents[agentIdx].targetX = ELEVATOR_SPAWN.x * GRID_SIZE
            updatedAgents[agentIdx].targetY = ELEVATOR_SPAWN.y * GRID_SIZE
            updatedAgents[agentIdx].path = path
          }

          return {
            ...task,
            progress: 100,
            status: 'done' as TaskStatus,
            completedAt: new Date().toLocaleTimeString()
          }
        }
        
        return {
          ...task,
          progress: newProgress
        }
      }

      return task
    })

    // Spawn random tasks periodically if total tasks under 8
    const currentActiveTasksCount = updatedTasks.filter(t => t.status !== 'done').length
    let finalTasks = updatedTasks
    if (currentActiveTasksCount < 6 && Math.random() < 0.005) {
      const titles = [
        'Optimize memory footprints in L0 Router',
        'Add compression buffer to timeline log parser',
        'Verify DeepSeek client retry parameters',
        'Refactor skills load sequence to prevent cycle',
        'Create visual micro-interactions on settings tab',
        'Audit SQLite DB integrity on startup',
        'Add healthcheck heartbeat to QQBot gateway'
      ]
      const teams: ('infra' | 'logic' | 'frontend')[] = ['infra', 'logic', 'frontend']
      const newTitle = titles[Math.floor(Math.random() * titles.length)]
      const newTeam = teams[Math.floor(Math.random() * teams.length)]
      const reward = Math.floor(Math.random() * 200) + 150
      
      const newTask: Task = {
        id: `task-${Date.now()}`,
        title: newTitle,
        team: newTeam,
        reward,
        progress: 0,
        status: 'todo',
        duration: Math.floor(Math.random() * 8) + 6,
        logs: [],
        assignedAgentId: null
      }
      finalTasks = [...updatedTasks, newTask]
      logsAdded.push(`[SYSTEM] New backlog ticket spawned: "${newTitle}"`)
    }

    set({
      agents: updatedAgents,
      tasks: finalTasks,
      tokens: state.tokens + tokensGained,
      logs: logsAdded.length > 0 ? [...state.logs, ...logsAdded] : state.logs
    })
  }
}))
