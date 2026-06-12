import { useEffect, useRef } from 'react'
import * as d3 from 'd3-force'
import { drag } from 'd3-drag'
import { select } from 'd3-selection'
import type { SimulationPersona, SimulationRelationEdge } from '@/types'

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

interface SimulationGraphProps {
  personas: SimulationPersona[]
  edges: SimulationRelationEdge[]
  onSelectAgent?: (agentId: string) => void
  selectedAgentId?: string | null
}

const ROLE_COLORS: Record<string, string> = {
  moderator: '#f54e00', // brand orange
  mediator: '#f54e00',
  host: '#f54e00',
  pro: '#16a34a',       // success green
  con: '#dc2626',       // error/destructive red
  neutral: '#2563eb',   // info blue
}

export function SimulationGraph({
  personas,
  edges,
  onSelectAgent,
  selectedAgentId,
}: SimulationGraphProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null)
  const containerRef = useRef<HTMLDivElement | null>(null)

  // Cache latest values for render cycle
  const selectRef = useRef(onSelectAgent)
  const selectedIdRef = useRef(selectedAgentId)
  useEffect(() => {
    selectRef.current = onSelectAgent
    selectedIdRef.current = selectedAgentId
  }, [onSelectAgent, selectedAgentId])

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

    // Retrieve theme colors dynamically from index.css tokens
    const computedStyle = getComputedStyle(document.documentElement)
    const isDark = document.documentElement.classList.contains('dark')

    const primaryColor = computedStyle.getPropertyValue('--primary').trim() || '#f54e00'
    const cardColor = computedStyle.getPropertyValue('--card').trim() || (isDark ? '#26251e' : '#ffffff')
    const fgColor = computedStyle.getPropertyValue('--foreground').trim() || (isDark ? '#f2f1ed' : '#26251e')
    const mutedFgColor = computedStyle.getPropertyValue('--muted-foreground').trim() || (isDark ? '#c7c1b6' : '#75756d')

    // Build unique nodes list
    const nodes: GraphNode[] = personas.map((p, i) => {
      const lowerRole = p.role.toLowerCase()
      let color: string
      if (lowerRole.includes('moderator') || lowerRole.includes('mediator') || lowerRole.includes('host')) {
        color = ROLE_COLORS.moderator
      } else if (lowerRole.includes('pro') || lowerRole.includes('agree')) {
        color = ROLE_COLORS.pro
      } else if (lowerRole.includes('con') || lowerRole.includes('rebut') || lowerRole.includes('disagree')) {
        color = ROLE_COLORS.con
      } else {
        const colors = [ROLE_COLORS.neutral, '#8b5cf6', '#ec4899', '#f59e0b', '#06b6d4']
        color = colors[i % colors.length]
      }

      return {
        id: p.id,
        name: p.name,
        role: p.role,
        color,
      }
    })

    // Filter and map edges to d3-force format
    const links: GraphLink[] = edges
      .map((e) => {
        const sourceNode = nodes.find((n) => n.id === e.source || n.name === e.source)
        const targetNode = nodes.find((n) => n.id === e.target || n.name === e.target)
        if (!sourceNode || !targetNode) return null
        return {
          source: sourceNode,
          target: targetNode,
          type: e.type,
          weight: e.weight,
        }
      })
      .filter((l): l is NonNullable<typeof l> => l !== null) as GraphLink[]

    // Set up D3 simulation
    const simulation = d3
      .forceSimulation<GraphNode>(nodes)
      .force(
        'link',
        d3
          .forceLink<GraphNode, GraphLink>(links)
          .id((d) => d.id)
          .distance(120)
      )
      .force('charge', d3.forceManyBody().strength(-300))
      .force('center', d3.forceCenter(width / 2, height / 2))
      .force('collision', d3.forceCollide().radius(45))

    // Drag interaction setup
    const dragBehavior = drag<HTMLCanvasElement, unknown>()
      .subject((event) => {
        const x = event.x
        const y = event.y
        return nodes.find((node) => {
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

    // Tick handler for redrawing
    simulation.on('tick', () => {
      ctx.clearRect(0, 0, width, height)

      // 1. Draw theme-aware grid background
      ctx.strokeStyle = isDark ? 'rgba(255, 255, 255, 0.02)' : 'rgba(0, 0, 0, 0.02)'
      ctx.lineWidth = 1
      const gridSize = 40
      for (let x = 0; x < width; x += gridSize) {
        ctx.beginPath()
        ctx.moveTo(x, 0)
        ctx.lineTo(x, height)
        ctx.stroke()
      }
      for (let y = 0; y < height; y += gridSize) {
        ctx.beginPath()
        ctx.moveTo(0, y)
        ctx.lineTo(width, y)
        ctx.stroke()
      }

      // 2. Draw Links (Relations)
      links.forEach((link) => {
        const source = link.source as GraphNode
        const target = link.target as GraphNode

        ctx.beginPath()
        ctx.moveTo(source.x!, source.y!)
        ctx.lineTo(target.x!, target.y!)

        const type = link.type.toLowerCase()
        if (type.includes('agree') || type.includes('support')) {
          ctx.strokeStyle = 'rgba(22, 163, 74, 0.4)' // Success green
        } else if (type.includes('rebut') || type.includes('disagree') || type.includes('oppose')) {
          ctx.strokeStyle = 'rgba(220, 38, 38, 0.4)' // Destructive red
        } else {
          ctx.strokeStyle = primaryColor + '44' // Brand orange accent with opacity
        }

        ctx.lineWidth = Math.min(2 + link.weight, 6)
        ctx.stroke()

        // Draw flowing dots
        const time = (Date.now() / 1500) % 1
        const dotX = source.x! + (target.x! - source.x!) * time
        const dotY = source.y! + (target.y! - source.y!) * time
        ctx.beginPath()
        ctx.arc(dotX, dotY, 2.5, 0, 2 * Math.PI)
        ctx.fillStyle = ctx.strokeStyle
        ctx.fill()
      })

      // 3. Draw Nodes (Agents)
      nodes.forEach((node) => {
        const isSelected = selectedIdRef.current === node.id

        // Glow ring around selected node
        if (isSelected) {
          ctx.beginPath()
          ctx.arc(node.x!, node.y!, 30, 0, 2 * Math.PI)
          ctx.fillStyle = primaryColor + '22'
          ctx.strokeStyle = primaryColor + '99'
          ctx.lineWidth = 2
          ctx.fill()
          ctx.stroke()
        }

        // Inner node body matching current card background
        ctx.beginPath()
        ctx.arc(node.x!, node.y!, 22, 0, 2 * Math.PI)
        ctx.fillStyle = cardColor
        ctx.strokeStyle = node.color
        ctx.lineWidth = isSelected ? 3 : 2
        ctx.fill()
        ctx.stroke()

        // Center dot
        ctx.beginPath()
        ctx.arc(node.x!, node.y!, 6, 0, 2 * Math.PI)
        ctx.fillStyle = node.color
        ctx.fill()

        // Agent Name
        ctx.font = 'bold 12px ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace'
        ctx.fillStyle = fgColor
        ctx.textAlign = 'center'
        ctx.textBaseline = 'top'
        ctx.fillText(node.name, node.x!, node.y! + 28)

        // Agent Role Subtitle
        ctx.font = '10px sans-serif'
        ctx.fillStyle = mutedFgColor
        const displayRole = node.role.length > 18 ? `${node.role.slice(0, 15)}...` : node.role
        ctx.fillText(displayRole, node.x!, node.y! + 43)
      })
    })

    return () => {
      simulation.stop()
    }
  }, [personas, edges])

  return (
    <div ref={containerRef} className="relative w-full h-full min-h-[350px] overflow-hidden bg-card/40 rounded-xl border border-border">
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
