import { useEffect, useRef } from 'react'
import * as d3force from 'd3-force'
import { drag } from 'd3-drag'
import { select } from 'd3-selection'
import { zoom, zoomIdentity, type ZoomTransform } from 'd3-zoom'
import type { SimulationPersona, RelationshipDTO } from '@/types'

interface GraphNode extends d3force.SimulationNodeDatum {
  id: string
  name: string
  role: string
  color: string
  isActive: boolean
}

interface GraphLink extends d3force.SimulationLinkDatum<GraphNode> {
  type: string
  weight: number
  linkKind?: 'interaction' | 'relationship'
  relationKind?: string
  familiarity?: number
  affinity?: number
  subjectId?: string
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
  relationships?: RelationshipDTO[]  // NEW
  graphLayer?: 'interaction' | 'relationship' | 'both'  // NEW
  onSelectAgent?: (agentId: string) => void
  selectedAgentId?: string | null
  pulseNodes?: Set<string>
  pulseVersion?: number
  activeAgentIds?: Set<string>
  onPulseRelationship?: (subjectId: string, targetId: string) => void  // NEW
}

const ROLE_COLORS: Record<string, string> = {
  moderator: '#f54e00',
  mediator: '#f54e00',
  host: '#f54e00',
  pro: '#16a34a',
  con: '#dc2626',
  neutral: '#2563eb',
}

const MAX_GRAPH_LINKS = 200

// ─── Relationship Edge Styles ─────────────────────────────────────────────
const RELATION_STYLES: Record<string, { color: string; dash: number[]; width: number; label: string; arrow: boolean }> = {
  parent:    { color: '#e91e63', dash: [],       width: 2.5, label: 'Parent',    arrow: true },
  child:     { color: '#e91e63', dash: [4, 4],   width: 2,   label: 'Child',     arrow: true },
  sibling:   { color: '#9c27b0', dash: [],       width: 2,   label: 'Sibling',   arrow: false },
  spouse:    { color: '#e91e63', dash: [2, 6],   width: 2,   label: 'Spouse',    arrow: false },
  friend:    { color: '#4caf50', dash: [],       width: 2,   label: 'Friend',    arrow: false },
  rival:     { color: '#f44336', dash: [6, 3],   width: 2.5, label: 'Rival',     arrow: false },
  colleague: { color: '#2196f3', dash: [],       width: 1.5, label: 'Colleague', arrow: false },
  mentor:    { color: '#ff9800', dash: [3, 3],   width: 2,   label: 'Mentor',    arrow: true },
  mentee:    { color: '#ff9800', dash: [6, 3],   width: 2,   label: 'Mentee',    arrow: true },
  neighbor:  { color: '#607d8b', dash: [2, 4],   width: 1.5, label: 'Neighbor',  arrow: false },
  stranger:  { color: '#9e9e9e', dash: [1, 6],   width: 0.5, label: 'Stranger',  arrow: false },
}

