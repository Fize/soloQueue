import { useEffect, useRef, useState, useCallback } from 'react'
import { useSimStore, WORKSTATIONS } from '../store/simStore'
import { sounds } from '../utils/audio'

// Import furniture
import imgElevator from '../assets/furniture/office_entrance.png'
import imgSecDesk from '../assets/furniture/secretary_desk.png'
import SecretaryChatDialog from './SecretaryChatDialog'
import imgCubicle from '../assets/furniture/cubicle_tileset.png'
import imgPlant from '../assets/furniture/potted_plant.png'
import imgCooler from '../assets/furniture/water_cooler.png'

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
}

const GRID_SIZE = 32

// --- Particle type for agent status effects ---
interface Particle {
  x: number
  y: number
  vy: number
  life: number
  maxLife: number
  color: string
  size: number
}

/** Utility: draw an image scaled to fit a box on the canvas */
function drawImageScaled(
  ctx: CanvasRenderingContext2D,
  img: HTMLImageElement,
  cx: number,
  cy: number,
  boxW: number,
  boxH: number,
  flipX = false
) {
  ctx.save()
  ctx.translate(cx, cy)
  if (flipX) {
    ctx.scale(-1, 1)
  }
  const scale = Math.min(boxW / img.width, boxH / img.height)
  const dw = img.width * scale
  const dh = img.height * scale
  // centre the image within the box
  ctx.drawImage(img, -dw / 2, -dh, dw, dh)
  ctx.restore()
}

/** Utility: draw a cropped part of an image scaled to fit a box on the canvas */
function drawImagePartScaled(
  ctx: CanvasRenderingContext2D,
  img: HTMLImageElement,
  sx: number,
  sy: number,
  sw: number,
  sh: number,
  cx: number,
  cy: number,
  boxW: number,
  boxH: number,
  flipX = false
) {
  ctx.save()
  ctx.translate(cx, cy)
  if (flipX) {
    ctx.scale(-1, 1)
  }
  const scale = Math.min(boxW / sw, boxH / sh)
  const dw = sw * scale
  const dh = sh * scale
  // centre the image within the box
  ctx.drawImage(img, sx, sy, sw, sh, -dw / 2, -dh, dw, dh)
  ctx.restore()
}

function drawWallTile(ctx: CanvasRenderingContext2D, x: number, y: number) {
  ctx.save()
  // Shadow on the floor
  ctx.fillStyle = 'rgba(0, 0, 0, 0.2)'
  ctx.fillRect(x - 2, y, 4, GRID_SIZE)
  
  // Wall body
  ctx.fillStyle = '#3e2214' // dark wood wall body matching overall office theme
  ctx.fillRect(x, y - 16, GRID_SIZE, GRID_SIZE + 16)
  
  // Panel highlights / vertical lines
  ctx.strokeStyle = '#5a3825'
  ctx.lineWidth = 2
  ctx.beginPath()
  ctx.moveTo(x + GRID_SIZE / 2, y - 16)
  ctx.lineTo(x + GRID_SIZE / 2, y + GRID_SIZE)
  ctx.stroke()
  
  // Wall trim top
  ctx.fillStyle = '#8c5a3c' // light wood trim
  ctx.fillRect(x, y - 16, GRID_SIZE, 6)
  
  // Trim bottom outline
  ctx.strokeStyle = '#2d160b'
  ctx.lineWidth = 1
  ctx.beginPath()
  ctx.moveTo(x, y - 10)
  ctx.lineTo(x + GRID_SIZE, y - 10)
  ctx.moveTo(x, y + GRID_SIZE - 2)
  ctx.lineTo(x + GRID_SIZE, y + GRID_SIZE - 2)
  ctx.stroke()

  // Outlines
  ctx.strokeStyle = '#221100'
  ctx.lineWidth = 2
  ctx.strokeRect(x, y - 16, GRID_SIZE, GRID_SIZE + 16)
  ctx.restore()
}

function drawGate(ctx: CanvasRenderingContext2D, x: number, y: number) {
  ctx.save()
  // Draw floor guide line or mat under the gate
  ctx.fillStyle = '#5c3a21'
  ctx.fillRect(x, y + GRID_SIZE - 6, GRID_SIZE, 6)
  
  // Left post
  ctx.fillStyle = '#8c5a3c'
  ctx.strokeStyle = '#221100'
  ctx.lineWidth = 2
  ctx.fillRect(x, y - 8, 6, GRID_SIZE + 8)
  ctx.strokeRect(x, y - 8, 6, GRID_SIZE + 8)
  
  // Right post
  ctx.fillRect(x + GRID_SIZE - 6, y - 8, 6, GRID_SIZE + 8)
  ctx.strokeRect(x + GRID_SIZE - 6, y - 8, 6, GRID_SIZE + 8)
  
  // Draw gate wings (slightly open at an angle to look 3D and walkable)
  // Left wing (swinging open towards left/up)
  ctx.fillStyle = 'rgba(230, 176, 83, 0.85)' // light gold wood gate
  ctx.beginPath()
  ctx.rect(x + 6, y + 4, 10, 18)
  ctx.fill()
  ctx.stroke()
  
  // Left wing diagonal bar
  ctx.strokeStyle = '#8c5a3c'
  ctx.lineWidth = 1.5
  ctx.beginPath()
  ctx.moveTo(x + 8, y + 6)
  ctx.lineTo(x + 14, y + 20)
  ctx.stroke()

  // Right wing (swinging open towards right/up)
  ctx.fillStyle = 'rgba(230, 176, 83, 0.85)'
  ctx.beginPath()
  ctx.rect(x + GRID_SIZE - 16, y + 4, 10, 18)
  ctx.fill()
  ctx.stroke()
  
  // Right wing diagonal bar
  ctx.strokeStyle = '#8c5a3c'
  ctx.lineWidth = 1.5
  ctx.beginPath()
  ctx.moveTo(x + GRID_SIZE - 14, y + 6)
  ctx.lineTo(x + GRID_SIZE - 8, y + 20)
  ctx.stroke()
  
  // Little green indicator light on the posts
  ctx.fillStyle = '#39ff14' // neon green
  ctx.beginPath()
  ctx.arc(x + 3, y - 4, 2, 0, Math.PI * 2)
  ctx.arc(x + GRID_SIZE - 3, y - 4, 2, 0, Math.PI * 2)
  ctx.fill()

  ctx.restore()
}


interface SpriteFrame {
  sx: number
  sy: number
  sw: number
  sh: number
}

