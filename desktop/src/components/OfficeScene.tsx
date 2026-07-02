import { useEffect, useRef, useState } from 'react'
import { useSimStore, WORKSTATIONS } from '../stores/simStore'
import { sounds } from '../utils/audio'
import { Application, Container, Sprite, Texture, Rectangle, Graphics, Text, Assets } from 'pixi.js'

// Import furniture
import SecretaryChatDialog from './SecretaryChatDialog'

// Import sprites
import spriteL1Female from '../assets/sprites/secretary_female.png'
import spriteL1Male from '../assets/sprites/secretary_male.png'
import spriteL2Female from '../assets/sprites/leader_female.png'
import spriteL2Male from '../assets/sprites/leader_male.png'
import spriteL3Female from '../assets/sprites/agent_female.png'
import spriteL3Male from '../assets/sprites/agent_male.png'

import imgSecretaryDesk from '../assets/furniture/secretary_desk.png'
import imgCubicle from '../assets/furniture/cubicle_tileset.png'
import imgPlant from '../assets/furniture/potted_plant.png'
import imgWaterCooler from '../assets/furniture/water_cooler.png'
import imgOfficeEntrance from '../assets/furniture/office_entrance.png'
import imgSofa from '../assets/furniture/sofa.png'

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
  const [isPanelOpen, setIsPanelOpen] = useState(false)
  const [pan, setPan] = useState({ x: 0, y: 0 })
  const [zoom, setZoom] = useState(1)
  const [timeStr, setTimeStr] = useState('')
  const [isDarkTheme, setIsDarkTheme] = useState(() => document.documentElement.classList.contains('dark'))
  const [pixiReady, setPixiReady] = useState(false)
  const [dimensions, setDimensions] = useState({ width: 0, height: 0 })

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
  const agentSpritesRef = useRef<Record<string, { container: Container; sprite: Sprite; label: Text; baseRing?: Graphics; mask?: Graphics }>>({})

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

    const app = new Application()
    appRef.current = app

    let initialized = false
    let destroyed = false

    const initPixi = async () => {
      try {
        const currentRect = container.getBoundingClientRect()
        setDimensions({ width: currentRect.width, height: currentRect.height })

        await app.init({
          canvas,
          width: currentRect.width,
          height: currentRect.height,
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

        // Fixed boundaries for Game Dev Tycoon style layout
        const maxGridX = 32
        const maxGridY = 21

        const mapW = maxGridX * GRID_SIZE
        const mapH = maxGridY * GRID_SIZE

        // 2. Draw Floors (Game Dev Tycoon style rooms & roads)
        const floors = new Graphics()
        
        const devCarpet = isDarkTheme ? '#1e293b' : '#f8fafc'
        const devWalkway = isDarkTheme ? '#0f172a' : '#e2e8f0'
        
        const recepFloor = isDarkTheme ? '#2d1f10' : '#faeedf'
        const recepPlank = isDarkTheme ? '#1c130a' : '#dec7ad'
        
        const breakFloor1 = isDarkTheme ? '#064e3b' : '#dcfce7'
        const breakFloor2 = isDarkTheme ? '#022c22' : '#f0fdf4'
        
        const entFloor = isDarkTheme ? '#334155' : '#cbd5e1'
        const entBorder = isDarkTheme ? '#475569' : '#94a3b8'

        // Helper to fill rect
        const fillRect = (gx: number, gy: number, gw: number, gh: number, color: string) => {
          floors.rect(gx * GRID_SIZE, gy * GRID_SIZE, gw * GRID_SIZE, gh * GRID_SIZE)
          floors.fill(color)
        }

        // DEV ZONES (Infra, Logic, Frontend)
        fillRect(2, 2, 8, 7, devCarpet)
        fillRect(11, 2, 9, 7, devCarpet)
        fillRect(21, 2, 9, 7, devCarpet)

        // CORRIDOR & DOORS
        fillRect(2, 10, 28, 2, devWalkway) // Main horizontal
        // Door gaps
        fillRect(5, 9, 2, 1, devWalkway)
        fillRect(15, 9, 2, 1, devWalkway)
        fillRect(25, 9, 2, 1, devWalkway)
        fillRect(5, 12, 2, 1, devWalkway)
        fillRect(15, 12, 2, 1, devWalkway)
        fillRect(25, 12, 2, 1, devWalkway)

        // RECEPTION
        fillRect(2, 13, 8, 6, recepFloor)
        for (let py = 13 * GRID_SIZE + 8; py < 19 * GRID_SIZE; py += 8) {
          floors.moveTo(2 * GRID_SIZE, py).lineTo(10 * GRID_SIZE, py)
        }
        floors.stroke({ color: recepPlank, width: 0.5 })

        // BREAKROOM (Checkers)
        for (let gx = 11; gx < 20; gx++) {
          for (let gy = 13; gy < 19; gy++) {
            fillRect(gx, gy, 1, 1, (gx + gy) % 2 === 0 ? breakFloor1 : breakFloor2)
          }
        }

        // ENTRANCE (Marble)
        fillRect(21, 13, 9, 6, entFloor)
        for (let ex = 21 * GRID_SIZE; ex <= 30 * GRID_SIZE; ex += GRID_SIZE) {
          floors.moveTo(ex, 13 * GRID_SIZE).lineTo(ex, 19 * GRID_SIZE)
        }
        for (let ey = 13 * GRID_SIZE; ey <= 19 * GRID_SIZE; ey += GRID_SIZE) {
          floors.moveTo(21 * GRID_SIZE, ey).lineTo(30 * GRID_SIZE, ey)
        }
        floors.stroke({ color: entBorder, width: 0.5 })

        world.addChild(floors)

        // 2.5. Draw Floor grid overlay
        const grid = new Graphics()
        for (let c = 1; c < maxGridX; c++) {
          grid.moveTo(c * GRID_SIZE, 0).lineTo(c * GRID_SIZE, mapH)
        }
        for (let r = 1; r < maxGridY; r++) {
          grid.moveTo(0, r * GRID_SIZE).lineTo(mapW, r * GRID_SIZE)
        }
        grid.stroke({ color: isDarkTheme ? 'rgba(51, 65, 85, 0.25)' : 'rgba(226, 232, 240, 0.45)', width: 1 })
        world.addChild(grid)

        // 3. Draw Outer & Inner Walls
        const officeWalls = new Graphics()
        const wallFillColor = isDarkTheme ? '#1e293b' : '#ffffff'
        const wallStrokeColor = isDarkTheme ? '#475569' : '#cbd5e1'

        const drawWall = (gx: number, gy: number, gw: number, gh: number) => {
          officeWalls.rect(gx * GRID_SIZE, gy * GRID_SIZE, gw * GRID_SIZE, gh * GRID_SIZE)
        }

        // Outer Walls
        drawWall(1, 1, 30, 1) // Top
        drawWall(1, 19, 30, 1) // Bottom
        drawWall(1, 2, 1, 17) // Left
        drawWall(30, 2, 1, 17) // Right

        // Inner Horizontal Row 9
        drawWall(2, 9, 3, 1)
        drawWall(7, 9, 3, 1)
        drawWall(10, 9, 1, 1) // cross
        drawWall(11, 9, 4, 1)
        drawWall(17, 9, 3, 1)
        drawWall(20, 9, 1, 1) // cross
        drawWall(21, 9, 4, 1)

        // Inner Horizontal Row 12
        drawWall(2, 12, 3, 1)
        drawWall(7, 12, 3, 1)
        drawWall(10, 12, 1, 1) // cross
        drawWall(11, 12, 4, 1)
        drawWall(17, 12, 3, 1)
        drawWall(20, 12, 1, 1) // cross
        drawWall(21, 12, 4, 1)

        // Elevator Shaft (Thick block)
        drawWall(27, 9, 3, 4)

        // Inner Vertical Col 10
        drawWall(10, 2, 1, 7)
        drawWall(10, 13, 1, 6)

        // Inner Vertical Col 20
        drawWall(20, 2, 1, 7)
        drawWall(20, 13, 1, 6)

        officeWalls.fill(wallFillColor)
        officeWalls.stroke({ color: wallStrokeColor, width: 2 })
        
        // Wall Moldings (Top Outer Wall)
        for (let x = 2 * GRID_SIZE; x < 30 * GRID_SIZE; x += 4 * GRID_SIZE) {
          officeWalls.rect(x, 1 * GRID_SIZE + 6, 32, 2)
          officeWalls.fill(wallStrokeColor)
        }

        world.addChild(officeWalls)

        // (labelStyle removed since Floor labels were removed)
        // Preload textures
        const texEntrance = await Assets.load(imgOfficeEntrance)
        const texSecretaryDesk = await Assets.load(imgSecretaryDesk)
        const texCubicle = await Assets.load(imgCubicle)
        const texPlant = await Assets.load(imgPlant)
        const texWaterCooler = await Assets.load(imgWaterCooler)
        const texSofa = await Assets.load(imgSofa)



        // (Floor labels removed to prevent hardcoded text)

        // 4. Depth container for Y-sorted sprites
        const depthGroup = new Container()
        depthGroupRef.current = depthGroup
        world.addChild(depthGroup)

        // Draw Entrance Area (Lobby + Elevator) using sprite
        const drawEntrance = () => {
          const rectClosed = new Rectangle(59, 57, 512, 781)
          const texClosed = new Texture({ source: texEntrance.source, frame: rectClosed })

          const entrance = new Sprite(texClosed)
          entrance.anchor.set(0.5, 0.5)
          // Scale to fit 2x3 grids (width 64, height 97)
          entrance.width = 64
          entrance.height = 97
          entrance.x = 28.5 * GRID_SIZE
          entrance.y = 13.0 * GRID_SIZE - 48.5 // Flush with lobby floor (gy=13)
          depthGroup.addChild(entrance)
        }
        drawEntrance()

        // Draw Secretary Desk using sprite
        const drawSecretaryDesk = () => {
          const rect = new Rectangle(97, 106, 1006, 635)
          const tex = new Texture({ source: texSecretaryDesk.source, frame: rect })
          const desk = new Sprite(tex)
          desk.anchor.set(0.5, 0.5)
          // secretary desk width 1006, scale to fit ~ 3.5 grids
          const deskHeight = 110 * (635 / 1006)
          
          // Background desk (Chair backrest)
          const deskBg = new Sprite(tex)
          deskBg.anchor.set(0.5, 0.5)
          deskBg.width = 110
          deskBg.height = deskHeight
          const deskBgContainer = new Container()
          deskBgContainer.x = 7.0 * GRID_SIZE + 16
          deskBgContainer.y = 15.0 * GRID_SIZE + 16 - 16 // Sort well BEFORE agent (agent is at +16)
          deskBg.y = 8 // Visual center is 488 (15*32+16-8), container is 480, offset is +8
          deskBgContainer.addChild(deskBg)
          depthGroup.addChild(deskBgContainer)

          // Foreground desk (Desk surface, monitors)
          const deskFg = new Sprite(tex)
          deskFg.anchor.set(0.5, 0.5)
          deskFg.width = 110
          deskFg.height = deskHeight
          
          // Mask foreground desk to hide ONLY the chair backrest.
          // This allows the front desk surface to naturally cover the agent's legs.
          const deskMask = new Graphics()
          deskMask.rect(-60, -40, 120, deskHeight + 40) // Cover entire desk area
          deskMask.cut()
          deskMask.rect(-15, -40, 30, 26) // Cut out a small window for the chair backrest (y=-40 to -14)
          deskMask.fill(0xffffff)
          deskFg.mask = deskMask
          deskFg.addChild(deskMask)

          const deskFgContainer = new Container()
          deskFgContainer.x = 7.0 * GRID_SIZE + 16
          deskFgContainer.y = 15.0 * GRID_SIZE + 16 + 16 // Sort AFTER agent
          deskFg.y = -24 // Visual center 488, container 512, offset -24
          deskFgContainer.addChild(deskFg)
          depthGroup.addChild(deskFgContainer)
        }
        drawSecretaryDesk()

        // Draw Workstations using sprite
        const drawWorkstation = (ws: { x: number; y: number; direction: 'left' | 'right' | 'up' | 'down' }) => {
          const rectCubicle = new Rectangle(27, 28, 227, 190)
          const texCubicleDesk = new Texture({ source: texCubicle.source, frame: rectCubicle })

          const rectChair = new Rectangle(70, 752, 113, 165)
          const texChair = new Texture({ source: texCubicle.source, frame: rectChair })

          // Draw desk
          const deskContainer = new Container()
          const deskSprite = new Sprite(texCubicleDesk)
          deskSprite.anchor.set(0.5, 0.5)
          deskSprite.width = 44
          deskSprite.height = 36
          deskContainer.addChild(deskSprite)

          // Draw chair
          const chairContainer = new Container()
          const chairSprite = new Sprite(texChair)
          chairSprite.anchor.set(0.5, 0.5)
          chairSprite.width = 18
          chairSprite.height = 26
          chairContainer.addChild(chairSprite)

          // Rotate if necessary
          if (ws.direction === 'left') {
            deskContainer.angle = -90; chairContainer.angle = -90
          } else if (ws.direction === 'right') {
            deskContainer.angle = 90; chairContainer.angle = 90
          } else if (ws.direction === 'down') {
            deskContainer.angle = 180; chairContainer.angle = 180
          }

          // In Y-sorting, desk should be drawn BEFORE agent, chair should be drawn AFTER agent.
          deskContainer.x = ws.x * GRID_SIZE + 16
          deskContainer.y = ws.y * GRID_SIZE + 16 - 6 
          
          chairContainer.x = ws.x * GRID_SIZE + 16
          chairContainer.y = ws.y * GRID_SIZE + 16 + 12

          depthGroup.addChild(deskContainer)
          depthGroup.addChild(chairContainer)
        }
        Object.entries(WORKSTATIONS).forEach(([wsId, ws]) => {
          if (wsId !== 'desk-L1') { // L1 desk is drawn separately as secretary desk
            drawWorkstation(ws)
          }
        })

        // Draw Potted Plants using sprite
        const drawPlant = (gx: number, gy: number) => {
          const rect = new Rectangle(184, 92, 656, 892)
          const tex = new Texture({ source: texPlant.source, frame: rect })
          const plant = new Sprite(tex)
          plant.anchor.set(0.5, 0.8)
          plant.width = 24
          plant.height = 24 * (892 / 656)
          plant.x = gx * GRID_SIZE
          plant.y = gy * GRID_SIZE
          depthGroup.addChild(plant)
        }
        drawPlant(2.5, 3.0)
        drawPlant(9.5, 3.0)
        drawPlant(11.5, 3.0)
        drawPlant(18.5, 3.0)
        drawPlant(21.5, 3.0)
        drawPlant(28.5, 3.0)
        drawPlant(2.5, 14.5)
        drawPlant(2.5, 18.5)
        drawPlant(19.5, 14.5)
        drawPlant(22.5, 18.5)
        drawPlant(28.5, 18.5)

        // Draw Water Cooler using sprite
        const drawWaterCooler = () => {
          const rect = new Rectangle(266, 51, 431, 943)
          const tex = new Texture({ source: texWaterCooler.source, frame: rect })
          const cooler = new Sprite(tex)
          cooler.anchor.set(0.5, 0.8)
          cooler.width = 24
          cooler.height = 24 * (943 / 431)
          cooler.x = 11.5 * GRID_SIZE // Moved into breakroom
          cooler.y = 14.5 * GRID_SIZE
          depthGroup.addChild(cooler)
        }
        drawWaterCooler()

        // Draw Sofa in Breakroom
        const drawSofa = () => {
          const sofa = new Sprite(texSofa)
          sofa.anchor.set(0.5, 0.8)
          sofa.width = 96
          sofa.height = 48
          sofa.x = 15.5 * GRID_SIZE
          sofa.y = 16.0 * GRID_SIZE
          depthGroup.addChild(sofa)
        }
        drawSofa()

        const drawTable = () => {
          const tableContainer = new Container()
          tableContainer.x = 15.5 * GRID_SIZE
          tableContainer.y = 17.5 * GRID_SIZE
          
          const shadow = new Graphics()
          shadow.roundRect(-24, -12, 48, 24, 4)
          shadow.fill({ color: 0x000000, alpha: 0.2 })
          shadow.y = 4
          
          const table = new Graphics()
          table.roundRect(-24, -12, 48, 24, 4)
          table.fill(0xd4a373)
          table.stroke({ color: 0x8d6e63, width: 2 })
          
          tableContainer.addChild(shadow)
          tableContainer.addChild(table)
          depthGroup.addChild(tableContainer)
        }
        drawTable()

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

        setPixiReady(true)
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
      setDimensions({ width: currentRect.width, height: currentRect.height })
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
        if (Math.abs(dx) > 0.1 || Math.abs(dy) > 0.1) {
          if (Math.abs(dx) > Math.abs(dy)) {
            row = dx > 0 ? 2 : 1
          } else {
            row = dy > 0 ? 0 : 3
          }
        } else {
          // Stationary: face workstation direction
          const ws = WORKSTATIONS[agent.workstationId]
          if (ws) {
            if (ws.direction === 'down') row = 0
            else if (ws.direction === 'left') row = 1
            else if (ws.direction === 'right') row = 2
            else if (ws.direction === 'up') row = 3
          }
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

      // Apply mask and offset for L1 agent to sit in the executive chair
      if (agent.workstationId === 'desk-L1' && agent.status === 'working') {
        data.sprite.x = 0
        data.sprite.y = -6 // Move agent up to sit properly in the chair
        if (data.mask) {
          data.sprite.mask = null
          data.container.removeChild(data.mask)
          data.mask.destroy()
          data.mask = undefined
        }
      } else {
        data.sprite.x = 0
        data.sprite.y = 0
        if (data.mask) {
          data.sprite.mask = null
          data.container.removeChild(data.mask)
          data.mask.destroy()
          data.mask = undefined
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
    if (!world || !pixiReady) return
    const { width, height } = dimensions
    if (width === 0 || height === 0) return
    
    // Recalculate map boundaries dynamically
    let maxGridX = 26
    let maxGridY = 20
    Object.values(WORKSTATIONS).forEach(w => {
      if (w.x + 6 > maxGridX) maxGridX = w.x + 6
      if (w.y + 6 > maxGridY) maxGridY = w.y + 6
    })
    const mapW = maxGridX * GRID_SIZE
    const mapH = maxGridY * GRID_SIZE

    const visibleWidth = width - (isPanelOpen ? 340 : 0)
    const baseScale = Math.min((visibleWidth * 0.95) / mapW, (height * 0.95) / mapH)
    const renderScale = zoom * baseScale

    world.scale.set(renderScale)
    world.position.set(
      pan.x + (visibleWidth - mapW * renderScale) / 2,
      pan.y + (height - mapH * renderScale) / 2
    )
  }, [pan, zoom, isPanelOpen, pixiReady, dimensions, agents.length])

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
    // Allow much deeper zoom-out for infinite canvas feeling
    setZoom((z) => Math.max(0.1, Math.min(5, z * delta)))
  }

  const handleCanvasClick = (e: React.MouseEvent) => {
    const container = containerRef.current
    if (!container) return
    const rect = container.getBoundingClientRect()

    let maxGridX = 26
    let maxGridY = 20
    Object.values(WORKSTATIONS).forEach(w => {
      if (w.x + 6 > maxGridX) maxGridX = w.x + 6
      if (w.y + 6 > maxGridY) maxGridY = w.y + 6
    })
    const mapW = maxGridX * GRID_SIZE
    const mapH = maxGridY * GRID_SIZE

    const visibleWidth = rect.width - (isPanelOpen ? 340 : 0)
    const baseScale = Math.min((visibleWidth * 0.95) / mapW, (rect.height * 0.95) / mapH)
    const renderScale = zoom * baseScale
    const offsetX = pan.x + (visibleWidth - mapW * renderScale) / 2
    const offsetY = pan.y + (rect.height - mapH * renderScale) / 2

    const clickX = (e.clientX - rect.left - offsetX) / renderScale
    const clickY = (e.clientY - rect.top - offsetY) / renderScale

    // Secretary desk area check (grid 2-5, 11-13)
    const secDeskX = clickX / GRID_SIZE
    const secDeskY = clickY / GRID_SIZE
    if (secDeskX >= 2 && secDeskX <= 5 && secDeskY >= 11 && secDeskY <= 13) {
      sounds.playSelect()
      setIsPanelOpen(true)
      return
    }

    const clickedAgent = agents.find(agent => {
      const dx = agent.x + 16 - clickX
      const dy = agent.y + 16 - clickY
      return dx * dx + dy * dy < 250
    })

    if (clickedAgent) {
      sounds.playSelect()
      if (clickedAgent.type === 'L1') {
        setIsPanelOpen(true)
      } else if (clickedAgent.status === 'error') {
        resolveError(clickedAgent.id)
      }
    }
  }

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
              <span>Starting backend...</span>
            </div>
          )}
          {backendStatus === 'error' && (
            <div className="flex items-center gap-2 text-[10px] text-red-500">
              <span>Backend error: {backendError || 'Connection failed'}</span>
              <button
                onClick={onRetryBackend}
                className="px-2 py-0.5 bg-red-50/50 border border-red-200 rounded text-red-700 hover:bg-red-100 transition-colors"
              >
                Retry
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
      <div className="flex-1 flex flex-row min-h-0 relative bg-slate-50 overflow-hidden">
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

        {/* Side Panel Area (340px) - sliding right overlay */}
        <div className={`absolute right-0 top-0 h-full w-[340px] z-50 border-l border-gray-200 bg-white/95 backdrop-blur flex flex-col shadow-2xl transition-transform duration-300 ease-in-out ${
          isPanelOpen ? 'translate-x-0' : 'translate-x-full'
        }`}>
          {/* Tab Content */}
          <div className="flex-1 min-h-0 overflow-hidden relative">
            <SecretaryChatDialog onClose={() => setIsPanelOpen(false)} />
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