export function SimulationGraph({
  personas,
  edges,
  relationships = [],
  graphLayer = 'both',
  onSelectAgent,
  selectedAgentId,
  pulseNodes,
  pulseVersion: _pulseVersion,
  activeAgentIds,
  onPulseRelationship,
}: SimulationGraphProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null)
  const containerRef = useRef<HTMLDivElement | null>(null)
  const simRef = useRef<d3force.Simulation<GraphNode, GraphLink> | null>(null)
  const linksRef = useRef<GraphLink[]>([])
  const nodesRef = useRef<GraphNode[]>([])
  const selectRef = useRef(onSelectAgent)
  const selectedIdRef = useRef(selectedAgentId)
  const pulseRef = useRef(pulseNodes)
  const activeRef = useRef(activeAgentIds)
  const ctxRef = useRef<CanvasRenderingContext2D | null>(null)
  const themeRef = useRef({
    primaryColor: '#f54e00',
    cardColor: '#fff',
    fgColor: '#000',
    mutedFgColor: '#666',
    isDark: false,
  })
  const transformRef = useRef(zoomIdentity)
  const animFrameRef = useRef<number>(0)
  const graphLayerRef = useRef(graphLayer)
  const relationshipsRef = useRef(relationships)
  // Sync refs with props
  useEffect(() => { selectRef.current = onSelectAgent }, [onSelectAgent])
  useEffect(() => { selectedIdRef.current = selectedAgentId }, [selectedAgentId])
  useEffect(() => { pulseRef.current = pulseNodes }, [pulseNodes])
  useEffect(() => { activeRef.current = activeAgentIds }, [activeAgentIds])
  useEffect(() => { graphLayerRef.current = graphLayer }, [graphLayer])
  useEffect(() => { relationshipsRef.current = relationships }, [relationships])

  useEffect(() => {
    if (!onPulseRelationship) return
    selectRef.current = onSelectAgent
  }, [onSelectAgent, onPulseRelationship])

  // Build nodes from personas
  useEffect(() => {
    const nodes: GraphNode[] = personas.map((p) => ({
      id: p.id,
      name: p.name,
      role: p.role,
      color: ROLE_COLORS[p.role?.toLowerCase()] || ROLE_COLORS.neutral,
      isActive: true,
    }))

    const canvas = canvasRef.current
    const container = containerRef.current
    if (!canvas || !container) return

    const dpr = window.devicePixelRatio || 1
    const rect = container.getBoundingClientRect()
    canvas.width = rect.width * dpr
    canvas.height = rect.height * dpr
    canvas.style.width = rect.width + 'px'
    canvas.style.height = rect.height + 'px'
    ctxRef.current = canvas.getContext('2d')!

    // Detect dark mode
    themeRef.current.isDark = document.documentElement.classList.contains('dark')

    const simulation = d3force.forceSimulation<GraphNode>(nodes)
      .force('center', d3force.forceCenter(rect.width / 2, rect.height / 2))
      .force('charge', d3force.forceManyBody().strength(-300))
      .force('link', d3force.forceLink<GraphNode, GraphLink>([]).distance(120).strength(0.3))
      .force('collision', d3force.forceCollide().radius(40))

    // Drag behavior
    const dragBehavior = drag<HTMLCanvasElement, unknown>()
      .subject((event) => {
        const t = transformRef.current
        const x = (event.x - t.x) / t.k
        const y = (event.y - t.y) / t.k
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
        const t = transformRef.current
        event.subject.fx = (event.x - t.x) / t.k
        event.subject.fy = (event.y - t.y) / t.k
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

    const zoomBehavior = zoom<HTMLCanvasElement, unknown>()
      .scaleExtent([0.2, 4])
      .on('zoom', (event) => {
        transformRef.current = event.transform
      })

    const selection = select(canvas)
    selection.call(dragBehavior as any)
    selection.call(zoomBehavior as any)

    const preventScroll = (e: WheelEvent) => {
      if (container.contains(e.target as Node)) {
        e.preventDefault()
      }
    }
    container.addEventListener('wheel', preventScroll, { passive: false })

    let running = true
    const draw = () => {
      if (!running) return
      const c = ctxRef.current
      if (!c) {
        animFrameRef.current = requestAnimationFrame(draw)
        return
      }

      const w = canvas.width / dpr
      const h = canvas.height / dpr
      const t = themeRef.current
      const transform = transformRef.current
      const layer = graphLayerRef.current
      const rels = relationshipsRef.current

      c.save()
      c.clearRect(0, 0, w, h)

      // Apply zoom/pan transform
      c.translate(transform.x, transform.y)
      c.scale(transform.k, transform.k)

      // Grid
      if (transform.k > 0.4) {
        const gridSpacing = 60
        const gridAlpha = Math.min(1, (transform.k - 0.4) / 0.6) * 0.03
        c.strokeStyle = t.isDark ? `rgba(255,255,255,${gridAlpha})` : `rgba(0,0,0,${gridAlpha})`
        c.lineWidth = 1 / transform.k
        const offsetX = ((transform.x % (gridSpacing * transform.k)) / transform.k)
        const offsetY = ((transform.y % (gridSpacing * transform.k)) / transform.k)
        for (let x = -gridSpacing + offsetX; x < w / transform.k + gridSpacing; x += gridSpacing) {
          c.beginPath()
          c.moveTo(x, -gridSpacing)
          c.lineTo(x, h / transform.k + gridSpacing)
          c.stroke()
        }
        for (let y = -gridSpacing + offsetY; y < h / transform.k + gridSpacing; y += gridSpacing) {
          c.beginPath()
          c.moveTo(-gridSpacing, y)
          c.lineTo(w / transform.k + gridSpacing, y)
          c.stroke()
        }
      }

      // ─── Draw Links ──────────────────────────────────────────────────
      const showInteraction = layer === 'interaction' || layer === 'both'
      const showRelationship = layer === 'relationship' || layer === 'both'

      // Accumulate relationship edges from RelationshipDTOs
      const relEdges: GraphLink[] = []
      if (showRelationship && rels.length > 0) {
        const nodeMap = new Map(nodesRef.current.map(n => [n.id, n]))
        for (const rel of rels) {
          const src = nodeMap.get(rel.subject_id)
          const tgt = nodeMap.get(rel.target_id)
          if (src && tgt) {
            relEdges.push({
              source: src,
              target: tgt,
              type: rel.kind,
              weight: 0,
              linkKind: 'relationship',
              relationKind: rel.kind,
              familiarity: rel.familiarity,
              affinity: rel.affinity,
              subjectId: rel.subject_id,
            })
          }
        }
      }

      // Draw relationship edges first (bottom layer)
      if (showRelationship) {
        for (const link of relEdges) {
          drawRelationshipEdge(c, link, transform)
        }
      }

      // Draw interaction edges (top layer)
      if (showInteraction) {
        for (const link of linksRef.current) {
          const source = link.source as GraphNode
          const target = link.target as GraphNode
          if (!source || !target) continue

          c.beginPath()
          c.moveTo(source.x!, source.y!)
          c.lineTo(target.x!, target.y!)
          const linkType = link.type.toLowerCase()
          if (linkType.includes('agree') || linkType.includes('support')) {
            c.strokeStyle = 'rgba(22,163,74,0.4)'
          } else if (linkType.includes('rebut') || linkType.includes('disagree') || linkType.includes('oppose')) {
            c.strokeStyle = 'rgba(220,38,38,0.4)'
          } else {
            c.strokeStyle = t.primaryColor + '44'
          }
          c.lineWidth = Math.min(1.5 + link.weight * 0.5, 5) / transform.k
          c.stroke()

          // Animated dot along interaction edges only
          const dotT = (Date.now() / 2000) % 1
          const dotX = source.x! + (target.x! - source.x!) * dotT
          const dotY = source.y! + (target.y! - source.y!) * dotT
          const dotRadius = Math.max(2, 3 / transform.k)
          c.beginPath()
          c.arc(dotX, dotY, dotRadius, 0, 2 * Math.PI)
          c.fillStyle = c.strokeStyle
          c.fill()
        }
      }

      // ─── Draw Nodes ──────────────────────────────────────────────────
      const baseRadius = 24
      nodesRef.current.forEach((node) => {
        const isSelected = selectedIdRef.current === node.id
        const isPulsing = pulseRef.current?.has(node.id)
        const isInactive = activeRef.current && !activeRef.current.has(node.id)
        const r = baseRadius / transform.k

        // Pulse ring
        if (isPulsing) {
          const pulsePhase = (Date.now() / 600) % 1
          const pulseRadius = r + Math.sin(pulsePhase * Math.PI * 2) * (8 / transform.k)
          const pulseAlpha = 0.35 * (1 - pulsePhase)
          c.beginPath()
          c.arc(node.x!, node.y!, pulseRadius, 0, 2 * Math.PI)
          c.fillStyle =
            node.color +
            Math.round(pulseAlpha * 255)
              .toString(16)
              .padStart(2, '0')
          c.fill()
        }

        // Selection ring
        if (isSelected) {
          c.beginPath()
          c.arc(node.x!, node.y!, r + 8 / transform.k, 0, 2 * Math.PI)
          c.fillStyle = t.primaryColor + '22'
          c.strokeStyle = t.primaryColor + '99'
          c.lineWidth = 2 / transform.k
          c.fill()
          c.stroke()
        }

        // Node body
        c.beginPath()
        c.arc(node.x!, node.y!, r, 0, 2 * Math.PI)
        c.fillStyle = isInactive ? t.mutedFgColor + '44' : t.cardColor
        c.strokeStyle = isInactive ? t.mutedFgColor + '66' : node.color
        c.lineWidth = isSelected ? 3 / transform.k : 2 / transform.k
        c.fill()
        c.stroke()

        // Inner dot
        c.beginPath()
        c.arc(node.x!, node.y!, 6 / transform.k, 0, 2 * Math.PI)
        c.fillStyle = isInactive ? t.mutedFgColor + '66' : node.color
        c.fill()

        // Label
        const fontSize = Math.max(10, 13 / transform.k)
        c.font = `bold ${fontSize}px ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace`
        c.fillStyle = isInactive ? t.mutedFgColor : t.fgColor
        c.textAlign = 'center'
        c.textBaseline = 'top'
        c.fillText(node.name, node.x!, node.y! + r + 4 / transform.k)

        // Role label
        const roleFontSize = Math.max(8, 10 / transform.k)
        c.font = `${roleFontSize}px sans-serif`
        c.fillStyle = isInactive ? t.mutedFgColor + '88' : t.mutedFgColor
        const roleLabel = node.role.length > 20 ? node.role.slice(0, 17) + '...' : node.role
        c.fillText(roleLabel, node.x!, node.y! + r + 4 / transform.k + fontSize + 2 / transform.k)
      })

      // Inactive overlay
      if (activeRef.current && nodesRef.current.some((n) => !activeRef.current!.has(n.id))) {
        c.restore()
        c.save()
        c.fillStyle = t.mutedFgColor + '99'
        c.font = '11px ui-monospace, sans-serif'
        c.textAlign = 'right'
        c.textBaseline = 'bottom'
        c.fillText('Gray = inactive agents', w - 12, h - 12)
      }

      c.restore()

      animFrameRef.current = requestAnimationFrame(draw)
    }

    animFrameRef.current = requestAnimationFrame(draw)
    simRef.current = simulation

    return () => {
      running = false
      cancelAnimationFrame(animFrameRef.current)
      simulation.stop()
      selection.on('.drag', null)
      selection.on('.zoom', null)
      container.removeEventListener('wheel', preventScroll)
      simRef.current = null
    }
  }, [personas, graphLayer]) // re-init when layer changes

  // Incrementally update links when edges change
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
    if (existing.length > MAX_GRAPH_LINKS) {
      existing.splice(0, existing.length - MAX_GRAPH_LINKS)
    }

    const linkForce = simRef.current.force('link') as d3force.ForceLink<GraphNode, GraphLink> | undefined
    if (linkForce) {
      linkForce.links(existing)
    }

    if (newLinks.length > 0 || existing.length > 0) {
      simRef.current.alpha(0.3).restart()
    }
  }, [edges])

  // Sync relationships to trigger redraw
  useEffect(() => {
    relationshipsRef.current = relationships
  }, [relationships])

  // Expose pulseRelationship method
  useEffect(() => {
    if (!onPulseRelationship) return
    // The parent can call onPulseRelationship to trigger edge pulse
  }, [onPulseRelationship])

  return (
    <div
      ref={containerRef}
      className="relative w-full h-full min-h-[400px] overflow-hidden bg-card/30 rounded-xl border border-border"
    >
      <canvas ref={canvasRef} className="absolute inset-0 cursor-grab active:cursor-grabbing" />
      {/* Legend - Interaction layer */}
      <div className="absolute top-3 left-3 bg-card/90 backdrop-blur-md px-3 py-1.5 rounded-lg border border-border text-[10px] text-muted-foreground font-mono shadow-sm z-10">
        <div className="flex flex-wrap gap-x-4 gap-y-1">
          <span className="flex items-center gap-1.5">
            <span className="w-2.5 h-2.5 rounded-full bg-primary block" /> Host/Moderator
          </span>
          <span className="flex items-center gap-1.5">
            <span className="w-2.5 h-2.5 rounded-full bg-success block" /> Pro
          </span>
          <span className="flex items-center gap-1.5">
            <span className="w-2.5 h-2.5 rounded-full bg-error block" /> Con
          </span>
        </div>
        {/* Relationship legend */}
        {relationships.length > 0 && (graphLayer === 'relationship' || graphLayer === 'both') && (
          <div className="mt-1.5 pt-1.5 border-t border-border/40">
            <div className="text-[9px] text-muted-foreground mb-1">Relationships:</div>
            <div className="flex flex-wrap gap-x-3 gap-y-0.5">
              {getActiveRelationKinds(relationships).map((kind) => {
                const style = RELATION_STYLES[kind] || RELATION_STYLES.stranger
                return (
                  <span key={kind} className="flex items-center gap-1">
                    <span
                      className="inline-block w-3 h-0.5 rounded-full"
                      style={{
                        backgroundColor: style.color,
                        borderTop: style.dash.length > 0 ? `${style.width}px dashed ${style.color}` : 'none',
                      }}
                    />
                    <span className="text-[9px]">{style.label}</span>
                  </span>
                )
              })}
            </div>
          </div>
        )}
      </div>
      {/* Controls hint */}
      <div className="absolute bottom-3 right-3 text-[10px] text-muted-foreground/60 font-mono select-none z-10 bg-card/60 backdrop-blur-sm px-2 py-1 rounded border border-border/40">
        Scroll to zoom • Drag to pan • Click to select
      </div>
    </div>
  )
}

