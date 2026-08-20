import { useEffect, useRef, useCallback } from 'react'
import * as d3Force from 'd3-force'
import * as d3Selection from 'd3-selection'
import * as d3Zoom from 'd3-zoom'
import type { GraphNode, GraphEdge, NodeRunState } from '@/types'
import { cn } from '@/lib/utils'

// ─── Types ──────────────────────────────────────────────────────────────

interface DAGPreviewProps {
  nodes: GraphNode[]
  edges: GraphEdge[]
  entryNodes?: string[]
  nodeStates?: Record<string, NodeRunState> // optional state overlay for run views
  className?: string
  interactive?: boolean
  onNodeClick?: (nodeId: string) => void
  onEdgeClick?: (edgeId: string) => void
}

export function fitGraphToViewport(
  points: Array<{ id: string; x: number; y: number }>,
  width: number,
  height: number,
  padding = 24,
): { points: Map<string, { x: number; y: number }>; scale: number } {
  if (points.length === 0) return { points: new Map(), scale: 1 }

  const minX = Math.min(...points.map(point => point.x))
  const maxX = Math.max(...points.map(point => point.x))
  const minY = Math.min(...points.map(point => point.y))
  const maxY = Math.max(...points.map(point => point.y))
  const graphWidth = Math.max(1, maxX - minX)
  const graphHeight = Math.max(1, maxY - minY)
  const scale = Math.min(
    1,
    Math.max(1, width - padding * 2) / graphWidth,
    Math.max(1, height - padding * 2) / graphHeight,
  )

  return {
    scale,
    points: new Map(points.map(point => [point.id, {
      x: padding + (point.x - minX) * scale,
      y: padding + (point.y - minY) * scale,
    }])),
  }
}

// ─── Node state → fill color ─────────────────────────────────────────────

const stateFillMap: Record<string, string> = {
  queued: 'var(--color-card)',
  running: 'color-mix(in srgb, var(--color-signal) 15%, var(--color-card))',
  succeeded: 'color-mix(in srgb, var(--color-success) 10%, var(--color-card))',
  failed: 'color-mix(in srgb, #ef4444 10%, var(--color-card))',
  cancelled: 'color-mix(in srgb, var(--color-warning) 10%, var(--color-card))',
  timed_out: 'color-mix(in srgb, #fb923c 10%, var(--color-card))',
}

const stateStrokeMap: Record<string, string> = {
  queued: 'var(--color-border)',
  running: 'var(--color-signal)',
  succeeded: 'var(--color-success)',
  failed: '#ef4444',
  cancelled: 'var(--color-warning)',
  timed_out: '#fb923c',
}

// ─── DAGPreview Component ───────────────────────────────────────────────