/** Utility: parse sprite sheet by scanning columns dynamically for contiguous active segments */
function parseSpriteSheet(img: HTMLImageElement): SpriteFrame[][] {
  const canvas = document.createElement('canvas')
  canvas.width = img.width
  canvas.height = img.height
  const ctx = canvas.getContext('2d')
  if (!ctx) return []
  ctx.drawImage(img, 0, 0)
  
  let imgData: ImageData
  try {
    imgData = ctx.getImageData(0, 0, img.width, img.height)
  } catch (e) {
    console.error("Failed to get image data for sprite sheet:", e)
    return []
  }
  const data = imgData.data

  const rows = 4
  const cellH = img.height / rows
  const frames: SpriteFrame[][] = []

  for (let r = 0; r < rows; r++) {
    const rowFrames: SpriteFrame[] = []
    const yStart = r * cellH
    const yEnd = (r + 1) * cellH

    // Scan columns in this row to see if they have any pixels with alpha > 10
    const colHasPixels = new Array(img.width).fill(false)
    for (let x = 0; x < img.width; x++) {
      for (let y = Math.floor(yStart); y < Math.ceil(yEnd); y++) {
        const idx = (y * img.width + x) * 4
        if (data[idx + 3] > 10) {
          colHasPixels[x] = true
          break
        }
      }
    }

    // Find contiguous segments of true
    const segments: { start: number; end: number }[] = []
    let inSegment = false
    let start = 0
    for (let x = 0; x < img.width; x++) {
      if (colHasPixels[x] && !inSegment) {
        start = x
        inSegment = true
      } else if (!colHasPixels[x] && inSegment) {
        const width = x - start
        if (width > 10) { // Filter out noise columns
          segments.push({ start, end: x - 1 })
        }
        inSegment = false
      }
    }
    if (inSegment) {
      const width = img.width - start
      if (width > 10) {
        segments.push({ start, end: img.width - 1 })
      }
    }

    // For each segment, compute the tight vertical bounding box
    segments.forEach(seg => {
      let yMin = yEnd
      let yMax = yStart
      let hasPixels = false

      for (let y = Math.floor(yStart); y < Math.ceil(yEnd); y++) {
        for (let x = seg.start; x <= seg.end; x++) {
          const idx = (y * img.width + x) * 4
          if (data[idx + 3] > 10) {
            if (y < yMin) yMin = y
            if (y > yMax) yMax = y
            hasPixels = true
          }
        }
      }

      if (hasPixels && yMin <= yMax) {
        rowFrames.push({
          sx: seg.start,
          sy: yMin,
          sw: seg.end - seg.start + 1,
          sh: yMax - yMin + 1
        })
      } else {
        rowFrames.push({
          sx: seg.start,
          sy: yStart,
          sw: seg.end - seg.start + 1,
          sh: cellH
        })
      }
    })

    // Fallback: if no segments found, default to splitting the row into 8 columns
    if (rowFrames.length === 0) {
      const cols = 8
      const cellW = img.width / cols
      for (let c = 0; c < cols; c++) {
        rowFrames.push({
          sx: c * cellW,
          sy: yStart,
          sw: cellW,
          sh: cellH
        })
      }
    }

    frames.push(rowFrames)
  }

  return frames
}


