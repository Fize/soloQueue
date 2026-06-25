import { useCallback, useEffect, useRef, useState } from 'react'
import buildingBg from '../assets/backgrounds/building_exterior.png'

interface TitleSceneProps {
  onStart: () => void
  backendStatus?: 'ready' | 'loading' | 'error'
  backendError?: string
  onRetryBackend?: () => void
}

/* ── Rain splash ────────────────────────────────────── */
interface Splash {
  x: number
  y: number
  frame: number       // 0, 1, 2 → three expansion frames
  timer: number       // seconds remaining in current frame
}

/* ── Rain particle ──────────────────────────────────── */
interface RainDrop {
  x: number
  y: number
  vy: number          // px/s
  length: number      // px
}

/* ── Cloud ──────────────────────────────────────────── */
interface Cloud {
  x: number
  y: number
  speed: number       // px/s
  w: number           // overall width
  blobs: { dx: number; dy: number; r: number }[]
}

/* ── Window glow rect ───────────────────────────────── */
interface GlowRect {
  /** Fractions of the building draw rect (0-1) */
  rx: number
  ry: number
  rw: number
  rh: number
  /** Phase offset for sine oscillation */
  phase: number
}

/* ── Helpers ────────────────────────────────────────── */
const rand = (lo: number, hi: number) => Math.random() * (hi - lo) + lo

function makeCloud(canvasW: number): Cloud {
  const w = rand(100, 180)
  const blobCount = Math.floor(rand(3, 6))
  const blobs: Cloud['blobs'] = []
  for (let i = 0; i < blobCount; i++) {
    blobs.push({
      dx: rand(-w * 0.35, w * 0.35),
      dy: rand(-w * 0.12, w * 0.08),
      r: rand(w * 0.18, w * 0.32),
    })
  }
  return {
    x: rand(-200, canvasW + 100),
    y: rand(40, 160),
    speed: rand(3, 7),
    w,
    blobs,
  }
}

function makeDrop(w: number, startAtTop: boolean): RainDrop {
  return {
    x: rand(0, w),
    y: startAtTop ? rand(-30, 0) : rand(-30, w * 0.8),
    vy: rand(300, 600),
    length: rand(10, 25),
  }
}

const RAIN_COUNT = 50
const CLOUD_COUNT = 5
const SPLASH_FRAME_DURATION = 0.06 // seconds per splash frame

/* ── Window glow positions (relative to building rect) */
const GLOW_WINDOWS: GlowRect[] = [
  { rx: 0.15, ry: 0.22, rw: 0.10, rh: 0.08, phase: 0 },
  { rx: 0.30, ry: 0.22, rw: 0.10, rh: 0.08, phase: 0.8 },
  { rx: 0.58, ry: 0.22, rw: 0.10, rh: 0.08, phase: 1.6 },
  { rx: 0.75, ry: 0.22, rw: 0.10, rh: 0.08, phase: 2.4 },
  { rx: 0.15, ry: 0.42, rw: 0.10, rh: 0.08, phase: 3.2 },
  { rx: 0.30, ry: 0.42, rw: 0.10, rh: 0.08, phase: 4.0 },
  { rx: 0.58, ry: 0.42, rw: 0.10, rh: 0.08, phase: 4.8 },
  { rx: 0.75, ry: 0.42, rw: 0.10, rh: 0.08, phase: 5.6 },
]

