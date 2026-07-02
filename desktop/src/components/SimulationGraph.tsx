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
  relationships?: RelationshipDTO[] // NEW
  graphLayer?: 'interaction' | 'relationship' | 'both' // NEW
  onSelectAgent?: (agentId: string | null) => void
  selectedAgentId?: string | null
  pulseNodes?: Set<string>
  pulseVersion?: number
  activeAgentIds?: Set<string>
  onPulseRelationship?: (subjectId: string, targetId: string) => void // NEW
  onOpenDetails?: (agentId: string) => void // NEW
}

const ROLE_COLORS: Record<string, string> = {
  moderator: '#ff6b00',
  mediator: '#ff6b00',
  host: '#ff6b00',
  pro: '#10b981',
  con: '#f43f5e',
  neutral: '#3b82f6',
}

const MAX_GRAPH_LINKS = 200
const NODE_RADIUS = 24

// ─── Relationship Edge Styles ─────────────────────────────────────────────
const RELATION_STYLES: Record<
  string,
  { color: string; dash: number[]; width: number; label: string; arrow: boolean }
> = {
  parent: { color: '#ec4899', dash: [], width: 2.5, label: 'Parent', arrow: true },
  child: { color: '#ec4899', dash: [4, 4], width: 2, label: 'Child', arrow: true },
  sibling: { color: '#a855f7', dash: [], width: 2, label: 'Sibling', arrow: false },
  spouse: { color: '#ec4899', dash: [2, 6], width: 2, label: 'Spouse', arrow: false },
  friend: { color: '#14b8a6', dash: [], width: 2, label: 'Friend', arrow: false },
  rival: { color: '#9a3412', dash: [6, 3], width: 2.5, label: 'Rival', arrow: false },
  colleague: { color: '#64748b', dash: [], width: 1.5, label: 'Colleague', arrow: false },
  mentor: { color: '#d97706', dash: [3, 3], width: 2, label: 'Mentor', arrow: true },
  mentee: { color: '#d97706', dash: [6, 3], width: 2, label: 'Mentee', arrow: true },
  neighbor: { color: '#94a3b8', dash: [2, 4], width: 1.5, label: 'Neighbor', arrow: false },
  stranger: { color: '#cbd5e1', dash: [1, 6], width: 0.5, label: 'Stranger', arrow: false },
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
  onOpenDetails,
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
    primaryColor: '#5e6ad2',
    cardColor: '#fff',
    fgColor: '#000',
    mutedFgColor: '#666',
    isDark: false,
  })
  const transformRef = useRef(zoomIdentity)
  const animFrameRef = useRef<number>(0)
  const graphLayerRef = useRef(graphLayer)
  const relationshipsRef = useRef(relationships)
  const onOpenDetailsRef = useRef(onOpenDetails)
  // Sync refs with props
  useEffect(() => {
    selectRef.current = onSelectAgent
  }, [onSelectAgent])
  useEffect(() => {
    onOpenDetailsRef.current = onOpenDetails
  }, [onOpenDetails])
  useEffect(() => {
    selectedIdRef.current = selectedAgentId
  }, [selectedAgentId])
  useEffect(() => {
    pulseRef.current = pulseNodes
  }, [pulseNodes])
  useEffect(() => {
    activeRef.current = activeAgentIds
  }, [activeAgentIds])
  useEffect(() => {
    graphLayerRef.current = graphLayer
  }, [graphLayer])
  useEffect(() => {
    relationshipsRef.current = relationships
  }, [relationships])

  // Build nodes from personas
  useEffect(() => {
    const nodes: GraphNode[] = personas.map((p) => ({
      id: p.id,
      name: p.name,
      role: p.role,
      color: ROLE_COLORS[p.role?.toLowerCase()] || ROLE_COLORS.neutral,
      isActive: true,
    }))
    nodesRef.current = nodes

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

    const simulation = d3force
      .forceSimulation<GraphNode>(nodes)
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
        const found = nodesRef.current.find((node) => {
          const dx = node.x! - x
          const dy = node.y! - y
          return dx * dx + dy * dy < 900
        })
        if (found) {
          // Return custom subject wrapper to keep event.x/y as raw DOM coordinates
          return {
            node: found,
            startX: event.x,
            startY: event.y,
            initialNodeX: found.x!,
            initialNodeY: found.y!,
          }
        }
        return undefined
      })
      .on('start', (event) => {
        if (!event.active) simulation.alphaTarget(0.3).restart()
        const sub = event.subject as any
        if (sub && sub.node) {
          sub.node.fx = sub.initialNodeX
          sub.node.fy = sub.initialNodeY
        }
      })
      .on('drag', (event) => {
        const t = transformRef.current
        const sub = event.subject as any
        if (sub && sub.node) {
          const dx = (event.x - sub.startX) / t.k
          const dy = (event.y - sub.startY) / t.k
          sub.node.fx = sub.initialNodeX + dx
          sub.node.fy = sub.initialNodeY + dy
        }
      })
      .on('end', (event) => {
        if (!event.active) simulation.alphaTarget(0)
        const sub = event.subject as any
        if (sub && sub.node) {
          sub.node.fx = null
          sub.node.fy = null
          const dx = event.x - sub.startX
          const dy = event.y - sub.startY
          if (dx * dx + dy * dy < 25) {
            onOpenDetailsRef.current?.(sub.node.id)
          }
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

    const handleCanvasClick = (e: MouseEvent) => {
      const rect = canvas.getBoundingClientRect()
      const clickX = e.clientX - rect.left
      const clickY = e.clientY - rect.top
      const t = transformRef.current
      const x = (clickX - t.x) / t.k
      const y = (clickY - t.y) / t.k
      const clickedNode = nodesRef.current.find((node) => {
        const dx = node.x! - x
        const dy = node.y! - y
        return dx * dx + dy * dy < 900
      })
      if (!clickedNode) {
        selectRef.current?.(null)
      }
    }
    canvas.addEventListener('click', handleCanvasClick)

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
      c.clearRect(0, 0, canvas.width, canvas.height)
      c.scale(dpr, dpr)

      // Apply zoom/pan transform
      c.translate(transform.x, transform.y)
      c.scale(transform.k, transform.k)

      // Grid
      if (transform.k > 0.4) {
        const gridSpacing = 60
        const gridAlpha = Math.min(1, (transform.k - 0.4) / 0.6) * 0.03
        c.strokeStyle = t.isDark ? `rgba(255,255,255,${gridAlpha})` : `rgba(0,0,0,${gridAlpha})`
        c.lineWidth = 1 / transform.k
        const offsetX = (transform.x % (gridSpacing * transform.k)) / transform.k
        const offsetY = (transform.y % (gridSpacing * transform.k)) / transform.k
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
        const nodeMap = new Map(nodesRef.current.map((n) => [n.id, n]))
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

          const linkType = link.type.toLowerCase()
          let edgeColor: string
          if (linkType.includes('agree') || linkType.includes('support')) {
            edgeColor = 'rgba(16, 185, 129, 0.4)'
          } else if (
            linkType.includes('rebut') ||
            linkType.includes('disagree') ||
            linkType.includes('oppose')
          ) {
            edgeColor = 'rgba(244, 63, 94, 0.4)'
          } else {
            edgeColor = 'rgba(94, 106, 210, 0.25)'
          }
          c.strokeStyle = edgeColor
          c.lineWidth = Math.min(1.5 + link.weight * 0.5, 5) / transform.k
          c.setLineDash([])

          const bp = computeBorderPoint(
            source,
            target,
            NODE_RADIUS / transform.k,
            NODE_RADIUS / transform.k
          )
          const dotT = (Date.now() / 2000) % 1
          let dotX: number, dotY: number

          if (layer === 'both') {
            // Curve interaction edges opposite to relationship edges for visual separation
            const dx = target.x! - source.x!
            const dy = target.y! - source.y!
            const len = Math.sqrt(dx * dx + dy * dy) || 1
            const px = -dy / len
            const py = dx / len
            const cx = (source.x! + target.x!) / 2 + px * (-25 / transform.k)
            const cy = (source.y! + target.y!) / 2 + py * (-25 / transform.k)

            c.beginPath()
            c.moveTo(bp.startX, bp.startY)
            c.quadraticCurveTo(cx, cy, bp.endX, bp.endY)
            c.stroke()

            // Animated dot along curve
            const mt = 1 - dotT
            dotX = mt * mt * bp.startX + 2 * mt * dotT * cx + dotT * dotT * bp.endX
            dotY = mt * mt * bp.startY + 2 * mt * dotT * cy + dotT * dotT * bp.endY

            // Arrow at end of curved edge (tangent to curve at t=1)
            if (transform.k > 0.6) {
              drawArrowHead(
                c,
                bp.endX,
                bp.endY,
                Math.atan2(bp.endY - cy, bp.endX - cx),
                transform.k,
                edgeColor
              )
            }
          } else {
            c.beginPath()
            c.moveTo(bp.startX, bp.startY)
            c.lineTo(bp.endX, bp.endY)
            c.stroke()

            dotX = bp.startX + (bp.endX - bp.startX) * dotT
            dotY = bp.startY + (bp.endY - bp.startY) * dotT

            if (transform.k > 0.6) {
              const angle = Math.atan2(target.y! - source.y!, target.x! - source.x!)
              drawArrowHead(c, bp.endX, bp.endY, angle, transform.k, edgeColor)
            }
          }

          // Animated dot (shared for both curved and straight)
          const dotRadius = Math.max(2, 3 / transform.k)
          c.beginPath()
          c.arc(dotX, dotY, dotRadius, 0, 2 * Math.PI)
          c.fillStyle = edgeColor
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
      canvas.removeEventListener('click', handleCanvasClick)
      simRef.current = null
    }
  }, [personas])

  // Rebuild or update links when edges or personas change
  useEffect(() => {
    if (!simRef.current) return
    const nodes = nodesRef.current
    if (nodes.length === 0) return

    const newLinks: GraphLink[] = []

    for (const e of edges) {
      const src = nodes.find((n) => n.id === e.source || n.name === e.source)
      const tgt = nodes.find((n) => n.id === e.target || n.name === e.target)
      if (!src || !tgt) continue

      newLinks.push({ source: src, target: tgt, type: e.type, weight: e.weight })
    }

    linksRef.current = newLinks.slice(-MAX_GRAPH_LINKS)

    const linkForce = simRef.current.force('link') as
      | d3force.ForceLink<GraphNode, GraphLink>
      | undefined
    if (linkForce) {
      linkForce.links(linksRef.current)
    }

    simRef.current.alpha(0.3).restart()
  }, [edges, personas])

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
            <span
              className="w-2.5 h-2.5 rounded-full block"
              style={{ backgroundColor: ROLE_COLORS.host }}
            />{' '}
            Moderator/Mediator
          </span>
          <span className="flex items-center gap-1.5">
            <span
              className="w-2.5 h-2.5 rounded-full block"
              style={{ backgroundColor: ROLE_COLORS.pro }}
            />{' '}
            Supporter (Pro)
          </span>
          <span className="flex items-center gap-1.5">
            <span
              className="w-2.5 h-2.5 rounded-full block"
              style={{ backgroundColor: ROLE_COLORS.con }}
            />{' '}
            Opponent (Con)
          </span>
          <span className="flex items-center gap-1.5">
            <span
              className="w-2.5 h-2.5 rounded-full block"
              style={{ backgroundColor: ROLE_COLORS.neutral }}
            />{' '}
            Neutral
          </span>
        </div>
        {/* Relationship legend */}
        {relationships.length > 0 && (graphLayer === 'relationship' || graphLayer === 'both') && (
          <div className="mt-1.5 pt-1.5 border-t border-border/40">
            <div className="text-[9px] text-muted-foreground mb-1">Social Relationships:</div>
            <div className="flex flex-wrap gap-x-3 gap-y-0.5">
              {getActiveRelationKinds(relationships).map((kind) => {
                const style = RELATION_STYLES[kind] || RELATION_STYLES.stranger
                return (
                  <span key={kind} className="flex items-center gap-1">
                    <svg width="14" height="4" className="overflow-visible shrink-0">
                      <line
                        x1="0"
                        y1="2"
                        x2="14"
                        y2="2"
                        stroke={style.color}
                        strokeWidth={style.width}
                        strokeDasharray={style.dash.length > 0 ? style.dash.join(',') : 'none'}
                        strokeLinecap="round"
                      />
                    </svg>
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
        Scroll to zoom • Drag to pan • Click to select agent
      </div>
    </div>
  )
}

// ─── Relationship Edge Drawing ──────────────────────────────────────────

function drawRelationshipEdge(
  c: CanvasRenderingContext2D,
  link: GraphLink,
  transform: ZoomTransform
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
  c.setLineDash(style.dash.map((d) => d / k))
  c.strokeStyle =
    style.color +
    Math.round(alpha * 255)
      .toString(16)
      .padStart(2, '0')
  c.lineWidth = (style.width * (0.5 + familiarity * 0.5)) / k
  c.lineCap = 'round'

  // Compute node border points (edges stop at circle boundary, not center)
  const x1 = source.x!
  const y1 = source.y!
  const x2 = target.x!
  const y2 = target.y!
  const bp = computeBorderPoint(source, target, NODE_RADIUS / k, NODE_RADIUS / k)
  const dx = x2 - x1
  const dy = y2 - y1
  const len = Math.sqrt(dx * dx + dy * dy) || 1
  const px = -dy / len
  const py = dx / len

  const curveOffset = 25 / k
  const cx = (x1 + x2) / 2 + px * curveOffset
  const cy = (y1 + y2) / 2 + py * curveOffset

  c.beginPath()
  c.moveTo(bp.startX, bp.startY)
  c.quadraticCurveTo(cx, cy, bp.endX, bp.endY)
  c.stroke()

  // Arrow for directional relationships
  if (style.arrow) {
    const angle = Math.atan2(bp.endY - cy, bp.endX - cx)
    drawArrowHead(c, bp.endX, bp.endY, angle, k, style.color)
  }

  c.restore()
}

// ─── Edge Utilities ────────────────────────────────────────────────────

function computeBorderPoint(
  source: GraphNode,
  target: GraphNode,
  sourceRadius: number,
  targetRadius: number
): { startX: number; startY: number; endX: number; endY: number } {
  const dx = target.x! - source.x!
  const dy = target.y! - source.y!
  const dist = Math.sqrt(dx * dx + dy * dy)
  if (dist < 1) return { startX: source.x!, startY: source.y!, endX: target.x!, endY: target.y! }
  return {
    startX: source.x! + (dx / dist) * sourceRadius,
    startY: source.y! + (dy / dist) * sourceRadius,
    endX: target.x! - (dx / dist) * targetRadius,
    endY: target.y! - (dy / dist) * targetRadius,
  }
}

function drawArrowHead(
  c: CanvasRenderingContext2D,
  tipX: number,
  tipY: number,
  angle: number,
  k: number,
  color: string
) {
  const headLen = 8 / k
  c.save()
  c.setLineDash([])
  c.beginPath()
  c.moveTo(tipX, tipY)
  c.lineTo(
    tipX - headLen * Math.cos(angle - Math.PI / 6),
    tipY - headLen * Math.sin(angle - Math.PI / 6)
  )
  c.lineTo(
    tipX - headLen * Math.cos(angle + Math.PI / 6),
    tipY - headLen * Math.sin(angle + Math.PI / 6)
  )
  c.closePath()
  c.fillStyle = color
  c.fill()
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
    parent: 0,
    child: 1,
    sibling: 2,
    spouse: 3,
    friend: 4,
    rival: 5,
    colleague: 6,
    mentor: 7,
    mentee: 8,
    neighbor: 9,
    stranger: 10,
  }
  return Array.from(kinds).sort((a, b) => (order[a] ?? 99) - (order[b] ?? 99))
}