export default function OfficeScene({ onOpenKanban, onOpenShop }: OfficeSceneProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null)
  const { agents, tasks, tokens, resolveError, addLog } = useSimStore()

  const [pan, setPan] = useState({ x: 0, y: 0 })
  const [zoom, setZoom] = useState(1.0)
  const [timeStr, setTimeStr] = useState('')

  useEffect(() => {
    const updateClock = () => {
      const now = new Date()
      let hours = now.getHours()
      const minutes = now.getMinutes()
      const ampm = hours >= 12 ? 'PM' : 'AM'
      hours = hours % 12
      hours = hours ? hours : 12 // the hour '0' should be '12'
      const minStr = minutes < 10 ? '0' + minutes : minutes
      const hrStr = hours < 10 ? '0' + hours : hours
      setTimeStr(`${hrStr}:${minStr} ${ampm}`)
    }
    updateClock()
    const timer = setInterval(updateClock, 1000)
    return () => clearInterval(timer)
  }, [])
  const [isDragging, setIsDragging] = useState(false)
  const [dragStart, setDragStart] = useState({ x: 0, y: 0 })
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null)

  const imagesRef = useRef<Record<string, HTMLImageElement | undefined>>({})
  const spriteFramesRef = useRef<Record<string, SpriteFrame[][]>>({})
  const [assetsLoaded, setAssetsLoaded] = useState(false)

  // Particle system ref
  const particlesRef = useRef<Particle[]>([])
  const particleTimerRef = useRef<Record<string, number>>({})
  const lastTimeRef = useRef<number>(0)


  // Pre-load all images
  useEffect(() => {
    const sources: Record<string, string> = {
      elevator: imgElevator,
      secDesk: imgSecDesk,
      cubicle: imgCubicle,
      plant: imgPlant,
      cooler: imgCooler,
      L1_female: spriteL1Female,
      L1_male: spriteL1Male,
      L2_female: spriteL2Female,
      L2_male: spriteL2Male,
      L3_female: spriteL3Female,
      L3_male: spriteL3Male,
    }
    let loaded = 0
    const keys = Object.keys(sources)
    console.log("OfficeScene: Starting preloading", keys.length, "assets...");

    const parseAndSetLoaded = () => {
      const charKeys = ['L1_female', 'L1_male', 'L2_female', 'L2_male', 'L3_female', 'L3_male']
      charKeys.forEach(k => {
        const charImg = imagesRef.current[k]
        if (charImg) {
          try {
            spriteFramesRef.current[k] = parseSpriteSheet(charImg)
          } catch (err) {
            console.error(`Failed to parse sprite sheet ${k}:`, err)
          }
        }
      })
      setAssetsLoaded(true)
    }

    keys.forEach(key => {
      const img = new Image()
      img.src = sources[key]
      img.onload = () => {
        imagesRef.current[key] = img
        loaded++
        console.log("OfficeScene: Loaded asset:", key, `(${loaded}/${keys.length})`);
        if (loaded === keys.length) {
          console.log("OfficeScene: All assets loaded successfully!");
          parseAndSetLoaded()
        }
      }
      img.onerror = () => {
        console.warn('Failed to load asset:', key)
        loaded++
        if (loaded === keys.length) {
          console.log("OfficeScene: All assets finished with some warnings!");
          parseAndSetLoaded()
        }
      }
    })
  }, [])

  // Main draw loop
  const draw = useCallback((timestamp: number) => {
    const canvas = canvasRef.current
    if (!canvas) return
    const ctx = canvas.getContext('2d')
    if (!ctx) return

    // Delta time calculation
    const dt = lastTimeRef.current === 0 ? 0.016 : Math.min((timestamp - lastTimeRef.current) / 1000, 0.05)
    lastTimeRef.current = timestamp

    const cols = 24
    const rows = 18

    // Resize canvas to fit its container while keeping aspect ratio
    const rect = canvas.getBoundingClientRect()
    const dpr = window.devicePixelRatio || 1
    if (canvas.width !== rect.width * dpr || canvas.height !== rect.height * dpr) {
      canvas.width = rect.width * dpr
      canvas.height = rect.height * dpr
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
    }

    // Clear
    ctx.fillStyle = '#5a2800'
    ctx.fillRect(0, 0, rect.width, rect.height)

    ctx.save()

    // Auto-center and scale map based on canvas size
    const mapW = cols * GRID_SIZE
    const mapH = rows * GRID_SIZE
    const baseScale = Math.min((rect.width * 0.95) / mapW, (rect.height * 0.95) / mapH)
    const renderScale = zoom * baseScale
    const offsetX = pan.x + (rect.width - mapW * renderScale) / 2
    const offsetY = pan.y + (rect.height - mapH * renderScale) / 2

    ctx.translate(offsetX, offsetY)
    ctx.scale(renderScale, renderScale)

    // Calculate visible bounds in map coordinates to render infinitely tiled floor
    const startX = -offsetX / renderScale - 128
    const startY = -offsetY / renderScale - 128
    const endX = (rect.width - offsetX) / renderScale + 128
    const endY = (rect.height - offsetY) / renderScale + 128

    // ========================================
    // 1. Wood Floor — Infinite Staggered Vertical Planks
    // ========================================
    const plankW = 16
    const plankH = 64
    const firstCol = Math.floor(startX / plankW)
    const lastCol = Math.ceil(endX / plankW)

    for (let colIdx = firstCol; colIdx <= lastCol; colIdx++) {
      const px = colIdx * plankW
      const yOffset = (Math.abs(colIdx) % 2) * 32
      const firstRow = Math.floor((startY - yOffset) / plankH)
      const lastRow = Math.ceil((endY - yOffset) / plankH)
      for (let rowIdx = firstRow; rowIdx <= lastRow; rowIdx++) {
        const py = rowIdx * plankH + yOffset
        // Deterministic color
        const colors = ['#7d6553', '#7f695b', '#755d4c', '#6d5544']
        const hash = Math.abs((colIdx * 17 + rowIdx * 23) % colors.length)
        
        ctx.fillStyle = colors[hash]
        ctx.fillRect(px, py, plankW, plankH)
        
        // Plank outline (grout line)
        ctx.strokeStyle = '#553d30'
        ctx.lineWidth = 1
        ctx.strokeRect(px, py, plankW, plankH)
      }
    }

    // ========================================
    // 2. Middle Corridor & Roads — Infinite Tiled Stones
    // ========================================
    const corridorY = 8 * GRID_SIZE
    const corridorH = 2 * GRID_SIZE
    
    // Fill main corridor with stone tiles infinitely
    const firstCorridorX = Math.floor(startX / GRID_SIZE) * GRID_SIZE
    const lastCorridorX = Math.ceil(endX / GRID_SIZE) * GRID_SIZE
    for (let cx = firstCorridorX; cx <= lastCorridorX; cx += GRID_SIZE) {
      for (let cy = corridorY; cy < corridorY + corridorH; cy += GRID_SIZE) {
        const hash = Math.abs((Math.floor(cx / GRID_SIZE) * 7 + Math.floor(cy / GRID_SIZE) * 13) % 2)
        ctx.fillStyle = hash === 0 ? '#3c4652' : '#323b45'
        ctx.fillRect(cx, cy, GRID_SIZE, GRID_SIZE)
        
        ctx.strokeStyle = '#28303a'
        ctx.lineWidth = 1
        ctx.strokeRect(cx, cy, GRID_SIZE, GRID_SIZE)
      }
    }

    // Fill vertical branch path (column 15, rows 10..13) with stone tiles
    const sidePathX = 15 * GRID_SIZE
    for (let cy = 10 * GRID_SIZE; cy < 14 * GRID_SIZE; cy += GRID_SIZE) {
      const hash = Math.abs((15 * 7 + Math.floor(cy / GRID_SIZE) * 13) % 2)
      ctx.fillStyle = hash === 0 ? '#3c4652' : '#323b45'
      ctx.fillRect(sidePathX, cy, GRID_SIZE, GRID_SIZE)
      
      ctx.strokeStyle = '#28303a'
      ctx.lineWidth = 1
      ctx.strokeRect(sidePathX, cy, GRID_SIZE, GRID_SIZE)
    }

    // Fill elevator lobby floor (columns 16..23, rows 10..16) with stone tiles
    const totalW = cols * GRID_SIZE
    const totalH = rows * GRID_SIZE
    for (let cx = 16 * GRID_SIZE; cx < totalW; cx += GRID_SIZE) {
      for (let cy = 10 * GRID_SIZE; cy < 17 * GRID_SIZE; cy += GRID_SIZE) {
        const hash = Math.abs((Math.floor(cx / GRID_SIZE) * 7 + Math.floor(cy / GRID_SIZE) * 13) % 2)
        ctx.fillStyle = hash === 0 ? '#3c4652' : '#323b45'
        ctx.fillRect(cx, cy, GRID_SIZE, GRID_SIZE)
        
        ctx.strokeStyle = '#28303a'
        ctx.lineWidth = 1
        ctx.strokeRect(cx, cy, GRID_SIZE, GRID_SIZE)
      }
    }

    // Corridor borders (thick dark wood borders for high visual contrast)
    ctx.strokeStyle = '#1e252d'
    ctx.lineWidth = 3
    ctx.beginPath()
    // Top border of main corridor (infinite)
    ctx.moveTo(startX, corridorY)
    ctx.lineTo(endX, corridorY)
    // Bottom border of main corridor (up to side path)
    ctx.moveTo(startX, corridorY + corridorH)
    ctx.lineTo(15 * GRID_SIZE, corridorY + corridorH)
    // Bottom border of main corridor (after elevator lobby)
    ctx.moveTo(16 * GRID_SIZE, corridorY + corridorH)
    ctx.lineTo(endX, corridorY + corridorH)
    
    // Left border of the vertical side path (separating it from wood floor / break room)
    ctx.moveTo(15 * GRID_SIZE, 10 * GRID_SIZE)
    ctx.lineTo(15 * GRID_SIZE, 14 * GRID_SIZE)
    ctx.stroke()

    // Corridor label: centered horizontally and vertically
    ctx.save()
    ctx.fillStyle = '#9aa1a9'
    ctx.font = 'bold 13px VT323'
    ctx.textAlign = 'center'
    ctx.textBaseline = 'middle'
    
    const labelText = '═ MAIN CORRIDOR ═'
    const labelX = totalW / 2
    const labelY = corridorY + corridorH / 2
    
    // Clear a small box for the label text
    const textWidth = ctx.measureText(labelText).width
    ctx.fillStyle = '#323b45'
    ctx.fillRect(labelX - textWidth / 2 - 6, labelY - 10, textWidth + 12, 20)
    
    ctx.fillStyle = '#9aa1a9'
    ctx.fillText(labelText, labelX, labelY)
    ctx.restore()

    // ========================================
    // 3. Team Areas — Dynamic backings & labels
    // ========================================
    const maxWorkstationX = Math.max(24, ...Object.values(WORKSTATIONS).map(w => w.x))
    const teamBoxes = [
      { xMin: 1.5, xMax: 8.5, yMin: 1.5, yMax: 7.5, label: 'Team A' },
      { xMin: 9.5, xMax: 15.5, yMin: 1.5, yMax: 7.5, label: 'Team B' },
      { xMin: 16.5, xMax: 22.5, yMin: 1.5, yMax: 7.5, label: 'Team C' }
    ]

    // Dynamically add team boxes for columns beyond 23
    const maxCols = Math.ceil(maxWorkstationX / 8) * 8
    let teamLetterCode = 'D'.charCodeAt(0)
    for (let bxStart = 24.5; bxStart < maxCols; bxStart += 8) {
      const label = `Team ${String.fromCharCode(teamLetterCode)}`
      teamBoxes.push({
        xMin: bxStart,
        xMax: bxStart + 7,
        yMin: 1.5,
        yMax: 7.5,
        label
      })
      teamLetterCode++
      if (teamLetterCode > 'Z'.charCodeAt(0)) teamLetterCode = 'A'.charCodeAt(0)
    }

    teamBoxes.forEach(box => {
      const bx = box.xMin * GRID_SIZE
      const by = box.yMin * GRID_SIZE
      const bw = (box.xMax - box.xMin) * GRID_SIZE
      const bh = (box.yMax - box.yMin) * GRID_SIZE

      // Faint transparent backing
      ctx.fillStyle = 'rgba(255, 255, 255, 0.02)'
      ctx.strokeStyle = 'rgba(255, 255, 255, 0.06)'
      ctx.lineWidth = 1
      ctx.beginPath()
      ctx.roundRect(bx, by, bw, bh, 6)
      ctx.fill()
      ctx.stroke()

      // Draw team label above the desks (around y = by + 12)
      ctx.fillStyle = '#d29a38' // Yellow/gold color
      ctx.font = 'bold 15px VT323'
      ctx.textAlign = 'left'
      ctx.textBaseline = 'top'
      ctx.fillText(box.label, bx + 12, by + 12)
    })

    // ========================================
    // Break Room Zone (Startup Company style)
    // ========================================
    const bxMin = 9.5 * GRID_SIZE
    const bxMax = 14.5 * GRID_SIZE
    const byMin = 11.5 * GRID_SIZE
    const byMax = 15.5 * GRID_SIZE
    const brw = bxMax - bxMin
    const brh = byMax - byMin

    // Draw carpet backing
    ctx.fillStyle = '#662222' // Warm dark red carpet
    ctx.strokeStyle = '#8c3a3a'
    ctx.lineWidth = 1
    ctx.beginPath()
    ctx.roundRect(bxMin, byMin, brw, brh, 6)
    ctx.fill()
    ctx.stroke()

    // Draw border line
    ctx.save()
    ctx.strokeStyle = '#8c3a3a'
    ctx.lineWidth = 2
    ctx.setLineDash([4, 4])
    ctx.strokeRect(bxMin + 2, byMin + 2, brw - 4, brh - 4)
    ctx.restore()

    // Label
    ctx.save()
    ctx.fillStyle = '#e59b9b'
    ctx.font = 'bold 13px VT323'
    ctx.textAlign = 'center'
    ctx.textBaseline = 'middle'
    ctx.fillText('☕ BREAK ZONE', bxMin + brw / 2, byMin + 12)
    ctx.restore()

    // ========================================
    // 4. Task Boards — rectangular boards with post-it notes
    // ========================================
    const drawTaskBoard = (tx: number, ty: number, w: number, h: number) => {
      // Background board
      ctx.fillStyle = '#b6a084'
      ctx.strokeStyle = '#553d30'
      ctx.lineWidth = 2
      ctx.fillRect(tx, ty, w, h)
      ctx.strokeRect(tx, ty, w, h)

      // Post-its
      const postItColors = ['#f37a7a', '#7af37a', '#7a7af3', '#f3f37a', '#f37af3']
      const numPostIts = 5
      for (let i = 0; i < numPostIts; i++) {
        const px = tx + 8 + i * 18 + (i % 2) * 2
        const py = ty + 4 + (i % 3) * 1.5
        const pSize = 8
        ctx.fillStyle = postItColors[(i * 3) % postItColors.length]
        ctx.fillRect(px, py, pSize, pSize)
        ctx.strokeStyle = 'rgba(0,0,0,0.15)'
        ctx.lineWidth = 0.5
        ctx.strokeRect(px, py, pSize, pSize)
      }
    }

    // Hang task boards on the back wall inside each team zone dynamically
    teamBoxes.forEach(box => {
      drawTaskBoard((box.xMin + 1) * GRID_SIZE, 1.6 * GRID_SIZE, 110, 18)
    })

    // Outer walls (Left, Top, Bottom)
    ctx.strokeStyle = '#381a04'
    ctx.lineWidth = 4
    ctx.beginPath()
    // Top wall
    ctx.moveTo(startX, 2)
    ctx.lineTo(endX, 2)
    // Bottom wall
    ctx.moveTo(startX, totalH - 2)
    ctx.lineTo(endX, totalH - 2)
    // Left wall
    ctx.moveTo(2, 2)
    ctx.lineTo(2, totalH - 2)
    ctx.stroke()

    // Reset textBaseline
    ctx.textBaseline = 'alphabetic'

    // ========================================
    // 5. Sorted Render List (Y-Sorting / Depth Sorting)
    // ========================================
    interface SortableRenderItem {
      y: number
      draw: (ctx: CanvasRenderingContext2D) => void
    }
    const renderItems: SortableRenderItem[] = []

    const imgElev = imagesRef.current['elevator']
    const imgDesk = imagesRef.current['secDesk']
    const imgPlantObj = imagesRef.current['plant']
    const imgCoolerObj = imagesRef.current['cooler']
    const imgCub = imagesRef.current['cubicle']

    // Elevator (Enclosed in a recessed Wall Niche)
    if (imgElev) {
      renderItems.push({
        y: 13.5 * GRID_SIZE,
        draw: (ctx) => {
          // 1. Draw solid wood backing & side wall casings to integrate elevator lobby
          ctx.fillStyle = '#3e2214' // Wood wall color matching outer walls
          ctx.strokeStyle = '#5a3825'
          ctx.lineWidth = 2
          
          // Left wall column
          ctx.fillRect(17.2 * GRID_SIZE, 10.5 * GRID_SIZE, 10, 96)
          ctx.strokeRect(17.2 * GRID_SIZE, 10.5 * GRID_SIZE, 10, 96)

          // Right wall column
          ctx.fillRect(21.5 * GRID_SIZE, 10.5 * GRID_SIZE, 10, 96)
          ctx.strokeRect(21.5 * GRID_SIZE, 10.5 * GRID_SIZE, 10, 96)

          // Header wall panel
          ctx.fillRect(17.2 * GRID_SIZE, 10.5 * GRID_SIZE, 148, 16)
          ctx.strokeRect(17.2 * GRID_SIZE, 10.5 * GRID_SIZE, 148, 16)

          // 2. Draw elevator doors inside the niche
          drawImagePartScaled(ctx, imgElev, 59, 57, 1081, 781, 19.5 * GRID_SIZE, 13.5 * GRID_SIZE, 128, 96)
          
          // 3. Draw niche shadow overlay to create depth
          ctx.fillStyle = 'rgba(0, 0, 0, 0.45)'
          ctx.fillRect(17.5 * GRID_SIZE, 11.0 * GRID_SIZE, 128, 8)

          // Green indicator above elevator door
          ctx.fillStyle = '#000000'
          ctx.fillRect(19.5 * GRID_SIZE - 12, 11.2 * GRID_SIZE, 24, 12)
          ctx.fillStyle = '#39ff14' // neon green
          ctx.font = 'bold 10px monospace'
          ctx.textAlign = 'center'
          ctx.textBaseline = 'middle'
          ctx.fillText('12', 19.5 * GRID_SIZE, 11.2 * GRID_SIZE + 6)
          ctx.textBaseline = 'alphabetic'
        }
      })
    }

    // Secretary Desk (Cropped and Scaled)
    if (imgDesk) {
      renderItems.push({
        y: 13 * GRID_SIZE,
        draw: (ctx) => drawImagePartScaled(ctx, imgDesk, 97, 106, 1006, 635, 4 * GRID_SIZE, 13 * GRID_SIZE, 100, 64)
      })
    }

    // Plants
    if (imgPlantObj) {
      // Plant next to secretary desk
      renderItems.push({
        y: 12.5 * GRID_SIZE,
        draw: (ctx) => drawImageScaled(ctx, imgPlantObj, 1.5 * GRID_SIZE, 12.5 * GRID_SIZE, 32, 32)
      })
      // Plant in Team A cluster (5, 6)
      renderItems.push({
        y: 7.0 * GRID_SIZE,
        draw: (ctx) => drawImageScaled(ctx, imgPlantObj, 5.5 * GRID_SIZE, 7.0 * GRID_SIZE, 32, 32)
      })
      // Plant in Break Room corner (14.5, 12.5)
      renderItems.push({
        y: 13.0 * GRID_SIZE,
        draw: (ctx) => drawImageScaled(ctx, imgPlantObj, 14.5 * GRID_SIZE, 13.0 * GRID_SIZE, 32, 32)
      })
    }

    // Cooler (Relocated into Break Room)
    if (imgCoolerObj) {
      renderItems.push({
        y: 13.0 * GRID_SIZE,
        draw: (ctx) => drawImageScaled(ctx, imgCoolerObj, 10.5 * GRID_SIZE, 13.0 * GRID_SIZE, 32, 32)
      })
    }

    // Break Room Coffee Counter & Table
    renderItems.push({
      y: 13.0 * GRID_SIZE,
      draw: (ctx) => {
        // Draw counter table
        ctx.fillStyle = '#5c3a21' // Oak wood
        ctx.strokeStyle = '#382213'
        ctx.lineWidth = 2
        ctx.fillRect(13 * GRID_SIZE, 12 * GRID_SIZE - 8, 32, 24)
        ctx.strokeRect(13 * GRID_SIZE, 12 * GRID_SIZE - 8, 32, 24)
        
        // Draw coffee machine on top
        ctx.fillStyle = '#2d2d2d' // dark metallic
        ctx.fillRect(13.2 * GRID_SIZE, 11.5 * GRID_SIZE - 8, 16, 16)
        ctx.fillStyle = '#ff3333' // power button glow
        ctx.fillRect(13.3 * GRID_SIZE, 11.8 * GRID_SIZE - 8, 2, 2)
        
        // Steam particles
        ctx.strokeStyle = 'rgba(255, 255, 255, 0.4)'
        ctx.lineWidth = 1
        ctx.beginPath()
        const t = Date.now() / 200
        ctx.moveTo(13.45 * GRID_SIZE, 11.3 * GRID_SIZE - 8 + Math.sin(t) * 2)
        ctx.quadraticCurveTo(
          13.5 * GRID_SIZE + Math.cos(t) * 2, 11.1 * GRID_SIZE - 8,
          13.45 * GRID_SIZE + Math.sin(t) * 2, 10.9 * GRID_SIZE - 8
        )
        ctx.stroke()
      }
    })

    renderItems.push({
      y: 14.5 * GRID_SIZE,
      draw: (ctx) => {
        const tx = 12.5 * GRID_SIZE
        const ty = 14 * GRID_SIZE
        
        // Left Chair
        ctx.fillStyle = '#3c5a78'
        ctx.strokeStyle = '#1e2c3c'
        ctx.lineWidth = 1.5
        ctx.beginPath()
        ctx.roundRect(tx - 24, ty - 6, 12, 12, 3)
        ctx.fill()
        ctx.stroke()
        ctx.fillRect(tx - 26, ty - 6, 2, 12)
        
        // Right Chair
        ctx.fillStyle = '#3c5a78'
        ctx.beginPath()
        ctx.roundRect(tx + 12, ty - 6, 12, 12, 3)
        ctx.fill()
        ctx.stroke()
        ctx.fillRect(tx + 24, ty - 6, 2, 12)

        // Break table
        ctx.fillStyle = '#8c5a3c'
        ctx.strokeStyle = '#5c3a21'
        ctx.lineWidth = 2
        ctx.beginPath()
        ctx.arc(tx, ty, 14, 0, Math.PI * 2)
        ctx.fill()
        ctx.stroke()
        ctx.strokeStyle = 'rgba(255,255,255,0.08)'
        ctx.lineWidth = 1
        ctx.beginPath()
        ctx.arc(tx, ty, 8, 0, Math.PI * 2)
        ctx.stroke()
      }
    })

    // Secretary label
    renderItems.push({
      y: 14.5 * GRID_SIZE,
      draw: (ctx) => {
        ctx.fillStyle = '#d29a38'
        ctx.font = 'bold 13px VT323'
        ctx.textAlign = 'center'
        ctx.fillText('👩‍💼 L1 Secretary', 4 * GRID_SIZE, 14.2 * GRID_SIZE)
      }
    })

    // Elevator label
    renderItems.push({
      y: 14.5 * GRID_SIZE,
      draw: (ctx) => {
        ctx.fillStyle = '#9aa1a9'
        ctx.font = 'bold 13px VT323'
        ctx.textAlign = 'center'
        ctx.fillText('🚪 ELEVATOR ──> ──>', 19.5 * GRID_SIZE, 14.2 * GRID_SIZE)
      }
    })

    // Cubicles (Slicing Single Workstation Desk from tileset)
    if (imgCub) {
      Object.entries(WORKSTATIONS).forEach(([id, desk]) => {
        if (id === 'desk-L1') return
        renderItems.push({
          y: (desk.y + 0.5) * GRID_SIZE,
          draw: (ctx) => drawImagePartScaled(ctx, imgCub, 23, 28, 231, 198, desk.x * GRID_SIZE, (desk.y + 0.5) * GRID_SIZE, 64, 54)
        })
      })
    }

    // Add elevator lobby partition walls
    const wallRows = [10, 11, 12, 14, 15, 16]
    wallRows.forEach(row => {
      renderItems.push({
        y: (row + 1) * GRID_SIZE,
        draw: (ctx) => drawWallTile(ctx, 16 * GRID_SIZE, row * GRID_SIZE)
      })
    })

    // Add elevator lobby gate
    renderItems.push({
      y: 14 * GRID_SIZE,
      draw: (ctx) => drawGate(ctx, 16 * GRID_SIZE, 13 * GRID_SIZE)
    })


    // Agents
    agents.forEach(agent => {
      const spriteKey = `${agent.type}_${agent.gender}`
      const imgSprite = imagesRef.current[spriteKey]

      const isL1 = agent.type === 'L1'
      // L1 Chief Secretary sits on the chair (sort Y slightly larger than desk to render on top of chair)
      const sortY = isL1 ? 13 * GRID_SIZE + 2 : agent.y + 32
      const drawX = isL1 ? 4 * GRID_SIZE : agent.x + 16
      const drawY = isL1 ? 12.0 * GRID_SIZE : agent.y + 32

      renderItems.push({
        y: sortY,
        draw: (ctx) => {
          if (imgSprite) {
            const frames = spriteFramesRef.current[spriteKey]
            let frameToDraw: SpriteFrame | null = null
            
            let col = 0
            let row = 0
            let flipX = false

            // 1. Calculate row, col, flipX
            if (agent.type === 'L1') {
              if (agent.status === 'working') {
                row = 1
              } else if (agent.status === 'error') {
                row = 2
              } else {
                row = 0
              }
            } else {
              if (agent.path.length > 0) {
                const nextNode = agent.path[0]
                const dx = nextNode.x - agent.x
                const dy = nextNode.y - agent.y

                if (Math.abs(dx) > Math.abs(dy)) {
                  if (dx > 0) {
                    row = 2
                    flipX = true
                  } else {
                    row = 2
                    flipX = false
                  }
                } else {
                  if (dy > 0) {
                    row = 0
                    flipX = false
                  } else {
                    row = 1
                    flipX = false
                  }
                }
              } else {
                if (agent.status === 'working' || agent.status === 'error') {
                  const desk = WORKSTATIONS[agent.workstationId]
                  if (desk) {
                    if (desk.direction === 'up') {
                      row = 1
                      flipX = false
                    } else if (desk.direction === 'down') {
                      row = 0
                      flipX = false
                    } else if (desk.direction === 'left') {
                      row = 2
                      flipX = false
                    } else if (desk.direction === 'right') {
                      row = 2
                      flipX = true
                    }
                  }
                } else {
                  row = 0
                  flipX = false
                }
              }
            }

            // 2. Select frame (from frames array or fall back to grid slice)
            if (frames && frames.length >= 4 && frames[row] && frames[row].length > 0) {
              const frameRow = frames[row]
              if (agent.type === 'L1') {
                col = frameRow.length >= 8 ? 4 + (agent.frame % 4) : agent.frame % frameRow.length
              } else {
                if (agent.path.length > 0) {
                  col = frameRow.length === 9 ? agent.frame % 6 : agent.frame % 4
                } else if (agent.status === 'working' || agent.status === 'error') {
                  col = frameRow.length === 9 ? 6 + (agent.frame % 3) : 4 + (agent.frame % 4)
                } else {
                  col = frameRow.length === 9 ? agent.frame % 6 : agent.frame % 4
                }
              }
              frameToDraw = frameRow[col % frameRow.length]
            }

            // 3. Draw using frameToDraw or fall back to dynamic grid slicing
            ctx.save()
            ctx.translate(drawX, drawY)
            if (flipX) {
              ctx.scale(-1, 1)
            }

            const targetW = 32
            const targetH = 32

            if (frameToDraw) {
              const scale = Math.min(targetW / frameToDraw.sw, targetH / frameToDraw.sh)
              const dw = frameToDraw.sw * scale
              const dh = frameToDraw.sh * scale
              ctx.drawImage(
                imgSprite,
                frameToDraw.sx,
                frameToDraw.sy,
                frameToDraw.sw,
                frameToDraw.sh,
                -dw / 2,
                -dh,
                dw,
                dh
              )
            } else {
              // Dynamic grid slice fallback (e.g. 8 cols per row, 4 rows)
              const colsCount = 8
              const cellW = imgSprite.width ? imgSprite.width / colsCount : 32
              const cellH = imgSprite.height ? imgSprite.height / 4 : 32
              
              if (agent.type === 'L1') {
                col = 4 + (agent.frame % 4)
              } else {
                if (agent.path.length > 0) {
                  col = agent.frame % 4
                } else if (agent.status === 'working' || agent.status === 'error') {
                  col = 4 + (agent.frame % 4)
                } else {
                  col = agent.frame % 4
                }
              }

              const scale = Math.min(targetW / cellW, targetH / cellH)
              const dw = cellW * scale
              const dh = cellH * scale
              ctx.drawImage(
                imgSprite,
                (col % colsCount) * cellW,
                row * cellH,
                cellW,
                cellH,
                -dw / 2,
                -dh,
                dw,
                dh
              )
            }
            ctx.restore()

            // Status indicators
            if (agent.status === 'working') {
              ctx.fillStyle = 'rgba(78, 176, 54, 0.9)'
              ctx.strokeStyle = '#381a04'
              ctx.lineWidth = 1
              ctx.beginPath()
              ctx.roundRect(drawX + 4, drawY - 60, 28, 18, 4)
              ctx.fill()
              ctx.stroke()
              ctx.fillStyle = '#f6ebd3'
              ctx.font = '12px VT323'
              ctx.textAlign = 'center'
              ctx.fillText('💻', drawX + 18, drawY - 47)
            } else if (agent.status === 'error') {
              ctx.fillStyle = 'rgba(216, 56, 56, 0.95)'
              ctx.strokeStyle = '#381a04'
              ctx.lineWidth = 1
              ctx.beginPath()
              ctx.roundRect(drawX + 4, drawY - 60, 28, 18, 4)
              ctx.fill()
              ctx.stroke()
              ctx.fillStyle = '#f6ebd3'
              ctx.font = 'bold 12px VT323'
              ctx.textAlign = 'center'
              ctx.fillText('⚠️', drawX + 18, drawY - 47)
            }
          } else {
            // Fallback circle representation
            ctx.fillStyle =
              agent.type === 'L1' ? '#f3b72b' :
              agent.type === 'L2' ? '#e28a2b' : '#7ca84c'
            ctx.beginPath()
            ctx.arc(drawX, drawY - 16, 10, 0, Math.PI * 2)
            ctx.fill()
          }
        }
      })
    })

    // Stable sort and render items
    renderItems.sort((a, b) => a.y - b.y)
    renderItems.forEach(item => item.draw(ctx))

    // ========================================
    // 6. Atmospheric Desk Lighting
    // ========================================
    ctx.save()
    ctx.globalCompositeOperation = 'screen'
    Object.values(WORKSTATIONS).forEach(desk => {
      const cx = desk.x * GRID_SIZE + GRID_SIZE / 2
      const cy = desk.y * GRID_SIZE + GRID_SIZE / 2
      const grad = ctx.createRadialGradient(cx, cy, 0, cx, cy, 60)
      grad.addColorStop(0, 'rgba(243, 183, 43, 0.12)')
      grad.addColorStop(1, 'rgba(243, 183, 43, 0)')
      ctx.fillStyle = grad
      ctx.beginPath()
      ctx.arc(cx, cy, 60, 0, Math.PI * 2)
      ctx.fill()
    })
    ctx.globalCompositeOperation = 'source-over'
    ctx.restore()

    // ========================================
    // 7. Agent Status Particles — update & draw
    // ========================================
    // Spawn particles for working/error agents
    agents.forEach(agent => {
      if (agent.status === 'working' || agent.status === 'error') {
        const timerId = agent.id
        const elapsed = (particleTimerRef.current[timerId] ?? 0) + dt
        const spawnInterval = agent.status === 'working' ? 0.3 : 0.25

        if (elapsed >= spawnInterval) {
          particleTimerRef.current[timerId] = 0
          const isError = agent.status === 'error'
          particlesRef.current.push({
            x: agent.x + 16 + (Math.random() - 0.5) * 10,
            y: agent.y + 8,
            vy: isError ? -20 : -30,
            life: isError ? 0.8 : 1.0,
            maxLife: isError ? 0.8 : 1.0,
            color: isError ? 'rgba(216, 56, 56, 0.7)' : 'rgba(78, 176, 54, 0.7)',
            size: isError ? 4 : 3,
          })
        } else {
          particleTimerRef.current[timerId] = elapsed
        }
      }
    })

    // Update & draw particles
    const aliveParticles: Particle[] = []
    particlesRef.current.forEach(p => {
      p.y += p.vy * dt
      p.life -= dt
      if (p.life > 0) {
        const alpha = Math.max(0, p.life / p.maxLife)
        ctx.globalAlpha = alpha
        // Parse base color and apply current alpha
        ctx.fillStyle = p.color
        ctx.fillRect(p.x - p.size / 2, p.y - p.size / 2, p.size, p.size)
        aliveParticles.push(p)
      }
    })
    ctx.globalAlpha = 1.0
    particlesRef.current = aliveParticles

    // ========================================
    // 8. Vignette overlay
    // ========================================
    ctx.save()
    ctx.globalCompositeOperation = 'multiply'
    const vigCx = totalW / 2
    const vigCy = totalH / 2
    const vigRadius = Math.max(totalW, totalH) * 0.7
    const vigGrad = ctx.createRadialGradient(vigCx, vigCy, vigRadius * 0.4, vigCx, vigCy, vigRadius)
    vigGrad.addColorStop(0, 'rgba(255, 255, 255, 1)')
    vigGrad.addColorStop(1, 'rgba(56, 26, 4, 0.3)')
    ctx.fillStyle = vigGrad
    ctx.fillRect(0, 0, totalW, totalH)
    ctx.globalCompositeOperation = 'source-over'
    ctx.restore()

    ctx.restore()
  }, [agents, pan, zoom, assetsLoaded])

  const drawRef = useRef(draw)
  useEffect(() => {
    drawRef.current = draw
  }, [draw])

  useEffect(() => {
    if (!assetsLoaded) return
    let rafId = 0
    const loop = (timestamp: number) => {
      drawRef.current(timestamp)
      rafId = requestAnimationFrame(loop)
    }
    rafId = requestAnimationFrame(loop)
    return () => cancelAnimationFrame(rafId)
  }, [assetsLoaded])

  // Mouse interactions
  const handleMouseDown = (e: React.MouseEvent) => {
    setIsDragging(true)
    setDragStart({ x: e.clientX - pan.x, y: e.clientY - pan.y })
  }

  const handleMouseMove = (e: React.MouseEvent) => {
    if (!isDragging) return
    setPan({
      x: e.clientX - dragStart.x,
      y: e.clientY - dragStart.y,
    })
  }

  const handleMouseUp = () => setIsDragging(false)

  const handleWheel = (e: React.WheelEvent) => {
    const scale = e.deltaY < 0 ? 1.05 : 0.95
    setZoom(z => Math.max(0.5, Math.min(2.5, z * scale)))
  }

  const handleCanvasClick = (e: React.MouseEvent) => {
    if (isDragging) return
    const canvas = canvasRef.current
    if (!canvas) return
    const rect = canvas.getBoundingClientRect()

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
      setShowChat(true)
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
      if (clickedAgent.status === 'error') {
        resolveError(clickedAgent.id)
      }
    } else {
      setSelectedAgentId(null)
    }
  }

  // Selected agent info
  const selectedAgent = agents.find(a => a.id === selectedAgentId)

  const [showChat, setShowChat] = useState(false)

  const currentTask = selectedAgent?.currentTaskId
    ? tasks.find(t => t.id === selectedAgent.currentTaskId)
    : null

  const isMac = typeof window !== 'undefined' && navigator.userAgent.toLowerCase().includes('mac')

  return (
    <div className={`w-full h-full flex flex-col bg-[#3c2214] p-1.5 ${isMac ? 'pt-9' : ''} border-4 border-[#e6b053] select-none font-retro`}>
      {/* Top HUD Bar */}
      <div className="flex justify-between items-center bg-[#5f3e26] border-2 border-[#e6b053] px-4 py-2.5 text-[#f6ebd3] text-[18px] mb-1.5">
        <div className="font-bold tracking-wider">SOLOQUEUE INC.</div>
        <div className="text-[#5cc4f2] font-bold">{timeStr || '09:41 PM'}</div>
        <button
          onClick={() => {
            sounds.playSelect()
            addLog("[SYSTEM] Audio settings loaded.")
          }}
          className="cursor-pointer hover:text-white transition-colors flex items-center gap-1.5"
        >
          ⚙ SETTINGS
        </button>
      </div>

      {/* Main View Area */}
      <div className="relative flex-1 w-full overflow-hidden bg-[#5a2800]">
        <canvas
          ref={canvasRef}
          onMouseDown={handleMouseDown}
          onMouseMove={handleMouseMove}
          onMouseUp={handleMouseUp}
          onMouseLeave={handleMouseUp}
          onWheel={handleWheel}
          onClick={handleCanvasClick}
          className="w-full h-full cursor-grab active:cursor-grabbing"
        />


        {/* Side panel */}
        {selectedAgent && (
          <div className="absolute right-3 bottom-3 top-3 z-20 w-72 pointer-events-auto">
            <div className="pixel-border-paper bg-parchment p-4 flex flex-col h-full shadow-2xl justify-between">
              <div>
                <div className="flex justify-between items-start border-b-2 border-charcoal-brown pb-2 mb-3">
                  <div>
                    <h2 className="font-pixel text-[11px] text-charcoal-brown font-bold leading-none mb-1">
                      {selectedAgent.name}
                    </h2>
                    <span className="font-pixel text-[9px] text-grey-brown">
                      TYPE: {selectedAgent.type} (LVL {selectedAgent.level})
                    </span>
                  </div>
                  <button
                    onClick={() => setSelectedAgentId(null)}
                    className="font-pixel text-[11px] text-berry-red hover:text-charcoal-brown"
                  >
                    X
                  </button>
                </div>

                <div className="mb-3">
                  <span className="text-[16px] text-grey-brown font-bold">STATUS:</span>
                  <span className={`ml-2 text-[18px] font-bold ${
                    selectedAgent.status === 'working' ? 'text-crop-green' :
                    selectedAgent.status === 'error' ? 'text-berry-red font-blink animate-pulse' : 'text-charcoal-brown'
                  }`}>
                    {selectedAgent.status.toUpperCase()}
                  </span>
                  {selectedAgent.status === 'error' && (
                    <button
                      onClick={() => resolveError(selectedAgent.id)}
                      className="pixel-btn pixel-btn-red py-1 px-2 text-[9px] mt-2 block w-full"
                    >
                      RESOLVE PANIC
                    </button>
                  )}
                </div>

                {currentTask ? (
                  <div className="p-2 border-2 border-wood-pine bg-parchment-dark">
                    <p className="font-pixel text-[9px] text-grey-brown mb-1">RUNNING TASK:</p>
                    <p className="text-[16px] font-bold text-charcoal-brown leading-tight mb-2">
                      {currentTask.title}
                    </p>
                    <div className="w-full bg-charcoal-brown h-2.5 p-[2px] border border-wood-pine mb-1">
                      <div
                        className="bg-crop-green h-full transition-all duration-100"
                        style={{ width: `${currentTask.progress}%` }}
                      />
                    </div>
                    <span className="font-pixel text-[8px] text-charcoal-brown float-right">
                      {Math.floor(currentTask.progress)}%
                    </span>
                  </div>
                ) : (
                  <p className="text-grey-brown text-[16px] italic">
                    Currently resting. Assign a task from the Issues list.
                  </p>
                )}
              </div>

              {currentTask && (
                <div className="flex-1 flex flex-col justify-end mt-3 min-h-[120px] overflow-hidden">
                  <span className="font-pixel text-[9px] text-grey-brown border-b border-charcoal-brown mb-1">
                    REASONING LOGS
                  </span>
                  <div className="flex-1 bg-black p-2 font-mono text-[10px] text-green-500 overflow-y-auto leading-tight pr-1">
                    {currentTask.logs.slice(0, Math.floor((currentTask.progress / 100) * currentTask.logs.length) + 1).map((log, idx) => (
                      <div key={idx} className="mb-1 last:mb-0 break-words whitespace-pre-wrap">
                        {log}
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          </div>
        )}
        {showChat && <SecretaryChatDialog onClose={() => setShowChat(false)} />}
      </div>

      {/* Bottom HUD Bar */}
      <div className="flex justify-between items-center bg-[#5f3e26] border-2 border-[#e6b053] px-4 py-1.5 text-[#f6ebd3] text-[18px] mt-1.5">
        <div className="font-bold">💰 {tokens.toLocaleString()}</div>
        
        <button
          onClick={onOpenKanban}
          className="flex items-center gap-2 cursor-pointer hover:text-white border border-transparent hover:border-[#e6b053] px-3 py-1 rounded transition-all font-bold"
        >
          📋 {tasks.length} tasks
        </button>
        
        <button
          onClick={onOpenShop}
          className="flex items-center gap-2 cursor-pointer hover:text-white border border-transparent hover:border-[#e6b053] px-3 py-1 rounded transition-all font-bold"
        >
          👥 {agents.length} active
        </button>
      </div>
    </div>
  )
}