export function DAGPreview({
  nodes,
  edges,
  entryNodes = [],
  nodeStates,
  className,
  interactive = false,
  onNodeClick,
  onEdgeClick,
}: DAGPreviewProps) {
  const ref = useRef<HTMLDivElement>(null)
  const svgRef = useRef<SVGSVGElement | null>(null)

  const renderGraph = useCallback(() => {
    const el = ref.current
    if (!el) return

    const rect = el.getBoundingClientRect()
    const width = rect.width || 600
    const height = rect.height || 400

    // Clear previous content
    el.innerHTML = ''

    // Create SVG
    const svg = d3Selection.select(el)
      .append('svg')
      .attr('width', width)
      .attr('height', height)
      .attr('viewBox', `0 0 ${width} ${height}`)

    svgRef.current = svg.node()

    // Defs for arrow markers
    const defs = svg.append('defs')

    // Default arrow
    defs.append('marker')
      .attr('id', 'arrow-default')
      .attr('viewBox', '0 -5 10 10')
      .attr('refX', 24)
      .attr('refY', 0)
      .attr('markerWidth', 8)
      .attr('markerHeight', 8)
      .attr('orient', 'auto')
      .append('path')
      .attr('d', 'M0,-5L10,0L0,5')
      .attr('fill', 'var(--color-chart-1)')

    // Loop arrow
    defs.append('marker')
      .attr('id', 'arrow-loop')
      .attr('viewBox', '0 -5 10 10')
      .attr('refX', 24)
      .attr('refY', 0)
      .attr('markerWidth', 8)
      .attr('markerHeight', 8)
      .attr('orient', 'auto')
      .append('path')
      .attr('d', 'M0,-5L10,0L0,5')
      .attr('fill', 'var(--color-chart-2)')

    // Active arrow (signal)
    defs.append('marker')
      .attr('id', 'arrow-signal')
      .attr('viewBox', '0 -5 10 10')
      .attr('refX', 24)
      .attr('refY', 0)
      .attr('markerWidth', 8)
      .attr('markerHeight', 8)
      .attr('orient', 'auto')
      .append('path')
      .attr('d', 'M0,-5L10,0L0,5')
      .attr('fill', 'var(--color-signal)')

    // Container for zoom
    const g = svg.append('g')

    // Zoom behavior
    if (interactive) {
      const zoom = d3Zoom.zoom<SVGSVGElement, unknown>()
        .scaleExtent([0.3, 3])
        .on('zoom', (event) => {
          g.attr('transform', event.transform)
        })
      svg.call(zoom)
    }

    // Run force simulation to compute layout
    const simNodes = nodes.map(n => ({ ...n, x: n.position.x, y: n.position.y }))
    const simLinks = edges.map(e => ({ ...e }))

    const simulation = d3Force.forceSimulation(simNodes)
      .force('link', d3Force.forceLink(simLinks).id((d: any) => d.id).distance(150))
      .force('charge', d3Force.forceManyBody().strength(-300))
      .force('center', d3Force.forceCenter(width / 2, height / 2))
      .force('collision', d3Force.forceCollide().radius(80))
      .stop()

    // Run simulation synchronously
    for (let i = 0; i < 100; i++) simulation.tick()

    const fitted = fitGraphToViewport(
      simNodes.map(node => ({ id: node.id, x: (node as any).x, y: (node as any).y })),
      width,
      height,
    )
    const renderNodes = simNodes.map(node => {
      const point = fitted.points.get(node.id)
      return { ...node, x: point?.x || 0, y: point?.y || 0 }
    })

    // Draw edges
    const edgeGroup = g.append('g').attr('class', 'edges')

    for (const edge of edges) {
      const sourceNode = renderNodes.find(n => n.id === edge.source)
      const targetNode = renderNodes.find(n => n.id === edge.target)
      if (!sourceNode || !targetNode) continue

      const isLoop = edge.source === edge.target
      const edgeClass = isLoop ? 'arrow-loop' : 'arrow-default'

      let pathD: string
      if (isLoop) {
        // Curved self-loop
        const cx = (sourceNode as any).x
        const cy = (sourceNode as any).y
        pathD = `M ${cx + 70} ${cy - 40} C ${cx + 130} ${cy - 90}, ${cx + 130} ${cy + 50}, ${cx + 70} ${cy + 10}`
      } else {
        const sx = (sourceNode as any).x
        const sy = (sourceNode as any).y
        const tx = (targetNode as any).x
        const ty = (targetNode as any).y
        pathD = `M ${sx} ${sy} L ${tx} ${ty}`
      }

      edgeGroup.append('path')
        .attr('d', pathD)
        .attr('stroke', isLoop ? 'var(--color-chart-2)' : 'var(--color-chart-1)')
        .attr('stroke-width', isLoop ? 1.5 : 1.5)
        .attr('stroke-dasharray', isLoop ? '4,4' : 'none')
        .attr('fill', 'none')
        .attr('marker-end', `url(#${edgeClass})`)
        .attr('class', cn('cursor-pointer', interactive && 'hover:stroke-[2.5]'))
        .style('pointer-events', interactive ? 'auto' : 'none')
        .on('click', () => {
          if (interactive && onEdgeClick) onEdgeClick(edge.id)
        })

      // Edge label
      const mx = isLoop
        ? (sourceNode as any).x + 110
        : ((sourceNode as any).x + (targetNode as any).x) / 2
      const my = isLoop
        ? (sourceNode as any).y - 50
        : ((sourceNode as any).y + (targetNode as any).y) / 2 - 8

      if (edge.outcome && !isLoop) {
        edgeGroup.append('text')
          .attr('x', mx)
          .attr('y', my)
          .attr('text-anchor', 'middle')
          .attr('fill', 'var(--color-muted-foreground)')
          .attr('font-size', String(9 * fitted.scale))
          .attr('font-family', 'var(--font-mono)')
          .text(edge.outcome)
      }
    }

    // Draw nodes
    const nodeGroup = g.append('g').attr('class', 'nodes')

    for (const simNode of renderNodes) {
      const node = simNode as any
      const nodeId = node.id
      const state = nodeStates?.[nodeId]
      const isEntry = entryNodes.includes(nodeId)

      const fill = state ? stateFillMap[state] || 'var(--color-card)' : 'var(--color-card)'
      const stroke = state
        ? stateStrokeMap[state] || 'var(--color-border)'
        : isEntry
          ? 'var(--color-accent)'
          : 'var(--color-border)'

      const pulseClass = state === 'running' ? 'animate-pulse' : ''

      // Node rect
      const nodeEl = nodeGroup.append('g')
        .attr('transform', `translate(${node.x - 80 * fitted.scale}, ${node.y - 28 * fitted.scale}) scale(${fitted.scale})`)
        .attr('class', cn('cursor-pointer', interactive && 'hover:opacity-80'))
        .on('click', () => {
          if (interactive && onNodeClick) onNodeClick(nodeId)
        })

      // Background rect
      nodeEl.append('rect')
        .attr('width', 160)
        .attr('height', 56)
        .attr('rx', 10)
        .attr('fill', fill)
        .attr('stroke', stroke)
        .attr('stroke-width', state === 'running' ? 2 : 1.5)
        .attr('class', pulseClass)

      // Entry badge
      if (isEntry) {
        nodeEl.append('rect')
          .attr('x', 0)
          .attr('y', 0)
          .attr('width', 36)
          .attr('height', 14)
          .attr('rx', 3)
          .attr('fill', 'var(--color-accent)')
          .attr('class', 'rounded-tl-lg')
        nodeEl.append('text')
          .attr('x', 18)
          .attr('y', 10)
          .attr('text-anchor', 'middle')
          .attr('fill', 'var(--color-accent-foreground)')
          .attr('font-size', String(7 * fitted.scale))
          .attr('font-weight', 'bold')
          .attr('font-family', 'var(--font-mono)')
          .text('ENTRY')
      }

      // Node ID
      nodeEl.append('text')
        .attr('x', 12)
        .attr('y', 24)
        .attr('fill', 'var(--color-foreground)')
        .attr('font-size', String(11 * fitted.scale))
        .attr('font-weight', '600')
        .attr('font-family', 'var(--font-mono)')
        .text(nodeId)

      // Agent name
      nodeEl.append('text')
        .attr('x', 12)
        .attr('y', 40)
        .attr('fill', 'var(--color-muted-foreground)')
        .attr('font-size', String(9 * fitted.scale))
        .attr('font-family', 'var(--font-sans)')
        .text(node.agent || '')

      // Outcome count badge
      const outcomeCount = Object.keys(node.outputs || {}).length
      if (outcomeCount > 1) {
        nodeEl.append('circle')
          .attr('cx', 148)
          .attr('cy', 16)
          .attr('r', 9)
          .attr('fill', 'var(--color-muted)')
        nodeEl.append('text')
          .attr('x', 148)
          .attr('y', 19)
          .attr('text-anchor', 'middle')
          .attr('fill', 'var(--color-muted-foreground)')
          .attr('font-size', String(8 * fitted.scale))
          .attr('font-family', 'var(--font-mono)')
          .text(String(outcomeCount))
      }
    }
  }, [nodes, edges, entryNodes, nodeStates, interactive, onNodeClick, onEdgeClick])

  useEffect(() => {
    renderGraph()
  }, [renderGraph])

  // Re-render on resize
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const observer = new ResizeObserver(() => {
      renderGraph()
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [renderGraph])

  return (
    <div
      ref={ref}
      className={cn('h-full w-full overflow-hidden', className)}
    />
  )
}