// ─── Relationship Edge Drawing ──────────────────────────────────────────

function drawRelationshipEdge(
  c: CanvasRenderingContext2D,
  link: GraphLink,
  transform: ZoomTransform,
) {
  const source = link.source as GraphNode
  const target = link.target as GraphNode
  if (!source || !target) return

  const kind = link.relationKind || 'stranger'
  const style = RELATION_STYLES[kind] || RELATION_STYLES.stranger
  const k = transform.k

  c.save()

  const familiarity = link.familiarity ?? 0.5
  const alpha = Math.min(0.9, 0.3 + familiarity * 0.6)

  // Dashed line
  c.setLineDash(style.dash.map(d => d / k))
  c.strokeStyle = style.color + Math.round(alpha * 255).toString(16).padStart(2, '0')
  c.lineWidth = (style.width * (0.5 + familiarity * 0.5)) / k
  c.lineCap = 'round'

  c.beginPath()
  c.moveTo(source.x!, source.y!)
  c.lineTo(target.x!, target.y!)
  c.stroke()

  // Arrow for directional relationships
  if (style.arrow) {
    const angle = Math.atan2(target.y! - source.y!, target.x! - source.x!)
    const headLen = 10 / k
    const offset = 24 / k
    const endX = target.x! - Math.cos(angle) * offset
    const endY = target.y! - Math.sin(angle) * offset

    c.setLineDash([])
    c.beginPath()
    c.moveTo(endX, endY)
    c.lineTo(
      endX - headLen * Math.cos(angle - Math.PI / 6),
      endY - headLen * Math.sin(angle - Math.PI / 6),
    )
    c.lineTo(
      endX - headLen * Math.cos(angle + Math.PI / 6),
      endY - headLen * Math.sin(angle + Math.PI / 6),
    )
    c.closePath()
    c.fillStyle = style.color
    c.fill()
  }

  c.restore()
}

// ─── Helper: get unique relationship kinds from relationship data ──────

function getActiveRelationKinds(relationships: RelationshipDTO[]): string[] {
  const kinds = new Set<string>()
  for (const rel of relationships) {
    if (rel.kind && rel.kind !== 'stranger') {
      kinds.add(rel.kind)
    }
  }
  // Sort: family first, then social, then professional
  const order: Record<string, number> = {
    parent: 0, child: 1, sibling: 2, spouse: 3,
    friend: 4, rival: 5,
    colleague: 6, mentor: 7, mentee: 8,
    neighbor: 9, stranger: 10,
  }
  return Array.from(kinds).sort((a, b) => (order[a] ?? 99) - (order[b] ?? 99))
}
