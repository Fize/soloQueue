import { useEffect, useRef, useState } from 'react'
import { useSimStore, WORKSTATIONS } from '../stores/simStore'
import { sounds } from '../utils/audio'
import { Application, Container, Sprite, Texture, Rectangle, Graphics, Text } from 'pixi.js'

// Import furniture
import imgElevator from '../assets/furniture/office_entrance.png'
import imgSecDesk from '../assets/furniture/secretary_desk.png'
import imgCubicle from '../assets/furniture/cubicle_tileset.png'
import imgPlant from '../assets/furniture/potted_plant.png'
import imgCooler from '../assets/furniture/water_cooler.png'
import SecretaryChatDialog from './SecretaryChatDialog'

// Import sprites
import spriteL1Female from '../assets/sprites/secretary_female.png'
import spriteL1Male from '../assets/sprites/secretary_male.png'
import spriteL2Female from '../assets/sprites/leader_female.png'
import spriteL2Male from '../assets/sprites/leader_male.png'
import spriteL3Female from '../assets/sprites/agent_female.png'
import spriteL3Male from '../assets/sprites/agent_male.png'

interface OfficeSceneProps {
  onOpenKanban: () => void
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
  onOpenKanban,
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
    tasks, 
    tokens, 
    resolveError
  } = useSimStore()

  // Interaction States
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null)
  const [activeTab, setActiveTab] = useState<'chat' | 'detail'>('chat')
  const [pan, setPan] = useState({ x: 0, y: 0 })
  const [zoom, setZoom] = useState(1)
  const [timeStr, setTimeStr] = useState('')

  // Drag states
  const isDragging = useRef(false)
  const dragStart = useRef({ x: 0, y: 0 })

  // PixiJS Containers
  const worldRef = useRef<Container | null>(null)
  const depthGroupRef = useRef<Container | null>(null)
  const particlesRef = useRef<Particle[]>([])

  // Store parsed textures
  const spriteTexturesRef = useRef<Record<string, Texture[][]>>({})
  const agentSpritesRef = useRef<Record<string, { container: Container; sprite: Sprite; label: Text }>>({})

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
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        onOpenKanban()
      }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'h') {
        e.preventDefault()
        onOpenShop()
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [onOpenKanban, onOpenShop])

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
          background: '#1a0f08',
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
        const grid = new Graphics()
        grid.rect(0, 0, mapW, mapH)
        grid.fill('#2c160b')
        
        // Draw grid lines
        for (let c = 0; c <= 24; c++) {
          grid.moveTo(c * GRID_SIZE, 0).lineTo(c * GRID_SIZE, mapH)
        }
        for (let r = 0; r <= 18; r++) {
          grid.moveTo(0, r * GRID_SIZE).lineTo(mapW, r * GRID_SIZE)
        }
        grid.stroke({ color: '#241208', width: 1 })
        world.addChild(grid)

        // 3. Wall boundaries (static graphic)
        const walls = new Graphics()
        // Top wall
        walls.rect(0, 0, mapW, GRID_SIZE)
        // Left wall
        walls.rect(0, 0, GRID_SIZE, mapH)
        // Bottom wall
        walls.rect(0, mapH - GRID_SIZE, mapW, GRID_SIZE)
        // Right wall
        walls.rect(mapW - GRID_SIZE, 0, GRID_SIZE, mapH)
        walls.fill('#1a0c04')
        walls.stroke({ color: '#0f0502', width: 2 })
        world.addChild(walls)

        // 4. Depth container for Y-sorted sprites
        const depthGroup = new Container()
        depthGroupRef.current = depthGroup
        world.addChild(depthGroup)

        // Load static furniture images
        const imgElev = new Image()
        imgElev.src = imgElevator
        const imgDesk = new Image()
        imgDesk.src = imgSecDesk
        const imgPlantObj = new Image()
        imgPlantObj.src = imgPlant
        const imgCoolerObj = new Image()
        imgCoolerObj.src = imgCooler
        const imgCub = new Image()
        imgCub.src = imgCubicle

        // Add static furniture sprites to depth group once images load
        imgElev.onload = () => {
          const elevTex = Texture.from(imgElev)
          const elev = new Sprite(new Texture({
            source: elevTex.source,
            frame: new Rectangle(59, 57, 1081, 781)
          }))
          elev.anchor.set(0.5, 1)
          elev.x = 19.5 * GRID_SIZE
          elev.y = 13.5 * GRID_SIZE
          elev.width = 128
          elev.height = 96
          depthGroup.addChild(elev)
        }

        imgDesk.onload = () => {
          const deskTex = Texture.from(imgDesk)
          const desk = new Sprite(new Texture({
            source: deskTex.source,
            frame: new Rectangle(97, 106, 1006, 635)
          }))
          desk.anchor.set(0.5, 1)
          desk.x = 4 * GRID_SIZE
          desk.y = 13 * GRID_SIZE
          desk.width = 100
          desk.height = 64
          depthGroup.addChild(desk)
        }

        const addPlant = (x: number, y: number) => {
          imgPlantObj.onload = () => {
            const plant = new Sprite(Texture.from(imgPlantObj))
            plant.anchor.set(0.5, 1)
            plant.x = x * GRID_SIZE
            plant.y = y * GRID_SIZE
            plant.width = 32
            plant.height = 32
            depthGroup.addChild(plant)
          }
        }
        addPlant(1.5, 12.5)
        addPlant(5.5, 7.0)
        addPlant(14.5, 13.0)

        imgCoolerObj.onload = () => {
          const cooler = new Sprite(Texture.from(imgCoolerObj))
          cooler.anchor.set(0.5, 1)
          cooler.x = 10.5 * GRID_SIZE
          cooler.y = 13.0 * GRID_SIZE
          cooler.width = 32
          cooler.height = 32
          depthGroup.addChild(cooler)
        }

        // Draw Workstations (Cubicles)
        imgCub.onload = () => {
          const cubTex = Texture.from(imgCub)
          Object.values(WORKSTATIONS).forEach((ws) => {
            const cub = new Sprite(new Texture({
              source: cubTex.source,
              frame: new Rectangle(23, 28, 231, 198)
            }))
            cub.anchor.set(0.5, 1)
            cub.x = ws.x * GRID_SIZE
            cub.y = (ws.y + 0.5) * GRID_SIZE
            cub.width = 64
            cub.height = 54
            depthGroup.addChild(cub)
          })
        }

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
  }, [])

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
        container.addChild(sprite)

        // Add label text
        const label = new Text({
          text: agent.name,
          style: {
            fontFamily: 'monospace',
            fontSize: 9,
            fill: '#ffffff',
            stroke: { color: '#000000', width: 2 },
            align: 'center',
          }
        })
        label.anchor.set(0.5, 1)
        label.y = -36 // Render name label above sprite head
        container.addChild(label)

        depthGroup.addChild(container)
        data = { container, sprite, label }
        agentSpritesRef.current[agent.id] = data
      }

      // Sync position
      data.container.x = agent.x + 16
      data.container.y = agent.y + 16

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
          data.sprite.texture = cols[frameIndex]
          data.sprite.width = cols[frameIndex].width
          data.sprite.height = cols[frameIndex].height
        }
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
  }, [agents])

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

  const currentTask = selectedAgent?.currentTaskId
    ? tasks.find(t => t.id === selectedAgent.currentTaskId)
    : null

  return (
    <div className="w-full h-full flex flex-col bg-[#1a0f08] select-none font-retro relative overflow-hidden">
      {/* Top HUD Bar (40px height) */}
      <div className="flex justify-between items-center bg-[#1a0f08]/90 backdrop-blur border-b border-[#e6b053]/20 px-4 h-10 text-[#f6ebd3] shrink-0">
        <div className="flex items-center gap-2">
          <span className="font-bold tracking-wider text-[12px] text-[#e6b053]">SOLOQUEUE INC.</span>
          <span className="font-pixel text-[8px] bg-[#e6b053]/20 text-[#e6b053] px-1.5 py-0.5 rounded">OFFICE</span>
        </div>

        {/* Backend Status Banner in the middle */}
        <div className="flex items-center gap-2">
          {backendLoading && (
            <div className="flex items-center gap-1.5 text-[10px] text-[#8c7662]">
              <span className="animate-spin inline-block w-2.5 h-2.5 border-2 border-[#e6b053] border-t-transparent rounded-full" />
              <span>启动后端中...</span>
            </div>
          )}
          {backendStatus === 'error' && (
            <div className="flex items-center gap-2 text-[10px] text-red-400">
              <span>后端异常: {backendError || '连接失败'}</span>
              <button
                onClick={onRetryBackend}
                className="px-2 py-0.5 bg-red-950/50 border border-red-500/50 rounded text-red-200 hover:bg-red-900/50 transition-colors"
              >
                重试
              </button>
            </div>
          )}
        </div>

        {/* Right side: Time, Tokens, Connection Dot */}
        <div className="flex items-center gap-4 text-[12px]">
          <div className="text-[#e6b053] font-bold">{timeStr || '09:41 PM'}</div>
          <div className="text-[#f6ebd3] font-bold">💰 {tokens.toLocaleString()}</div>
          <div className="flex items-center gap-1">
            <span className={`w-2 h-2 rounded-full ${isConnected ? 'bg-emerald-500' : 'bg-red-500 animate-pulse'}`} />
            <span className="text-[8px] font-pixel text-[#8c7662]">
              {isConnected ? 'ONLINE' : 'OFFLINE'}
            </span>
          </div>
        </div>
      </div>

      {/* Main Row: Canvas (left) + Side Panel (right) */}
      <div className="flex-1 flex flex-row min-h-0 relative bg-[#1a0f08]">
        {/* Canvas Area */}
        <div className="flex-1 h-full relative overflow-hidden bg-[#0f0a05]">
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
        <div className="w-[340px] shrink-0 border-l border-[#e6b053]/20 bg-[#1a0f08]/95 backdrop-blur flex flex-col h-full overflow-hidden">
          {/* Tabs header */}
          <div className="flex bg-[#0f0a05] border-b border-[#e6b053]/20 p-1 gap-1 shrink-0">
            <button
              onClick={() => { sounds.playSelect(); setActiveTab('chat'); }}
              className={`py-1.5 text-[10px] flex-1 rounded font-bold transition-all ${
                activeTab === 'chat'
                  ? 'bg-[#e6b053]/20 text-[#f6ebd3] border border-[#e6b053]/40'
                  : 'text-[#8c7662] hover:text-[#f6ebd3] hover:bg-[#1a0f08]'
              }`}
            >
              💬 SECRETARY CHAT
            </button>
            <button
              onClick={() => { sounds.playSelect(); setActiveTab('detail'); }}
              className={`py-1.5 text-[10px] flex-1 rounded font-bold transition-all relative ${
                activeTab === 'detail'
                  ? 'bg-[#e6b053]/20 text-[#f6ebd3] border border-[#e6b053]/40'
                  : 'text-[#8c7662] hover:text-[#f6ebd3] hover:bg-[#1a0f08]'
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
              <div className="p-4 flex flex-col h-full text-[#f6ebd3] overflow-y-auto">
                {selectedAgent ? (
                  <div className="flex-1 flex flex-col justify-between min-h-0">
                    <div className="shrink-0">
                      {/* Agent Info card */}
                      <div className="flex justify-between items-start border-b border-[#e6b053]/20 pb-3 mb-4">
                        <div>
                          <h2 className="font-bold text-[16px] text-[#f6ebd3] leading-none mb-1">
                            {selectedAgent.name}
                          </h2>
                          <span className="font-pixel text-[9px] text-[#8c7662]">
                            {selectedAgent.type} (LEVEL {selectedAgent.level})
                          </span>
                        </div>
                        <button
                          onClick={() => setSelectedAgentId(null)}
                          className="text-[#8c7662] hover:text-[#f6ebd3] font-bold text-[14px]"
                        >
                          ✕
                        </button>
                      </div>

                      {/* Status */}
                      <div className="mb-4 bg-[#241a0e] border border-[#e6b053]/15 p-3 rounded-lg">
                        <div className="flex justify-between items-center">
                          <span className="text-[12px] text-[#8c7662] font-bold">工作状态:</span>
                          <span className={`text-[12px] font-bold ${
                            selectedAgent.status === 'working' ? 'text-emerald-500' :
                            selectedAgent.status === 'error' ? 'text-red-500 animate-pulse' : 'text-[#f6ebd3]'
                          }`}>
                            {selectedAgent.status.toUpperCase()}
                          </span>
                        </div>

                        {selectedAgent.status === 'error' && (
                          <button
                            onClick={() => resolveError(selectedAgent.id)}
                            className="mt-3 block w-full py-1.5 bg-red-950 border border-red-500 text-red-200 font-bold rounded hover:bg-red-900 transition-colors text-[11px]"
                          >
                            RESOLVE PANIC
                          </button>
                        )}
                      </div>

                      {/* Task progress */}
                      {currentTask ? (
                        <div className="p-3 border border-[#e6b053]/15 bg-[#241a0e] rounded-lg">
                          <p className="font-pixel text-[8px] text-[#8c7662] mb-1.5">RUNNING TASK:</p>
                          <p className="text-[14px] font-bold text-[#f6ebd3] leading-tight mb-2.5">
                            {currentTask.title}
                          </p>
                          <div className="w-full bg-[#1a0f08] h-2.5 p-[1px] border border-[#e6b053]/20 rounded-full mb-1">
                            <div
                              className="bg-emerald-500 h-full rounded-full transition-all duration-100"
                              style={{ width: `${currentTask.progress}%` }}
                            />
                          </div>
                          <span className="font-pixel text-[8px] text-[#8c7662] float-right">
                            {Math.floor(currentTask.progress)}%
                          </span>
                        </div>
                      ) : (
                        <p className="text-[#8c7662] text-[12px] italic text-center py-6">
                          Currently resting. Assign a task from Kanban.
                        </p>
                      )}
                    </div>

                    {/* Reasoning logs */}
                    {currentTask && (
                      <div className="flex-1 flex flex-col justify-end mt-4 min-h-[160px] max-h-[300px] overflow-hidden">
                        <span className="font-pixel text-[8px] text-[#8c7662] border-b border-[#e6b053]/20 pb-1 mb-1.5">
                          REASONING LOGS
                        </span>
                        <div className="flex-1 bg-[#0f0a05] p-2.5 rounded font-mono text-[9px] text-[#4eb036] overflow-y-auto leading-tight pr-1 border border-[#e6b053]/10">
                          {currentTask.logs.slice(0, Math.floor((currentTask.progress / 100) * currentTask.logs.length) + 1).map((log, idx) => (
                            <div key={idx} className="mb-1 last:mb-0 break-words whitespace-pre-wrap">
                              {log}
                            </div>
                          ))}
                        </div>
                      </div>
                    )}
                  </div>
                ) : (
                  <div className="flex flex-col items-center justify-center h-full text-center p-4">
                    <span className="text-[28px] mb-2 animate-bounce">👥</span>
                    <p className="text-[#8c7662] text-[12px] leading-relaxed">
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
      <div className="flex justify-between items-center bg-[#1a0f08] border-t border-[#e6b053]/20 px-4 h-9 text-[#f6ebd3] shrink-0">
        <button
          onClick={onOpenKanban}
          className="flex items-center gap-1.5 cursor-pointer text-[#8c7662] hover:text-[#e6b053] hover:bg-[#e6b053]/10 px-2.5 py-1 rounded transition-all text-[11px] font-bold border border-transparent hover:border-[#e6b053]/20"
        >
          📋 Kanban <span className="font-pixel text-[8px] opacity-60">⌘K</span>
        </button>

        <div className="flex items-center gap-2 text-[10px] text-[#8c7662]">
          <span className="inline-block w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse" />
          <span>{agents.length} agents working</span>
        </div>

        <button
          onClick={onOpenShop}
          className="flex items-center gap-1.5 cursor-pointer text-[#8c7662] hover:text-[#e6b053] hover:bg-[#e6b053]/10 px-2.5 py-1 rounded transition-all text-[11px] font-bold border border-transparent hover:border-[#e6b053]/20"
        >
          🛒 Shop <span className="font-pixel text-[8px] opacity-60">⌘H</span>
        </button>
      </div>
    </div>
  )
}
