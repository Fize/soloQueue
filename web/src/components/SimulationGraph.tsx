import { useEffect, useRef } from 'react'
import * as d3 from 'd3-force'
import { drag } from 'd3-drag'
import { select } from 'd3-selection'
import type { SimulationPersona } from '@/types'

interface GraphNode extends d3.SimulationNodeDatum {
  id: string
  name: string
  role: string
  color: string
}

interface GraphLink extends d3.SimulationLinkDatum<GraphNode> {
  type: string
  weight: number
}

export interface GraphEdgeInput {
  source: string
  target: string
  type: string
  weight: number
}

interface SimulationGraphProps {
  personas: SimulationPersona[]
  edges: GraphEdgeInput[]
  onSelectAgent?: (agentId: string) => void
  selectedAgentId?: string | null
}

const ROLE_COLORS: Record<string, string> = {
  moderator: '#f54e00',
  mediator: '#f54e00',
  host: '#f54e00',
  pro: '#16a34a',
  con: '#dc2626',
  neutral: '#2563eb',
}

export function SimulationGraph({
  personas,
  edges,
  onSelectAgent,
  selectedAgentId,
}: SimulationGraphProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null)
  const containerRef = useRef<HTMLDivElement | null>(null)
  const simRef = useRef<d3.Simulation<GraphNode, GraphLink> | null>(null)
  const linksRef = useRef<GraphLink[]>([])
  const nodesRef = useRef<GraphNode[]>([])
  const selectRef = useRef(onSelectAgent)
  const selectedIdRef = useRef(selectedAgentId)
  const ctxRef = useRef<CanvasRenderingContext2D | null>(null)
  const themeRef = useRef({
    primaryColor: '#f54e00',
    cardColor: '#fff',
    fgColor: '#000',
    mutedFgColor: '#666',
    isDark: false,
  })

  useEffect(() => {
    selectRef.current = onSelectAgent
    selectedIdRef.current = selectedAgentId
  }, [onSelectAgent, selectedAgentId])

  // Build or rebuild nodes when personas change
  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return
    const container = containerRef.current
    const width = container ? container.clientWidth : 600
    const height = container ? container.clientHeight : 400

    canvas.width = width * window.devicePixelRatio
    canvas.height = height * window.devicePixelRatio
    canvas.style.width = `${width}px`
    canvas.style.height = `${height}px`

    const ctx = canvas.getContext('2d')
    if (!ctx) return
    ctx.scale(window.devicePixelRatio, window.devicePixelRatio)
    ctxRef.current = ctx

    const computedStyle = getComputedStyle(document.documentElement)
    const isDark = document.documentElement.classList.contains('dark')
    themeRef.current = {
      primaryColor: computedStyle.getPropertyValue('--primary').trim() || '#f54e00',
      cardColor:
        computedStyle.getPropertyValue('--card').trim() || (isDark ? '#26251e' : '#ffffff'),
      fgColor:
        computedStyle.getPropertyValue('--foreground').trim() || (isDark ? '#f2f1ed' : '#26251e'),
      mutedFgColor:
        computedStyle.getPropertyValue('--muted-foreground').trim() ||
        (isDark ? '#c7c1b6' : '#75756d'),
      isDark,
    }

    nodesRef.current = personas.map((p, i) => {
      const lowerRole = p.role.toLowerCase()
      let color: string
      if (
        lowerRole.includes('moderator') ||
        lowerRole.includes('mediator') ||
        lowerRole.includes('host')
      ) {
        color = ROLE_COLORS.moderator
      } else if (lowerRole.includes('pro') || lowerRole.includes('agree')) {
        color = ROLE_COLORS.pro
      } else if (
        lowerRole.includes('con') ||
        lowerRole.includes('rebut') ||
        lowerRole.includes('disagree')
      ) {
        color = ROLE_COLORS.con
      } else {
        const colors = [ROLE_COLORS.neutral, '#8b5cf6', '#ec4899', '#f59e0b', '#06b6d4']
        color = colors[i % colors.length]
      }
      return { id: p.id, name: p.name, role: p.role, color }
    })

    linksRef.current = []

    if (simRef.current) {
      simRef.current.stop()
    }

    const simulation = d3
      .forceSimulation<GraphNode>(nodesRef.current)
      .force(
        'link',
        d3
          .forceLink<GraphNode, GraphLink>(linksRef.current)
          .id((d) => d.id)
          .distance(120)
      )
      .force('charge', d3.forceManyBody().strength(-300))
      .force('center', d3.forceCenter(width / 2, height / 2))
      .force('collision', d3.forceCollide().radius(45))

    const dragBehavior = drag<HTMLCanvasElement, unknown>()
      .subject((event) => {
        const x = event.x
        const y = event.y
        return nodesRef.current.find((node) => {
          const dx = node.x! - x
          const dy = node.y! - y
          return dx * dx + dy * dy < 900
        })
      })
      .on('start', (event) => {
        if (!event.active) simulation.alphaTarget(0.3).restart()
        event.subject.fx = event.subject.x
        event.subject.fy = event.subject.y
      })
      .on('drag', (event) => {
        event.subject.fx = event.x
        event.subject.fy = event.y
      })
      .on('end', (event) => {
        if (!event.active) simulation.alphaTarget(0)
        event.subject.fx = null
        event.subject.fy = null
        const dx = event.x - event.subject.x
        const dy = event.y - event.subject.y
        if (dx * dx + dy * dy < 25) {
          selectRef.current?.(event.subject.id)
        }
      })

    select(canvas).call(dragBehavior as any)

    simulation.on('tick', () => {
      const c = ctxRef.current
      if (!c) return
      const w = canvas.width / window.devicePixelRatio
      const h = canvas.height / window.devicePixelRatio
      const t = themeRef.current

      c.clearRect(0, 0, w, h)

      // Grid
      c.strokeStyle = t.isDark ? 'rgba(255,255,255,0.02)' : 'rgba(0,0,0,0.02)'
      c.lineWidth = 1
      for (let x = 0; x < w; x += 40) {
        c.beginPath()
        c.moveTo(x, 0)
        c.lineTo(x, h)
        c.stroke()
      }
      for (let y = 0; y < h; y += 40) {
        c.beginPath()
        c.moveTo(0, y)
        c.lineTo(w, y)
        c.stroke()
      }

      // Links
      linksRef.current.forEach((link) => {
        const source = link.source as GraphNode
        const target = link.target as GraphNode
        c.beginPath()
        c.moveTo(source.x!, source.y!)
        c.lineTo(target.x!, target.y!)
        const type = link.type.toLowerCase()
        if (type.includes('agree') || type.includes('support')) {
          c.strokeStyle = 'rgba(22,163,74,0.4)'
        } else if (type.includes('rebut') || type.includes('disagree') || type.includes('oppose')) {
          c.strokeStyle = 'rgba(220,38,38,0.4)'
        } else {
          c.strokeStyle = t.primaryColor + '44'
        }
        c.lineWidth = Math.min(2 + link.weight, 6)
        c.stroke()
        const dotT = (Date.now() / 1500) % 1
        const dotX = source.x! + (target.x! - source.x!) * dotT
        const dotY = source.y! + (target.y! - source.y!) * dotT
        c.beginPath()
        c.arc(dotX, dotY, 2.5, 0, 2 * Math.PI)
        c.fillStyle = c.strokeStyle
        c.fill()
      })

      // Nodes
      nodesRef.current.forEach((node) => {
        const isSelected = selectedIdRef.current === node.id
        if (isSelected) {
          c.beginPath()
          c.arc(node.x!, node.y!, 30, 0, 2 * Math.PI)
          c.fillStyle = t.primaryColor + '22'
          c.strokeStyle = t.primaryColor + '99'
          c.lineWidth = 2
          c.fill()
          c.stroke()
        }
        c.beginPath()
        c.arc(node.x!, node.y!, 22, 0, 2 * Math.PI)
        c.fillStyle = t.cardColor
        c.strokeStyle = node.color
        c.lineWidth = isSelected ? 3 : 2
        c.fill()
        c.stroke()
        c.beginPath()
        c.arc(node.x!, node.y!, 6, 0, 2 * Math.PI)
        c.fillStyle = node.color
        c.fill()
        c.font = 'bold 12px ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace'
        c.fillStyle = t.fgColor
        c.textAlign = 'center'
        c.textBaseline = 'top'
        c.fillText(node.name, node.x!, node.y! + 28)
        c.font = '10px sans-serif'
        c.fillStyle = t.mutedFgColor
        c.fillText(
          node.role.length > 18 ? node.role.slice(0, 15) + '...' : node.role,
          node.x!,
          node.y! + 43
        )
      })
    })

    simRef.current = simulation

    return () => {
      simulation.stop()
      simRef.current = null
    }
  }, [personas])

  // Incrementally update links when edges change (without rebuilding simulation)
  useEffect(() => {
    if (!simRef.current) return
    const nodes = nodesRef.current
    if (nodes.length === 0) return

    const existing = linksRef.current
    const newLinks: GraphLink[] = []

    for (const e of edges) {
      const src = nodes.find((n) => n.id === e.source || n.name === e.source)
      const tgt = nodes.find((n) => n.id === e.target || n.name === e.target)
      if (!src || !tgt) continue

      const existingLink = existing.find(
        (l) => (l.source as GraphNode).id === src.id && (l.target as GraphNode).id === tgt.id
      )
      if (existingLink) {
        existingLink.weight = e.weight
        existingLink.type = e.type
      } else {
        newLinks.push({ source: src, target: tgt, type: e.type, weight: e.weight })
      }
    }

    if (newLinks.length > 0) {
      existing.push(...newLinks)
    }

    const linkForce = simRef.current.force('link') as d3.ForceLink<GraphNode, GraphLink> | undefined
    if (linkForce) {
      linkForce.links(existing)
    }

    if (newLinks.length > 0 || existing.length > 0) {
      simRef.current.alpha(0.3).restart()
    }
  }, [edges])

  return (
    <div
      ref={containerRef}
      className="relative w-full h-full min-h-[350px] overflow-hidden bg-card/40 rounded-xl border border-border"
    >
      <canvas ref={canvasRef} className="absolute inset-0 cursor-grab active:cursor-grabbing" />
      <div className="absolute top-3 left-3 bg-card/90 backdrop-blur-md px-3 py-1.5 rounded-lg border border-border text-[10px] text-muted-foreground font-mono flex gap-4 shadow-sm">
        <div className="flex items-center gap-1.5">
          <span className="w-2.5 h-2.5 rounded-full bg-primary block" /> Host/Moderator
        </div>
        <div className="flex items-center gap-1.5">
          <span className="w-2.5 h-2.5 rounded-full bg-success block" /> Pro Stance
        </div>
        <div className="flex items-center gap-1.5">
          <span className="w-2.5 h-2.5 rounded-full bg-error block" /> Con Stance
        </div>
      </div>
      <div className="absolute bottom-3 right-3 text-[10px] text-muted-foreground/80 font-mono select-none">
        Drag nodes to interact • Click to select agent
      </div>
    </div>
  )
}
