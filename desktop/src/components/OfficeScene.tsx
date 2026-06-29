import { useEffect, useRef, useState } from 'react'
import { useSimStore, WORKSTATIONS } from '../stores/simStore'
import { sounds } from '../utils/audio'
import { Application, Container, Sprite, Texture, Rectangle, Graphics, Text } from 'pixi.js'

// Import furniture
import SecretaryChatDialog from './SecretaryChatDialog'

// Import sprites
import spriteL1Female from '../assets/sprites/secretary_female.png'
import spriteL1Male from '../assets/sprites/secretary_male.png'
import spriteL2Female from '../assets/sprites/leader_female.png'
import spriteL2Male from '../assets/sprites/leader_male.png'
import spriteL3Female from '../assets/sprites/agent_female.png'
import spriteL3Male from '../assets/sprites/agent_male.png'

interface OfficeSceneProps {
  onOpenShop: () => void
  backendLoading: boolean
  backendError: string
  onRetryBackend: () => Promise<boolean>
  isConnected: boolean
  backendStatus: 'idle' | 'starting' | 'running' | 'error'
}

const GRID_SIZE = 32

interface Particle {
  sprite: Sprite
  vy: number
  life: number
  maxLife: number
}

// Helper: dynamic sprite sheet scanner for variable-width frames (translated to PixiJS textures)
function parseSpriteSheetTextures(imageElement: HTMLImageElement): Texture[][] {
  const canvas = document.createElement('canvas')
  canvas.width = imageElement.width
  canvas.height = imageElement.height
  const ctx = canvas.getContext('2d')
  if (!ctx) return []
  ctx.drawImage(imageElement, 0, 0)
  
  const imgData = ctx.getImageData(0, 0, imageElement.width, imageElement.height)
  const data = imgData.data

  const rows = 4
  const cellH = imageElement.height / rows
  const textures: Texture[][] = []

  // Create base texture source
  const baseTexture = Texture.from(imageElement)

  for (let r = 0; r < rows; r++) {
    const rowTextures: Texture[] = []
    const yStart = r * cellH
    const yEnd = (r + 1) * cellH

    // Scan columns in this row
    const colHasPixels = new Array(imageElement.width).fill(false)
    for (let x = 0; x < imageElement.width; x++) {
      for (let y = Math.floor(yStart); y < Math.ceil(yEnd); y++) {
        const idx = (y * imageElement.width + x) * 4
        if (data[idx + 3] > 10) {
          colHasPixels[x] = true
          break
        }
      }
    }

    // Find contiguous segments
    const segments: { start: number; end: number }[] = []
    let inSegment = false
    let start = 0
    for (let x = 0; x < imageElement.width; x++) {
      if (colHasPixels[x] && !inSegment) {
        start = x
        inSegment = true
      } else if (!colHasPixels[x] && inSegment) {
        const width = x - start
        if (width > 10) {
          segments.push({ start, end: x - 1 })
        }
        inSegment = false
      }
    }
    if (inSegment) {
      const width = imageElement.width - start
      if (width > 10) {
        segments.push({ start, end: imageElement.width - 1 })
      }
    }

    segments.forEach(seg => {
      let yMin = yEnd
      let yMax = yStart
      let hasPixels = false

      for (let y = Math.floor(yStart); y < Math.ceil(yEnd); y++) {
        for (let x = seg.start; x <= seg.end; x++) {
          const idx = (y * imageElement.width + x) * 4
          if (data[idx + 3] > 10) {
            if (y < yMin) yMin = y
            if (y > yMax) yMax = y
            hasPixels = true
          }
        }
      }

      if (hasPixels && yMin <= yMax) {
        // Create sub-texture frame
        const tex = new Texture({
          source: baseTexture.source,
          frame: new Rectangle(seg.start, yMin, seg.end - seg.start + 1, yMax - yMin + 1)
        })
        rowTextures.push(tex)
      }
    })
    textures.push(rowTextures)
  }
  return textures
}