export default function TitleScene({ onStart, backendStatus = 'ready', backendError, onRetryBackend }: TitleSceneProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null)
  const [imageLoaded, setImageLoaded] = useState(false)
  const imgRef = useRef<HTMLImageElement | null>(null)

  /* Preload the building image once */
  useEffect(() => {
    const img = new Image()
    img.src = buildingBg
    img.onload = () => {
      imgRef.current = img
      setImageLoaded(true)
    }
  }, [])

  /* ── Main animation loop ─────────────────────────── */
  const startLoop = useCallback(() => {
    const canvas = canvasRef.current
    if (!canvas) return
    const ctx = canvas.getContext('2d')
    if (!ctx) return

    /* ---------- DPR-aware resize ---------- */
    const resize = () => {
      const dpr = window.devicePixelRatio || 1
      canvas.width = window.innerWidth * dpr
      canvas.height = window.innerHeight * dpr
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
    }
    window.addEventListener('resize', resize)
    resize()

    /* ---------- State ---------- */
    const logicalW = () => window.innerWidth
    const logicalH = () => window.innerHeight

    const clouds: Cloud[] = Array.from({ length: CLOUD_COUNT }, () =>
      makeCloud(logicalW()),
    )
    const rain: RainDrop[] = Array.from({ length: RAIN_COUNT }, () =>
      makeDrop(logicalW(), false),
    )
    const splashes: Splash[] = []

    let lastTs = performance.now()
    let elapsed = 0 // total seconds for glow oscillation
    let animId = 0

    /* ---------- Frame ---------- */
    const frame = (ts: number) => {
      const dt = Math.min((ts - lastTs) / 1000, 0.05) // cap to avoid jumps
      lastTs = ts
      elapsed += dt

      const w = logicalW()
      const h = logicalH()

      /* ── Layer 0: Background (Building Image or Sky Gradient fallback) ── */
      const img = imgRef.current
      let bgDrawn = false
      let drawW = w
      let drawH = h
      let dx = 0
      let dy = 0

      if (img && img.complete && img.naturalWidth > 0) {
        const imgW = img.naturalWidth
        const imgH = img.naturalHeight
        const scale = Math.max(w / imgW, h / imgH)
        drawW = imgW * scale
        drawH = imgH * scale
        dx = (w - drawW) / 2
        dy = (h - drawH) / 2

        ctx.imageSmoothingEnabled = false
        ctx.drawImage(img, dx, dy, drawW, drawH)
        bgDrawn = true
      }

      if (!bgDrawn) {
        // Fallback sky gradient
        const sky = ctx.createLinearGradient(0, 0, 0, h)
        sky.addColorStop(0, '#87CEEB')
        sky.addColorStop(0.5, '#b0d0f0')
        sky.addColorStop(1, '#f6ebd3')
        ctx.fillStyle = sky
        ctx.fillRect(0, 0, w, h)
      }

      /* ── Layer 1: Clouds ── */
      ctx.fillStyle = 'rgba(255, 255, 255, 0.85)'
      for (const c of clouds) {
        c.x += c.speed * dt
        if (c.x - c.w * 0.5 > w + 50) {
          c.x = -c.w - rand(20, 120)
          c.y = rand(40, 160)
        }
        ctx.beginPath()
        for (const b of c.blobs) {
          ctx.moveTo(c.x + b.dx + b.r, c.y + b.dy)
          ctx.arc(c.x + b.dx, c.y + b.dy, b.r, 0, Math.PI * 2)
        }
        ctx.fill()
      }

      /* ── Layer 2: Rain ── */
      ctx.strokeStyle = 'rgba(124, 168, 76, 0.35)'
      ctx.lineWidth = 1.5
      for (let i = 0; i < rain.length; i++) {
        const d = rain[i]
        d.y += d.vy * dt
        if (d.y > h) {
          // Spawn splash
          splashes.push({ x: d.x, y: h - 2, frame: 0, timer: SPLASH_FRAME_DURATION })
          rain[i] = makeDrop(w, true)
        }
        ctx.beginPath()
        ctx.moveTo(d.x, d.y)
        ctx.lineTo(d.x, d.y + d.length)
        ctx.stroke()
      }

      /* Splash circles */
      for (let i = splashes.length - 1; i >= 0; i--) {
        const s = splashes[i]
        const radius = (s.frame + 1) * 3
        const alpha = 0.5 - s.frame * 0.15
        ctx.beginPath()
        ctx.arc(s.x, s.y, radius, 0, Math.PI * 2)
        ctx.strokeStyle = `rgba(124, 168, 76, ${Math.max(alpha, 0.05)})`
        ctx.lineWidth = 1
        ctx.stroke()

        s.timer -= dt
        if (s.timer <= 0) {
          s.frame++
          s.timer = SPLASH_FRAME_DURATION
        }
        if (s.frame > 2) {
          splashes.splice(i, 1)
        }
      }

      /* ── Layer 4: Window glow ── */
      if (bgDrawn) {
        for (const gw of GLOW_WINDOWS) {
          const opacity = 0.2 + 0.2 * (1 + Math.sin(elapsed * 2.0 + gw.phase))
          ctx.fillStyle = `rgba(243, 183, 43, ${opacity.toFixed(3)})`
          ctx.fillRect(
            dx + gw.rx * drawW,
            dy + gw.ry * drawH,
            gw.rw * drawW,
            gw.rh * drawH,
          )
        }
      }

      animId = requestAnimationFrame(frame)
    }

    animId = requestAnimationFrame(frame)

    return () => {
      window.removeEventListener('resize', resize)
      cancelAnimationFrame(animId)
    }
  }, [])

  /* Start loop immediately on mount */
  useEffect(() => {
    const cleanup = startLoop()
    return cleanup
  }, [startLoop])

  return (
    <div className="relative w-screen h-screen overflow-hidden select-none">
      {/* Layer 0-4: canvas */}
      <canvas
        ref={canvasRef}
        className="absolute inset-0 z-0 w-screen h-screen"
      />

      {/* Layer 5: UI Overlay */}
      <div className="absolute inset-0 z-10 flex flex-col items-center justify-end pb-[6%] pointer-events-none">
        {/* Spacer so content sits in the lower third */}
        <div className="flex-1" />

        {/* Title */}
        <h1
          className="font-pixel text-[28px] leading-none mb-2 pointer-events-auto"
          style={{
            color: '#f3b72b',
            textShadow: '3px 3px 0 #381a04, -1px -1px 0 #381a04',
          }}
        >
          SOLOQUEUE INC.
        </h1>

        {/* Subtitle */}
        <p className="font-pixel text-[12px] mb-8" style={{ color: '#8c7662' }}>
          Est. 2026
        </p>

        {/* Buttons */}
        <div className="flex flex-col gap-4 w-52 pointer-events-auto">
          <button
            onClick={onStart}
            className="pixel-btn pixel-btn-green w-full py-3 text-[14px]"
          >
            START WORK
          </button>
        </div>

        {/* Backend status */}
        {backendStatus !== undefined && (
          <div className="mt-4 flex items-center gap-2 pointer-events-auto">
            {backendStatus === 'loading' && (
              <>
                <div className="w-2.5 h-2.5 rounded-full bg-[#e28a2b] animate-pulse" />
                <span className="font-pixel text-[9px] text-[#8c7662]">Starting backend...</span>
              </>
            )}
            {backendStatus === 'error' && (
              <>
                <div className="w-2.5 h-2.5 rounded-full bg-[#d83838]" />
                <span className="font-pixel text-[8px] text-[#d83838] max-w-[200px]">
                  {backendError || 'Backend error'}
                </span>
                {onRetryBackend && (
                  <button onClick={onRetryBackend} className="font-pixel text-[9px] text-[#c0942c] underline ml-1">
                    RETRY
                  </button>
                )}
              </>
            )}
            {backendStatus === 'ready' && (
              <>
                <div className="w-2.5 h-2.5 rounded-full bg-[#4eb036]" />
                <span className="font-pixel text-[9px] text-[#4eb036]">Backend ready</span>
              </>
            )}
          </div>
        )}
      </div>

      {/* Version tag — bottom-left */}
      <div className="absolute bottom-3 left-4 z-10 font-pixel text-[10px] text-grey-brown">
        v0.1.0-mvp{!imageLoaded ? ' (loading...)' : ''}
      </div>
    </div>
  )
}