export default function OfficeScene({
  onOpenShop,
  backendLoading,
  backendError,
  onRetryBackend,
  isConnected,
  backendStatus
}: OfficeSceneProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const appRef = useRef<Application | null>(null)
  
  // Game state from simStore
  const { 
    agents, 
    tokens, 
    resolveError
  } = useSimStore()

  // Interaction States
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null)
  const [activeTab, setActiveTab] = useState<'chat' | 'detail'>('chat')
  const [pan, setPan] = useState({ x: 0, y: 0 })
  const [zoom, setZoom] = useState(1)
  const [timeStr, setTimeStr] = useState('')
  const [isDarkTheme, setIsDarkTheme] = useState(() => document.documentElement.classList.contains('dark'))

  // Listen to theme changes on documentElement
  useEffect(() => {
    const observer = new MutationObserver(() => {
      setIsDarkTheme(document.documentElement.classList.contains('dark'))
    })
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    return () => observer.disconnect()
  }, [])

  // Drag states
  const isDragging = useRef(false)
  const dragStart = useRef({ x: 0, y: 0 })

  // PixiJS Containers
  const worldRef = useRef<Container | null>(null)
  const depthGroupRef = useRef<Container | null>(null)
  const particlesRef = useRef<Particle[]>([])

  // Store parsed textures
  const spriteTexturesRef = useRef<Record<string, Texture[][]>>({})
  const agentSpritesRef = useRef<Record<string, { container: Container; sprite: Sprite; label: Text; baseRing?: Graphics }>>({})

  // Update digital clock
  useEffect(() => {
    const updateTime = () => {
      const date = new Date()
      setTimeStr(date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }))
    }
    updateTime()
    const timer = setInterval(updateTime, 1000)
    return () => clearInterval(timer)
  }, [])

  // Keyboard Shortcuts
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'h') {
        e.preventDefault()
        onOpenShop()
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [onOpenShop])

  // Initialize PixiJS Application
  useEffect(() => {
    if (!containerRef.current) return

    const container = containerRef.current
    const canvas = document.createElement('canvas')
    canvas.className = "w-full h-full cursor-grab active:cursor-grabbing"
    container.appendChild(canvas)

    const rect = container.getBoundingClientRect()
    
    const app = new Application()
    appRef.current = app

    let initialized = false
    let destroyed = false

    const initPixi = async () => {
      try {
        await app.init({
          canvas,
          width: rect.width,
          height: rect.height,
          background: isDarkTheme ? '#020104' : '#e2e8f0',
          resolution: window.devicePixelRatio || 1,
          autoDensity: true,
        })
        initialized = true

        if (destroyed) {
          app.destroy({ removeView: false })
          return
        }

        // 1. World container for panning/zooming
        const world = new Container()
        worldRef.current = world
        app.stage.addChild(world)

        const mapW = 24 * GRID_SIZE
        const mapH = 18 * GRID_SIZE

        // 2. Draw Floor grid
        const floorColor = isDarkTheme ? '#0a0714' : '#f5f3ef'
        const gridColor = isDarkTheme ? '#251a4a' : '#e5e2db'
        const wallColor = isDarkTheme ? '#140f29' : '#e9e7e2'
        const wallBorder = isDarkTheme ? '#8b5cf6' : '#cbd5e1'
        const moldColor = isDarkTheme ? '#a78bfa' : '#94a3b8'
        const partitionColor = isDarkTheme ? '#1e1b30' : '#e2e8f0'
        const partitionBorder = isDarkTheme ? '#564d7c' : '#cbd5e1'
        
        const elevatorColor = isDarkTheme ? '#1e1b30' : '#d1d5db'
        const elevatorRecess = isDarkTheme ? '#0f0d1a' : '#e5e7eb'
        const elevatorDoor = isDarkTheme ? '#3a3554' : '#f3f4f6'
        const elevatorDoorBorder = isDarkTheme ? '#564d7c' : '#cbd5e1'
        const elevatorLine = isDarkTheme ? '#8b5cf6' : '#9ca3af'
        const elevatorStripe = isDarkTheme ? '#ffd700' : '#f59e0b'
        const elevatorScreen = isDarkTheme ? '#252033' : '#f3f4f6'
        const elevatorScreenBorder = isDarkTheme ? '#8b5cf6' : '#cbd5e1'
        const elevatorScreenArrow = isDarkTheme ? '#34d399' : '#3b82f6'

        const stairColor = isDarkTheme ? '#140f29' : '#e9e7e2'
        const stairBorder = isDarkTheme ? '#8b5cf6' : '#cbd5e1'
        const stairStep = isDarkTheme ? '#1c182a' : '#e8dfc9'
        const stairStepShadow = isDarkTheme ? '#120f1a' : '#b5a98d'
        const stairRail = isDarkTheme ? '#8b5cf6' : '#94a3b8'

        const deskColor = isDarkTheme ? '#16102b' : '#e8dfc9'
        const deskBorder = isDarkTheme ? '#8b5cf6' : '#cbd5e1'
        const deskTop = isDarkTheme ? '#241b44' : '#faf9f6'
        const standColor = isDarkTheme ? '#8b5cf6' : '#9ca3af'
        const hologramBg = isDarkTheme ? { color: '#8b5cf6', alpha: 0.25 } : { color: '#3b82f6', alpha: 0.15 }
        const hologramBorder = isDarkTheme ? '#a78bfa' : '#60a5fa'
        const hologramText = isDarkTheme ? '#a78bfa' : '#3b82f6'
        const chairColor = isDarkTheme ? '#3d2e6b' : '#93c5fd'
        const chairBorder = isDarkTheme ? '#8b5cf6' : '#60a5fa'

        const wsDesk = isDarkTheme ? '#1e1b30' : '#e8dfc9'
        const wsDeskBorder = isDarkTheme ? '#564d7c' : '#cbd5e1'
        const iMacStand = isDarkTheme ? '#8b5cf6' : '#cbd5e1'
        const iMacScreenBg = isDarkTheme ? '#0a0714' : '#ffffff'
        const iMacScreenBorder = isDarkTheme ? '#8b5cf6' : '#cbd5e1'
        const keybColor = isDarkTheme ? '#2d2844' : '#e5e7eb'
        const glassColor = isDarkTheme ? { color: '#8b5cf6', alpha: 0.6 } : { color: '#34d399', alpha: 0.4 }

        const sofaColor = isDarkTheme ? '#42362b' : '#fdba74'
        const sofaBorder = isDarkTheme ? '#8b5cf6' : '#f97316'
        const sofaCushion = isDarkTheme ? '#241a0e' : '#ffedd5'
        const sofaCushionBorder = isDarkTheme ? '#564d7c' : '#fed7aa'

        const gymBase = isDarkTheme ? '#2a2e3d' : '#cbd5e1'
        const gymBaseBorder = isDarkTheme ? '#564d7c' : '#94a3b8'
        const gymBelt = isDarkTheme ? '#0f111a' : '#1f2937'
        const gymConsole = isDarkTheme ? '#120f1f' : '#4b5563'
        const gymScreen = isDarkTheme ? '#06b6d4' : '#0284c7'
        const matPink = isDarkTheme ? '#a04e7b' : '#fbcfe8'
        const matPinkBorder = isDarkTheme ? '#ec4899' : '#f472b6'
        const matBlue = isDarkTheme ? '#385d8a' : '#bae6fd'
        const matBlueBorder = isDarkTheme ? '#0284c7' : '#38bdf8'
        const rackBase = isDarkTheme ? '#36324d' : '#4b5563'
        const rackBaseBorder = isDarkTheme ? '#564d7c' : '#1f2937'

        const potColor = isDarkTheme ? '#423c5e' : '#ffffff'
        const potBorder = isDarkTheme ? '#8b5cf6' : '#cbd5e1'
        
        const coolerBody = isDarkTheme ? '#36324d' : '#ffffff'
        const coolerBodyBorder = isDarkTheme ? '#564d7c' : '#cbd5e1'
        const coolerScreen = isDarkTheme ? '#120f1f' : '#e5e7eb'
        const coolerBottle = isDarkTheme ? { color: '#06b6d4', alpha: 0.7 } : { color: '#93c5fd', alpha: 0.6 }
        const coolerBottleBorder = isDarkTheme ? '#22d3ee' : '#60a5fa'

        const serverRack = isDarkTheme ? '#181424' : '#e2e8f0'
        const serverRackBorder = isDarkTheme ? '#8b5cf6' : '#cbd5e1'
        const serverScreen = isDarkTheme ? '#0d0a14' : '#f8fafc'
        const serverShelf = isDarkTheme ? '#2d2545' : '#ffffff'
        const serverShelfBorder = isDarkTheme ? '#3d325c' : '#e2e8f0'

        const grid = new Graphics()
        grid.rect(0, 0, mapW, mapH)
        grid.fill(floorColor)
        
        // Draw grid lines
        for (let c = 0; c <= 24; c++) {
          grid.moveTo(c * GRID_SIZE, 0).lineTo(c * GRID_SIZE, mapH)
        }
        for (let r = 0; r <= 18; r++) {
          grid.moveTo(0, r * GRID_SIZE).lineTo(mapW, r * GRID_SIZE)
        }
        grid.stroke({ color: gridColor, width: 1 })
        world.addChild(grid)

        // 3. Wall boundaries
        const walls = new Graphics()
        // Top wall
        walls.rect(0, 0, mapW, GRID_SIZE)
        // Left wall
        walls.rect(0, 0, GRID_SIZE, mapH)
        // Bottom wall
        walls.rect(0, mapH - GRID_SIZE, mapW, GRID_SIZE)
        // Right wall
        walls.rect(mapW - GRID_SIZE, 0, GRID_SIZE, mapH)
        walls.fill(wallColor)
        walls.stroke({ color: wallBorder, width: 2 })

        // Draw clean wall molding strips
        for (let x = 2 * GRID_SIZE; x < mapW - 2 * GRID_SIZE; x += 4 * GRID_SIZE) {
          walls.rect(x, 6, 32, 2)
          walls.fill(moldColor)
        }
        world.addChild(walls)

        // 3.5. Room partition walls for Gym and Lounge
        const partitions = new Graphics()
        // Lounge wall partition
        partitions.rect(6 * GRID_SIZE - 2, 10 * GRID_SIZE, 4, 3 * GRID_SIZE)
        partitions.rect(6 * GRID_SIZE - 2, 14 * GRID_SIZE, 4, 2 * GRID_SIZE)
        // Gym wall partition
        partitions.rect(11 * GRID_SIZE - 2, 10 * GRID_SIZE, 4, 3 * GRID_SIZE)
        partitions.rect(11 * GRID_SIZE - 2, 14 * GRID_SIZE, 4, 2 * GRID_SIZE)
        // Lobby wall partition
        partitions.rect(16 * GRID_SIZE - 2, 10 * GRID_SIZE, 4, 3 * GRID_SIZE)
        partitions.rect(16 * GRID_SIZE - 2, 14 * GRID_SIZE, 4, 2 * GRID_SIZE)

        partitions.fill(partitionColor)
        partitions.stroke({ color: partitionBorder, width: 1 })
        
        // Draw glass panes in partitions
        partitions.rect(6 * GRID_SIZE - 1, 10.5 * GRID_SIZE, 2, 2 * GRID_SIZE)
        partitions.rect(11 * GRID_SIZE - 1, 10.5 * GRID_SIZE, 2, 2 * GRID_SIZE)
        partitions.rect(16 * GRID_SIZE - 1, 10.5 * GRID_SIZE, 2, 2 * GRID_SIZE)
        partitions.fill({ color: '#38bdf8', alpha: 0.15 })
        partitions.stroke({ color: '#bae6fd', width: 0.5 })
        world.addChild(partitions)

        // 3.8. Draw Floor labels to identify rooms
        const drawFloorLabels = () => {
          const createLabel = (text: string, gx: number, gy: number) => {
            const lbl = new Text({
              text,
              style: {
                fontFamily: 'monospace',
                fontSize: 9,
                fontWeight: 'bold',
                fill: isDarkTheme ? '#a78bfa' : '#64748b',
                align: 'center',
              }
            })
            lbl.alpha = 0.25
            lbl.anchor.set(0.5, 0.5)
            lbl.x = gx * GRID_SIZE
            lbl.y = gy * GRID_SIZE
            world.addChild(lbl)
          }

          createLabel('L1 RECEPTION', 4.5, 15.5)
          createLabel('BREAKROOM', 8.5, 15.5)
          createLabel('ENERGY GYM', 13.5, 15.5)
          createLabel('ELEVATOR LOBBY', 18.5, 15.5)
          createLabel('STAIRWELL', 22.0, 13)
          createLabel('DEV ZONE - INFRA', 4.5, 2.5)
          createLabel('DEV ZONE - LOGIC', 12.5, 2.5)
          createLabel('DEV ZONE - FRONTEND', 20.0, 2.5)
        }
        drawFloorLabels()

        // 4. Depth container for Y-sorted sprites
        const depthGroup = new Container()
        depthGroupRef.current = depthGroup
        world.addChild(depthGroup)

        // Procedural Drawing: Elevator
        const drawElevator = () => {
          const elev = new Graphics()
          // Casing
          elev.rect(-64, -96, 128, 96)
          elev.fill(elevatorColor)
          elev.stroke({ color: elevatorLine, width: 2 })

          // Door recess
          elev.rect(-48, -80, 96, 80)
          elev.fill(elevatorRecess)
          
          // Dual doors
          elev.rect(-44, -76, 42, 76)
          elev.fill(elevatorDoor)
          elev.stroke({ color: elevatorDoorBorder, width: 1.5 })
          elev.rect(2, -76, 42, 76)
          elev.fill(elevatorDoor)
          elev.stroke({ color: elevatorDoorBorder, width: 1.5 })

          // Vertical panel lines
          elev.moveTo(-6, -76).lineTo(-6, 0)
          elev.moveTo(6, -76).lineTo(6, 0)
          elev.stroke({ color: elevatorLine, width: 1 })

          // Warning stripe
          elev.rect(-52, -4, 104, 4)
          elev.fill(elevatorStripe)

          // Indicator screen
          elev.rect(-16, -90, 32, 10)
          elev.fill(elevatorScreen)
          elev.stroke({ color: elevatorScreenBorder, width: 1 })
          
          // Up triangle
          elev.moveTo(0, -88).lineTo(-4, -83).lineTo(4, -83).closePath()
          elev.fill(elevatorScreenArrow)

          elev.x = 19.5 * GRID_SIZE
          elev.y = 13.5 * GRID_SIZE
          depthGroup.addChild(elev)
        }
        drawElevator()

        // Procedural Drawing: Staircase
        const drawStaircase = () => {
          const stair = new Graphics()
          stair.rect(-32, -96, 64, 96)
          stair.fill(stairColor)
          stair.stroke({ color: stairBorder, width: 2 })

          // Steps
          const stepW = 56
          const stepH = 8
          for (let i = 0; i < 9; i++) {
            const stepY = -80 + i * 8
            stair.rect(-28, stepY, stepW, stepH)
            stair.fill(stairStep)
            stair.stroke({ color: stairBorder, width: 0.5 })
            
            stair.rect(-28, stepY + stepH - 2, stepW, 2)
            stair.fill(stairStepShadow)
          }

          // Chrome handrail
          stair.moveTo(-24, -80).lineTo(24, 0)
          stair.stroke({ color: stairRail, width: 2 })

          stair.x = 22 * GRID_SIZE
          stair.y = 16.0 * GRID_SIZE
          depthGroup.addChild(stair)
        }
        drawStaircase()

        // Procedural Drawing: Secretary Desk
        const drawSecretaryDesk = () => {
          const desk = new Graphics()
          // Wooden desk aligned to boundary of Columns 3-4, Row 13
          desk.roundRect(-32, -12, 64, 24, 4)
          desk.fill(deskColor)
          desk.stroke({ color: deskBorder, width: 2 })

          // Top panel
          desk.roundRect(-28, -10, 56, 8, 2)
          desk.fill(deskTop)
          desk.stroke({ color: deskBorder, width: 1 })

          // Screen stand
          desk.rect(-10, -18, 2, 6)
          desk.fill(standColor)
          
          // Hologram screen
          desk.moveTo(-20, -36).lineTo(20, -36).lineTo(16, -18).lineTo(-16, -18).closePath()
          desk.fill(hologramBg)
          desk.stroke({ color: hologramBorder, width: 1 })

          const hText = new Text({
            text: 'CHIEF',
            style: {
              fontFamily: 'monospace',
              fontSize: 6,
              fill: hologramText,
              align: 'center',
            }
          })
          hText.anchor.set(0.5, 0.5)
          hText.x = 0
          hText.y = -27
          desk.addChild(hText)

          // Keyboard
          desk.rect(-12, 0, 24, 4)
          desk.fill(standColor)

          // Secretary chair
          desk.roundRect(-10, 14, 20, 10, 3)
          desk.fill(chairColor)
          desk.stroke({ color: chairBorder, width: 1 })
          desk.rect(-4, 4, 8, 10)
          desk.fill(standColor)
          
          desk.x = 4 * GRID_SIZE
          desk.y = 13.5 * GRID_SIZE
          depthGroup.addChild(desk)
        }
        drawSecretaryDesk()

        // Procedural Drawing: Workstations
        const drawWorkstation = (wsId: string, ws: { x: number; y: number; direction: 'left' | 'right' | 'up' | 'down' }) => {
          const wsContainer = new Container()
          const desk = new Graphics()

          // Wood table top (aligned to top half: x = -16 to 16, y = -16 to 0)
          desk.roundRect(-16, -16, 32, 16, 3)
          desk.fill(wsDesk)
          desk.stroke({ color: wsDeskBorder, width: 1.5 })

          // iMac base & screen
          desk.rect(-10, -15, 20, 2)
          desk.fill(iMacScreenBg)
          desk.stroke({ color: iMacScreenBorder, width: 1 })

          desk.rect(-2, -13, 4, 3)
          desk.fill(iMacStand)

          // Keyboard
          desk.rect(-8, -6, 16, 3)
          desk.fill(keybColor)

          // Mint glass screen divider
          desk.rect(-16, -16, 32, 2)
          desk.fill(glassColor)

          // Chair (aligned to bottom half: x = -8 to 8, y = 4 to 14)
          desk.roundRect(-8, 4, 16, 10, 2)
          desk.fill(chairColor)
          desk.stroke({ color: chairBorder, width: 1 })
          
          desk.rect(-4, 0, 8, 4)
          desk.fill(iMacStand)

          wsContainer.addChild(desk)

          let teamColor = isDarkTheme ? '#a78bfa' : '#7c3aed'
          let teamName = 'LOGIC'
          if (wsId.startsWith('desk-A')) {
            teamColor = isDarkTheme ? '#22d3ee' : '#0284c7'
            teamName = 'INFRA'
          } else if (wsId.startsWith('desk-C')) {
            teamColor = isDarkTheme ? '#f472b6' : '#db2777'
            teamName = 'FRONT'
          }
          
          const teamLabel = new Text({
            text: teamName,
            style: {
              fontFamily: 'monospace',
              fontSize: 6,
              fill: teamColor,
              fontWeight: 'bold',
              align: 'center',
            }
          })
          teamLabel.anchor.set(0.5, 0.5)
          teamLabel.y = -22
          wsContainer.addChild(teamLabel)

          const teamDot = new Graphics()
          teamDot.circle(0, -28, 2)
          teamDot.fill(teamColor)
          wsContainer.addChild(teamDot)

          if (ws.direction === 'left') {
            wsContainer.angle = -90
          } else if (ws.direction === 'right') {
            wsContainer.angle = 90
          } else if (ws.direction === 'down') {
            wsContainer.angle = 180
          }

          wsContainer.x = ws.x * GRID_SIZE + 16
          wsContainer.y = ws.y * GRID_SIZE + 16
          depthGroup.addChild(wsContainer)
        }
        Object.entries(WORKSTATIONS).forEach(([wsId, ws]) => {
          drawWorkstation(wsId, ws)
        })

        // Procedural Drawing: Lounge Area Decor
        const drawLoungeDecor = () => {
          const lounge = new Graphics()
          
          // Curved Sectional Sofa
          lounge.roundRect(-24, -20, 48, 16, 4) // Backrest
          lounge.fill(sofaColor)
          lounge.stroke({ color: sofaBorder, width: 1 })
          lounge.roundRect(-20, -12, 40, 20, 4) // Cushion
          lounge.fill(sofaCushion)
          lounge.stroke({ color: sofaCushionBorder, width: 1 })

          // Round oak wood table
          lounge.circle(0, 20, 10)
          lounge.fill(deskColor)
          lounge.stroke({ color: sofaCushionBorder, width: 1.5 })
          
          // Coffee cup
          lounge.circle(-2, 18, 2)
          lounge.fill('#ffffff')
          
          lounge.x = 8.5 * GRID_SIZE
          lounge.y = 12.5 * GRID_SIZE
          depthGroup.addChild(lounge)
        }
        drawLoungeDecor()

        // Procedural Drawing: Gym Area Decor
        const drawGymDecor = () => {
          // Treadmill 1
          const t1 = new Graphics()
          t1.rect(-8, -18, 16, 36)
          t1.fill(gymBase)
          t1.stroke({ color: gymBaseBorder, width: 1 })
          t1.rect(-6, -14, 12, 28)
          t1.fill(gymBelt)
          t1.rect(-8, -22, 16, 4)
          t1.fill(gymConsole)
          t1.rect(-4, -26, 8, 4)
          t1.fill(gymScreen)
          t1.x = 13.5 * GRID_SIZE
          t1.y = 11.5 * GRID_SIZE
          depthGroup.addChild(t1)

          // Yoga mats
          const mats = new Graphics()
          mats.roundRect(-10, -18, 8, 20, 2)
          mats.fill(matPink)
          mats.stroke({ color: matPinkBorder, width: 0.5 })
          mats.roundRect(2, -14, 8, 20, 2)
          mats.fill(matBlue)
          mats.stroke({ color: matBlueBorder, width: 0.5 })
          mats.x = 14.0 * GRID_SIZE
          mats.y = 14.5 * GRID_SIZE
          depthGroup.addChild(mats)

          // Dumbbell Rack
          const rack = new Graphics()
          rack.rect(-12, -4, 24, 8)
          rack.fill(rackBase)
          rack.stroke({ color: rackBaseBorder, width: 1 })
          for (let i = -8; i <= 8; i += 8) {
            rack.circle(i, -1, 3)
            rack.fill(matBlue)
            rack.rect(i - 4, -1, 8, 2)
            rack.fill(gymBelt)
          }
          rack.x = 12.5 * GRID_SIZE
          rack.y = 13.5 * GRID_SIZE
          depthGroup.addChild(rack)
        }
        drawGymDecor()

        // Procedural Drawing: Potted Plants
        const drawPlant = (x: number, y: number) => {
          const plant = new Graphics()
          plant.moveTo(-8, 0).lineTo(8, 0).lineTo(12, -12).lineTo(-12, -12).closePath()
          plant.fill(potColor)
          plant.stroke({ color: potBorder, width: 1.5 })

          plant.moveTo(0, -12).bezierCurveTo(-14, -28, -6, -38, 0, -42).bezierCurveTo(6, -38, 14, -28, 0, -12).closePath()
          plant.fill('#10b981')
          plant.moveTo(-6, -12).bezierCurveTo(-18, -22, -16, -30, -10, -32).bezierCurveTo(-4, -28, -4, -20, -6, -12).closePath()
          plant.fill('#059669')
          plant.moveTo(6, -12).bezierCurveTo(18, -22, 16, -30, 10, -32).bezierCurveTo(4, -28, 4, -20, 6, -12).closePath()
          plant.fill('#34d399')
          
          plant.x = x * GRID_SIZE
          plant.y = y * GRID_SIZE
          depthGroup.addChild(plant)
        }
        drawPlant(1.5, 12.5)
        drawPlant(5.5, 7.0)
        drawPlant(14.5, 13.0)

        // Procedural Drawing: Water Cooler
        const drawWaterCooler = () => {
          const cooler = new Graphics()
          cooler.roundRect(-8, -14, 16, 14, 2)
          cooler.fill(coolerBody)
          cooler.stroke({ color: coolerBodyBorder, width: 1.5 })

          cooler.rect(-5, -12, 10, 8)
          cooler.fill(coolerScreen)

          cooler.rect(-3, -11, 2, 2)
          cooler.fill('#3b82f6')
          cooler.rect(1, -11, 2, 2)
          cooler.fill('#ef4444')

          cooler.roundRect(-7, -42, 14, 28, 4)
          cooler.fill(coolerBottle)
          cooler.stroke({ color: coolerBottleBorder, width: 1 })

          cooler.circle(-3, -32, 1)
          cooler.fill('#ffffff')
          cooler.circle(2, -26, 1.5)
          cooler.fill('#ffffff')
          cooler.circle(-1, -20, 1)
          cooler.fill('#ffffff')

          cooler.roundRect(-8, -44, 16, 3, 1)
          cooler.fill(coolerBody)

          cooler.x = 10.5 * GRID_SIZE
          cooler.y = 13.0 * GRID_SIZE
          depthGroup.addChild(cooler)
        }
        drawWaterCooler()

        // Procedural Drawing: Server Racks
        const drawServerRack = (x: number, y: number) => {
          const rack = new Graphics()
          rack.roundRect(-24, -64, 48, 64, 4)
          rack.fill(serverRack)
          rack.stroke({ color: serverRackBorder, width: 1.5 })

          rack.rect(-20, -58, 40, 52)
          rack.fill(serverScreen)

          for (let row = 0; row < 6; row++) {
            const h = -54 + row * 8
            rack.rect(-18, h, 36, 6)
            rack.fill(serverShelf)
            rack.stroke({ color: serverShelfBorder, width: 0.5 })

            rack.circle(-12, h + 3, 1)
            rack.fill('#3b82f6')
            rack.circle(-8, h + 3, 1)
            rack.fill('#10b981')
            
            rack.circle(12, h + 3, 1)
            rack.fill(Math.random() > 0.5 ? '#f59e0b' : '#ef4444')
          }

          rack.x = x * GRID_SIZE
          rack.y = y * GRID_SIZE
          depthGroup.addChild(rack)
        }
        drawServerRack(9.5, 1.5)
        drawServerRack(15.5, 1.5)

        // Load character spritesheets
        const loadCharacter = (name: string, src: string) => {
          const img = new Image()
          img.src = src
          img.onload = () => {
            spriteTexturesRef.current[name] = parseSpriteSheetTextures(img)
          }
        }
        loadCharacter('L1_female', spriteL1Female)
        loadCharacter('L1_male', spriteL1Male)
        loadCharacter('L2_female', spriteL2Female)
        loadCharacter('L2_male', spriteL2Male)
        loadCharacter('L3_female', spriteL3Female)
        loadCharacter('L3_male', spriteL3Male)

        // Main update loop
        app.ticker.add(() => {
          // 1. Sort depth elements
          depthGroup.children.sort((a, b) => a.y - b.y)

          // 2. Update particles
          const particles = particlesRef.current
          for (let i = particles.length - 1; i >= 0; i--) {
            const p = particles[i]
            p.sprite.y += p.vy
            p.life++
            if (p.life >= p.maxLife) {
              depthGroup.removeChild(p.sprite)
              p.sprite.destroy()
              particles.splice(i, 1)
            }
          }
        })
      } catch (err) {
        console.error('PixiJS init failed:', err)
      }
    }

    initPixi()

    // Handle window resizing
    const handleResize = () => {
      if (!appRef.current || !initialized) return
      const currentRect = container.getBoundingClientRect()
      appRef.current.renderer.resize(currentRect.width, currentRect.height)
    }
    window.addEventListener('resize', handleResize)

    return () => {
      destroyed = true
      window.removeEventListener('resize', handleResize)
      if (appRef.current) {
        if (initialized) {
          try {
            appRef.current.destroy({ removeView: false })
          } catch (e) {
            console.warn('Error during PixiJS destroy:', e)
          }
        }
        appRef.current = null
      }
      try {
        container.removeChild(canvas)
      } catch (e) {
        // ignore
      }
    }
  }, [isDarkTheme])

  // Sync state & update agent sprites dynamically in PixiJS Scene
  useEffect(() => {
    const depthGroup = depthGroupRef.current
    if (!depthGroup) return

    // Clean up removed agents
    Object.keys(agentSpritesRef.current).forEach((id) => {
      const active = agents.find((a) => a.id === id)
      if (!active) {
        const old = agentSpritesRef.current[id]
        depthGroup.removeChild(old.container)
        old.container.destroy({ children: true })
        delete agentSpritesRef.current[id]
      }
    })

    // Create / Update active agents
    agents.forEach((agent) => {
      let data = agentSpritesRef.current[agent.id]
      if (!data) {
        // Create new container
        const container = new Container()
        const sprite = new Sprite()
        sprite.anchor.set(0.5, 1)

        // Create base ring (rendered behind character)
        const baseRing = new Graphics()
        baseRing.ellipse(0, 0, 12, 4)
        baseRing.fill({ color: '#8b5cf6', alpha: 0.15 })
        baseRing.stroke({ color: '#8b5cf6', width: 1.5 })
        container.addChild(baseRing)

        container.addChild(sprite)

        // Add label text
        const label = new Text({
          text: `${agent.name} (Lv.${agent.level})`,
          style: {
            fontFamily: 'monospace',
            fontSize: 9,
            fill: isDarkTheme ? '#ffffff' : '#1e1b2e',
            stroke: { color: isDarkTheme ? '#000000' : '#ffffff', width: isDarkTheme ? 2 : 3 },
            align: 'center',
          }
        })
        label.anchor.set(0.5, 1)
        label.y = -36 // Render name label above sprite head
        container.addChild(label)

        depthGroup.addChild(container)
        data = { container, sprite, label, baseRing }
        agentSpritesRef.current[agent.id] = data
      }

      // Sync position
      data.container.x = agent.x + 16
      data.container.y = agent.y + 16

      // Update name & level badge dynamically
      data.label.text = `${agent.name} (Lv.${agent.level})`

      // Update baseRing style based on status
      if (data.baseRing) {
        data.baseRing.clear()
        let ringColor = isDarkTheme ? '#8b5cf6' : '#7c3aed'
        let ringAlpha = 0.2
        let strokeColor = isDarkTheme ? '#8b5cf6' : '#7c3aed'
        let strokeWidth = 1.5

        if (agent.status === 'working') {
          ringColor = '#10b981'
          strokeColor = '#10b981'
        } else if (agent.status === 'error') {
          ringColor = '#ef4444'
          strokeColor = '#ef4444'
          const pulse = Math.sin(Date.now() / 150) * 0.5 + 0.5
          ringAlpha = 0.1 + pulse * 0.3
          strokeWidth = 1.5 + pulse * 1.5
        }

        data.baseRing.ellipse(0, 0, 12, 4)
        data.baseRing.fill({ color: ringColor, alpha: ringAlpha })
        data.baseRing.stroke({ color: strokeColor, width: strokeWidth })
      }

      // Set character animation frame based on state & direction
      const sheetName = `${agent.type}_${agent.gender || 'female'}`
      const sheet = spriteTexturesRef.current[sheetName]
      if (sheet) {
        // Walk direction logic: Down (0), Left (1), Right (2), Up (3)
        let row = 0
        const dx = agent.targetX - agent.x
        const dy = agent.targetY - agent.y
        if (Math.abs(dx) > Math.abs(dy)) {
          row = dx > 0 ? 2 : 1
        } else if (Math.abs(dy) > 0.1) {
          row = dy > 0 ? 0 : 3
        }

        const cols = sheet[row]
        if (cols && cols.length > 0) {
          const frameIndex = agent.frame % cols.length
          const texture = cols[frameIndex]
          data.sprite.texture = texture
          const ratio = texture.width / texture.height
          data.sprite.height = 36
          data.sprite.width = 36 * ratio
        }
      }

      // Spawn floating coin particles when working
      if (agent.status === 'working' && Math.random() < 0.008) {
        const coinText = new Text({
          text: '+💰',
          style: {
            fontFamily: 'monospace',
            fontSize: 9,
            fill: '#fbbf24',
            fontWeight: 'bold',
            stroke: { color: '#000000', width: 2 },
          }
        })
        coinText.anchor.set(0.5, 0.5)
        coinText.x = data.container.x + (Math.random() * 16 - 8)
        coinText.y = data.container.y - 32

        depthGroup.addChild(coinText)
        particlesRef.current.push({
          sprite: coinText as any,
          vy: -0.6 - Math.random() * 0.4,
          life: 0,
          maxLife: 80,
        })
      }

      // Spawn panic/error warning particles
      if (agent.status === 'error' && Math.random() < 0.05) {
        const warning = new Sprite(Texture.EMPTY)
        const bubble = new Graphics()
        bubble.circle(0, 0, 4)
        bubble.fill('#ef4444')
        warning.addChild(bubble)

        warning.x = data.container.x + (Math.random() * 12 - 6)
        warning.y = data.container.y - 32
        depthGroup.addChild(warning)

        particlesRef.current.push({
          sprite: warning,
          vy: -0.5 - Math.random() * 0.5,
          life: 0,
          maxLife: 60,
        })
      }
    })
  }, [agents, isDarkTheme])

  // Sync pan/zoom state variables with PixiJS Camera Container
  useEffect(() => {
    const world = worldRef.current
    if (!world || !containerRef.current) return
    const rect = containerRef.current.getBoundingClientRect()
    
    const mapW = 24 * GRID_SIZE
    const mapH = 18 * GRID_SIZE
    const baseScale = Math.min((rect.width * 0.95) / mapW, (rect.height * 0.95) / mapH)
    const renderScale = zoom * baseScale

    world.scale.set(renderScale)
    world.position.set(
      pan.x + (rect.width - mapW * renderScale) / 2,
      pan.y + (rect.height - mapH * renderScale) / 2
    )
  }, [pan, zoom])

  // Canvas pan & zoom event handlers (reuse raw HTML event listeners)
  const handleMouseDown = (e: React.MouseEvent) => {
    if (e.button === 0) {
      isDragging.current = true
      dragStart.current = { x: e.clientX - pan.x, y: e.clientY - pan.y }
    }
  }

  const handleMouseMove = (e: React.MouseEvent) => {
    if (isDragging.current) {
      setPan({
        x: e.clientX - dragStart.current.x,
        y: e.clientY - dragStart.current.y,
      })
    }
  }

  const handleMouseUp = () => {
    isDragging.current = false
  }

  const handleWheel = (e: React.WheelEvent) => {
    const delta = e.deltaY > 0 ? 0.9 : 1.1
    setZoom((z) => Math.max(0.5, Math.min(3, z * delta)))
  }

  const handleCanvasClick = (e: React.MouseEvent) => {
    const container = containerRef.current
    if (!container) return
    const rect = container.getBoundingClientRect()

    const mapW = 24 * GRID_SIZE
    const mapH = 18 * GRID_SIZE
    const baseScale = Math.min((rect.width * 0.95) / mapW, (rect.height * 0.95) / mapH)
    const renderScale = zoom * baseScale
    const offsetX = pan.x + (rect.width - mapW * renderScale) / 2
    const offsetY = pan.y + (rect.height - mapH * renderScale) / 2

    const clickX = (e.clientX - rect.left - offsetX) / renderScale
    const clickY = (e.clientY - rect.top - offsetY) / renderScale

    // Secretary desk area check (grid 2-5, 11-13)
    const secDeskX = clickX / GRID_SIZE
    const secDeskY = clickY / GRID_SIZE
    if (secDeskX >= 2 && secDeskX <= 5 && secDeskY >= 11 && secDeskY <= 13) {
      sounds.playSelect()
      setActiveTab('chat')
      return
    }

    const clickedAgent = agents.find(agent => {
      const dx = agent.x + 16 - clickX
      const dy = agent.y + 16 - clickY
      return dx * dx + dy * dy < 250
    })

    if (clickedAgent) {
      sounds.playSelect()
      setSelectedAgentId(clickedAgent.id)
      setActiveTab('detail')
      if (clickedAgent.status === 'error') {
        resolveError(clickedAgent.id)
      }
    } else {
      setSelectedAgentId(null)
    }
  }

  // Selected agent info
  const selectedAgent = agents.find(a => a.id === selectedAgentId)

  return (
    <div className="w-full h-full flex flex-col bg-slate-50 select-none font-retro relative overflow-hidden">
      {/* Top HUD Bar (40px height) */}
      <div className="flex justify-between items-center bg-white border-b border-gray-200 px-4 h-10 text-gray-800 shrink-0">
        <div className="flex items-center gap-2">
          <span className="font-bold tracking-wider text-[12px] text-primary">SOLOQUEUE INC.</span>
          <span className="font-pixel text-[8px] bg-primary/10 text-primary px-1.5 py-0.5 rounded">OFFICE</span>
        </div>

        {/* Backend Status Banner in the middle */}
        <div className="flex items-center gap-2">
          {backendLoading && (
            <div className="flex items-center gap-1.5 text-[10px] text-gray-500">
              <span className="animate-spin inline-block w-2.5 h-2.5 border-2 border-primary border-t-transparent rounded-full" />
              <span>启动后端中...</span>
            </div>
          )}
          {backendStatus === 'error' && (
            <div className="flex items-center gap-2 text-[10px] text-red-500">
              <span>后端异常: {backendError || '连接失败'}</span>
              <button
                onClick={onRetryBackend}
                className="px-2 py-0.5 bg-red-50/50 border border-red-200 rounded text-red-700 hover:bg-red-100 transition-colors"
              >
                重试
              </button>
            </div>
          )}
        </div>

        {/* Right side: Time, Tokens, Connection Dot */}
        <div className="flex items-center gap-4 text-[12px]">
          <div className="text-primary font-bold">{timeStr || '09:41 PM'}</div>
          <div className="text-amber-600 font-bold">💰 {tokens.toLocaleString()}</div>
          <div className="flex items-center gap-1">
            <span className={`w-2 h-2 rounded-full ${isConnected ? 'bg-emerald-500' : 'bg-red-500 animate-pulse'}`} />
            <span className="text-[8px] font-pixel text-gray-400">
              {isConnected ? 'ONLINE' : 'OFFLINE'}
            </span>
          </div>
        </div>
      </div>

      {/* Main Row: Canvas (left) + Side Panel (right) */}
      <div className="flex-1 flex flex-row min-h-0 relative bg-slate-50">
        {/* Canvas Area */}
        <div className="flex-1 h-full relative overflow-hidden bg-white">
          <div
            ref={containerRef}
            onMouseDown={handleMouseDown}
            onMouseMove={handleMouseMove}
            onMouseUp={handleMouseUp}
            onMouseLeave={handleMouseUp}
            onWheel={handleWheel}
            onClick={handleCanvasClick}
            className="w-full h-full cursor-grab active:cursor-grabbing"
          />
        </div>

        {/* Side Panel Area (340px) */}
        <div className="w-[340px] shrink-0 border-l border-gray-200 bg-white/95 backdrop-blur flex flex-col h-full overflow-hidden">
          {/* Tabs header */}
          <div className="flex bg-gray-50 border-b border-gray-200 p-1 gap-1 shrink-0">
            <button
              onClick={() => { sounds.playSelect(); setActiveTab('chat'); }}
              className={`py-1.5 text-[10px] flex-1 rounded font-bold transition-all ${
                activeTab === 'chat'
                  ? 'bg-primary/10 text-primary border border-primary/20'
                  : 'text-gray-400 hover:text-gray-700 hover:bg-gray-100'
              }`}
            >
              💬 SECRETARY CHAT
            </button>
            <button
              onClick={() => { sounds.playSelect(); setActiveTab('detail'); }}
              className={`py-1.5 text-[10px] flex-1 rounded font-bold transition-all relative ${
                activeTab === 'detail'
                  ? 'bg-primary/10 text-primary border border-primary/20'
                  : 'text-gray-400 hover:text-gray-700 hover:bg-gray-100'
              }`}
            >
              📋 STAFF DETAIL
              {selectedAgent && selectedAgent.status === 'error' && (
                <span className="absolute top-1 right-1 w-2 h-2 rounded-full bg-red-500 animate-ping" />
              )}
            </button>
          </div>

          {/* Tab Content */}
          <div className="flex-1 min-h-0 overflow-hidden relative">
            {activeTab === 'chat' && (
              <SecretaryChatDialog />
            )}

            {activeTab === 'detail' && (
              <div className="p-4 flex flex-col h-full text-gray-800 overflow-y-auto">
                {selectedAgent ? (
                  <div className="flex-1 flex flex-col justify-between min-h-0">
                    <div className="shrink-0">
                      {/* Agent Info card */}
                      <div className="flex justify-between items-start border-b border-gray-200 pb-3 mb-4">
                        <div>
                          <h2 className="font-bold text-[16px] text-gray-800 leading-none mb-1">
                            {selectedAgent.name}
                          </h2>
                          <span className="font-pixel text-[9px] text-gray-400">
                            {selectedAgent.type} (LEVEL {selectedAgent.level})
                          </span>
                        </div>
                        <button
                          onClick={() => setSelectedAgentId(null)}
                          className="text-gray-400 hover:text-gray-700 font-bold text-[14px]"
                        >
                          ✕
                        </button>
                      </div>

                      {/* Status */}
                      <div className="mb-4 bg-gray-50 border border-gray-200 p-3 rounded-lg">
                        <div className="flex justify-between items-center">
                          <span className="text-[12px] text-gray-500 font-bold">工作状态:</span>
                          <span className={`text-[12px] font-bold ${
                            selectedAgent.status === 'working' ? 'text-emerald-600' :
                            selectedAgent.status === 'error' ? 'text-red-500 animate-pulse' : 'text-gray-800'
                          }`}>
                            {selectedAgent.status.toUpperCase()}
                          </span>
                        </div>

                        {selectedAgent.status === 'error' && (
                          <button
                            onClick={() => resolveError(selectedAgent.id)}
                            className="mt-3 block w-full py-1.5 bg-red-50 border border-red-200 text-red-700 font-bold rounded hover:bg-red-100 transition-colors text-[11px]"
                          >
                            RESOLVE PANIC
                          </button>
                        )}
                      </div>

                      <p className="text-gray-400 text-[12px] italic text-center py-6">
                        Currently resting.
                      </p>
                    </div>
                  </div>
                ) : (
                  <div className="flex flex-col items-center justify-center h-full text-center p-4">
                    <span className="text-[28px] mb-2 animate-bounce">👥</span>
                    <p className="text-gray-400 text-[12px] leading-relaxed">
                      在左侧地图中点击一个 Agent<br />查看其详细工作状态与推理日志
                    </p>
                  </div>
                )}
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Action Bar (Bottom 36px) */}
      <div className="flex justify-between items-center bg-white border-t border-gray-200 px-4 h-9 text-gray-800 shrink-0">
        <div />

        <div className="flex items-center gap-2 text-[10px] text-gray-400">
          <span className="inline-block w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse" />
          <span>{agents.length} agents working</span>
        </div>

        <button
          onClick={onOpenShop}
          className="flex items-center gap-1.5 cursor-pointer text-gray-500 hover:text-primary hover:bg-primary/10 px-2.5 py-1 rounded transition-all text-[11px] font-bold border border-transparent hover:border-primary/20"
        >
          🛒 Shop <span className="font-pixel text-[8px] opacity-60">⌘H</span>
        </button>
      </div>
    </div>
  )
}
