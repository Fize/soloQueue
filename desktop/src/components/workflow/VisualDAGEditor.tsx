import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  useReactFlow,
  useUpdateNodeInternals,
  type Connection,
  type NodeChange,
  type EdgeChange,
  type Node,
  type Edge,
  applyEdgeChanges,
  MarkerType,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { DndContext, useDroppable, type DragEndEvent } from '@dnd-kit/core'
import { WorkflowNode } from './WorkflowNodeComponent'
import { AgentPalette } from './AgentPalette'
import { NodePropertyPanel } from './NodePropertyPanel'
import { EdgePropertyPanel } from './EdgePropertyPanel'
import { useWorkflowStore } from '@/stores/workflowStore'
import { Link2, MousePointerClick } from 'lucide-react'
import { useTranslation } from '@/lib/i18n'
import type { AgentResponse, GraphNode, GraphEdge } from '@/types'

// ─── Droppable canvas wrapper ───────────────────────────────────────────

function DroppableCanvas({ children }: { children: React.ReactNode }) {
  const { setNodeRef, isOver } = useDroppable({ id: 'workflow-canvas' })
  return (
    <div
      ref={setNodeRef}
      className="flex-1 min-w-0 relative"
      style={{
        outline: isOver ? '2px solid var(--color-primary)' : 'none',
        outlineOffset: -2,
      }}
    >
      {children}
    </div>
  )
}

// ─── Convert GraphState → ReactFlow nodes/edges ────────────────────────

const workflowNodeDimensions = { width: 300, height: 168 }

function toRFNodes(
  graphNodes: GraphNode[],
  entryNodes: string[],
  agentRefs: Record<string, { template: string; model?: string }>,
  availableAgentsByID: Map<string, AgentResponse>,
  isConnecting: boolean,
  pendingConnection: { source: string; outcome: string } | null,
  onStartConnection: (source: string, outcome: string) => void,
  onCompleteConnection: (target: string) => void,
  connectLabel: string,
  connectTargetLabel: string,
): Node[] {
  return graphNodes.map((gn) => ({
    id: gn.id,
    type: 'workflowNode',
    position: gn.position,
    width: workflowNodeDimensions.width,
    height: workflowNodeDimensions.height,
    data: {
      ...gn,
      agentTemplate: agentRefs[gn.agent]?.template || '',
      agentDisplayName: availableAgentsByID.get(agentRefs[gn.agent]?.template || '')?.name || '',
      invalidAgent: !agentRefs[gn.agent] || !availableAgentsByID.has(agentRefs[gn.agent].template),
      isConnecting,
      pendingConnection,
      onStartConnection,
      onCompleteConnection,
      connectLabel,
      connectTargetLabel,
      isEntry: entryNodes.includes(gn.id),
      isTerminal: Object.values(gn.outputs || {}).some(o => o.to.length === 0),
      outcomeKeys: Object.keys(gn.outputs || {}),
    },
  }))
}

function toRFEdges(graphEdges: GraphEdge[]): Edge[] {
  return graphEdges.map((ge) => ({
    id: ge.id,
    source: ge.source,
    target: ge.target,
    type: 'smoothstep',
    label: ge.outcome,
    markerEnd: { type: MarkerType.ArrowClosed, width: 16, height: 16, color: ge.loop ? 'var(--color-chart-2)' : 'var(--color-chart-1)' },
    style: {
      stroke: ge.loop ? 'var(--color-chart-2)' : 'var(--color-chart-1)',
      strokeWidth: 2,
      strokeDasharray: ge.loop ? '5 4' : undefined,
    },
    labelStyle: { fontSize: 9, fontFamily: 'monospace', fill: 'var(--color-muted-foreground)' },
    labelBgStyle: { fill: 'var(--color-card)', fillOpacity: 0.92 },
    labelBgPadding: [6, 3] as [number, number],
    labelBgBorderRadius: 5,
  }))
}

// ─── Auto-layout (simple grid) ─────────────────────────────────────────

function autoLayoutNodes(nodes: GraphNode[]): GraphNode[] {
  const cols = Math.ceil(Math.sqrt(nodes.length))
  return nodes.map((n, i) => ({
    ...n,
    position: {
      x: 100 + (i % cols) * 280,
      y: 80 + Math.floor(i / cols) * 160,
    },
  }))
}

// ─── Component ──────────────────────────────────────────────────────────

export function VisualDAGEditor() {
  const { t } = useTranslation()
  const reactFlowInstance = useReactFlow()
  const updateNodeInternals = useUpdateNodeInternals()
  const {
    activeWorkflowGraph,
    activeWorkflowName,
    activeWorkflowEntryNodes,
    activeWorkflowAgents,
    availableAgents,
    availableAgentsLoading,
    availableAgentsError,
    setGraph,
    addNode,
    updateNode,
    removeNode,
    addEdge: addGraphEdge,
    updateEdge,
    removeEdge,
    toggleEntryNode,
    ensureAgentReference,
  } = useWorkflowStore()

  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null)
  const [selectedEdgeId, setSelectedEdgeId] = useState<string | null>(null)
  const [nodeIdCounter, setNodeIdCounter] = useState(0)
  const [isConnecting, setIsConnecting] = useState(false)
  const [pendingConnection, setPendingConnection] = useState<{ source: string; outcome: string } | null>(null)
  const fittedWorkflowRef = useRef<string | null>(null)

  const entryNodes = activeWorkflowEntryNodes

  const availableAgentNames = useMemo(
    () => new Set(availableAgents.map(agent => agent.id)),
    [availableAgents]
  )
  const availableAgentsByID = useMemo(
    () => new Map(availableAgents.map(agent => [agent.id, agent])),
    [availableAgents]
  )

  const createGraphConnection = useCallback((source: string, target: string, outcome: string) => {
    const edgeId = `${source}:${outcome}:${target}`
    addGraphEdge({
      id: edgeId,
      source,
      target,
      outcome,
      loop: source === target,
      maxTraversals: 0,
    })
  }, [addGraphEdge])

  const startClickConnection = useCallback((source: string, outcome: string) => {
    setPendingConnection({ source, outcome })
    setIsConnecting(true)
  }, [])

  const completeClickConnection = useCallback((target: string) => {
    if (!pendingConnection) return
    createGraphConnection(pendingConnection.source, target, pendingConnection.outcome)
    setPendingConnection(null)
    setIsConnecting(false)
  }, [createGraphConnection, pendingConnection])

  // ReactFlow nodes/edges
  const rfNodes = useMemo(
    () => toRFNodes(
      activeWorkflowGraph.nodes,
      entryNodes,
      activeWorkflowAgents,
      availableAgentsByID,
      isConnecting,
      pendingConnection,
      startClickConnection,
      completeClickConnection,
      t('workflow.connectAction'),
      t('workflow.connectHere'),
    ),
    [
      activeWorkflowGraph.nodes,
      entryNodes,
      activeWorkflowAgents,
      availableAgentsByID,
      isConnecting,
      pendingConnection,
      startClickConnection,
      completeClickConnection,
      t,
    ]
  )

  const rfEdges = useMemo(
    () => toRFEdges(activeWorkflowGraph.edges),
    [activeWorkflowGraph.edges]
  )

  // Node types
  const nodeTypes = useMemo(() => ({ workflowNode: WorkflowNode }), [])

  useEffect(() => {
    const frame = requestAnimationFrame(() => {
      for (const node of rfNodes) updateNodeInternals(node.id)
    })
    return () => cancelAnimationFrame(frame)
  }, [rfNodes, updateNodeInternals])

  // The workflow loads after React Flow mounts. Fit once per workflow when
  // its initial nodes arrive; fitting after every added node would pan the
  // user's canvas away from the drop position.
  useEffect(() => {
    if (!activeWorkflowName || rfNodes.length === 0 || fittedWorkflowRef.current === activeWorkflowName) return
    fittedWorkflowRef.current = activeWorkflowName
    const frame = requestAnimationFrame(() => {
      reactFlowInstance.fitView({ padding: 0.2, duration: 180 })
    })
    return () => cancelAnimationFrame(frame)
  }, [activeWorkflowName, reactFlowInstance, rfNodes.length])

  // Handle node changes (position updates)
  const onNodesChange = useCallback(
    (changes: NodeChange[]) => {
      const positions = new Map(
        changes.flatMap(change =>
          change.type === 'position' && change.position
            ? [[change.id, change.position] as const]
            : []
        )
      )
      if (positions.size === 0) return
      const newGraphNodes = activeWorkflowGraph.nodes.map(gn => {
        const nextPosition = positions.get(gn.id)
        return nextPosition ? { ...gn, position: nextPosition } : gn
      })
      setGraph({ ...activeWorkflowGraph, nodes: newGraphNodes })
    },
    [activeWorkflowGraph, setGraph]
  )

  // Handle edge changes
  const onEdgesChange = useCallback(
    (changes: EdgeChange[]) => {
      applyEdgeChanges(changes, rfEdges)
      // Handle removed edges
      const removedIds = new Set(
        changes
          .filter(c => c.type === 'remove')
          .map(c => c.id)
      )
      if (removedIds.size > 0) {
        for (const id of removedIds) {
          removeEdge(id)
        }
      }
    },
    [rfEdges, removeEdge]
  )

  // Handle new connections
  const onConnect = useCallback(
    (connection: Connection) => {
      if (!connection.source || !connection.target) return
      const outcome = connection.sourceHandle || 'default'
      createGraphConnection(connection.source, connection.target, outcome)
      setPendingConnection(null)
      setIsConnecting(false)
    },
    [createGraphConnection]
  )

  // Handle node click
  const onNodeClick = useCallback(
    (_event: React.MouseEvent, node: Node) => {
      setSelectedNodeId(node.id)
      setSelectedEdgeId(null)
    },
    []
  )

  // Handle edge click
  const onEdgeClick = useCallback(
    (_event: React.MouseEvent, edge: Edge) => {
      setSelectedEdgeId(edge.id)
      setSelectedNodeId(null)
    },
    []
  )

  // Handle pane click (deselect)
  const onPaneClick = useCallback(() => {
    setSelectedNodeId(null)
    setSelectedEdgeId(null)
  }, [])

  // Handle drag from palette → create new node at canvas position
  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event
      if (over?.id !== 'workflow-canvas' || !active?.data?.current) return
      const data = active.data.current as { type?: string; template?: string }
      if (data.type !== 'agent') return
      if (!data.template || !availableAgentNames.has(data.template)) return

      // Convert the pointer-down position plus Dnd Kit's final delta into the
      // pointer-up position. activatorEvent alone is the old bug: it always
      // points at the palette, not at the release location.
      const activator = event.activatorEvent as PointerEvent | MouseEvent | TouchEvent
      const startPoint = 'clientX' in activator
        ? { x: activator.clientX, y: activator.clientY }
        : activator.changedTouches[0]
          ? { x: activator.changedTouches[0].clientX, y: activator.changedTouches[0].clientY }
          : null
      const translated = active.rect.current.translated
      const screenPoint = startPoint
        ? { x: startPoint.x + event.delta.x, y: startPoint.y + event.delta.y }
        : translated
          ? { x: translated.left + translated.width / 2, y: translated.top + translated.height / 2 }
          : null
      if (!screenPoint) return
      const flowPos = reactFlowInstance.screenToFlowPosition(screenPoint)

      // Create new node
      const counter = nodeIdCounter + 1
      setNodeIdCounter(counter)
      const agentRef = ensureAgentReference(data.template)
      const nodeId = `${agentRef}_${counter}`
      const newNode: GraphNode = {
        id: nodeId,
        agent: agentRef,
        prompt: '',
        outputs: { done: { to: [], loop: false, max_traversals: 0 } },
        // screenToFlowPosition already accounts for the canvas transform and
        // the draggable anchor. Applying another card-size offset here would
        // shift the visual node away from the pointer.
        position: flowPos,
      }
      addNode(newNode)
    },
    [nodeIdCounter, addNode, reactFlowInstance, availableAgentNames, ensureAgentReference]
  )

  // Auto layout
  const handleAutoLayout = useCallback(() => {
    const laidOut = autoLayoutNodes(activeWorkflowGraph.nodes)
    setGraph({ ...activeWorkflowGraph, nodes: laidOut })
  }, [activeWorkflowGraph, setGraph])

  // Add a node for an existing agent. Agent references belong to the workflow
  // YAML and must not be fabricated here, otherwise server validation rejects
  // the saved definition.
  const handleAddAgent = useCallback((template: string) => {
    if (!availableAgentNames.has(template)) return
    const counter = nodeIdCounter + 1
    setNodeIdCounter(counter)
    const agentRef = ensureAgentReference(template)
    const nodeId = `${agentRef}_${counter}`
    const newNode: GraphNode = {
      id: nodeId,
      agent: agentRef,
      prompt: '',
      outputs: { done: { to: [], loop: false, max_traversals: 0 } },
      position: { x: 420 + (counter - 1) * 32, y: 180 + (counter - 1) * 24 },
    }
    addNode(newNode)
    requestAnimationFrame(() => {
      reactFlowInstance.fitView({ padding: 0.18, duration: 180 })
    })
  }, [nodeIdCounter, addNode, availableAgentNames, ensureAgentReference, reactFlowInstance])

  // Selected items
  const selectedNode = useMemo(
    () => selectedNodeId ? activeWorkflowGraph.nodes.find(n => n.id === selectedNodeId) : null,
    [selectedNodeId, activeWorkflowGraph.nodes]
  )
  const selectedEdge = useMemo(
    () => selectedEdgeId ? activeWorkflowGraph.edges.find(e => e.id === selectedEdgeId) : null,
    [selectedEdgeId, activeWorkflowGraph.edges]
  )

  const handleNodeAgentChange = useCallback((nodeId: string, template: string) => {
    if (!availableAgentNames.has(template)) return
    const agentRef = ensureAgentReference(template)
    updateNode(nodeId, { agent: agentRef })
  }, [availableAgentNames, ensureAgentReference, updateNode])

  return (
    <DndContext onDragEnd={handleDragEnd}>
      <div className="flex flex-1 min-h-0 overflow-hidden bg-background">
        {/* Agent Palette */}
        <AgentPalette
          agents={availableAgents}
          loading={availableAgentsLoading}
          error={availableAgentsError}
          entryNodes={entryNodes}
          onAddAgent={handleAddAgent}
          onAutoLayout={handleAutoLayout}
        />

        {/* Canvas */}
        <DroppableCanvas>
          <div className="pointer-events-none absolute left-4 top-4 z-10 flex max-w-md items-center gap-2 rounded-lg border border-primary/20 bg-card/95 px-3 py-2 text-[10px] text-muted-foreground shadow-sm backdrop-blur">
            <Link2 className="h-3.5 w-3.5 shrink-0 text-primary" />
            <span>{isConnecting ? t('workflow.connectTargetHint') : t('workflow.connectHint')}</span>
          </div>
          <ReactFlow
            nodes={rfNodes}
            edges={rfEdges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={onConnect}
            onConnectStart={() => setIsConnecting(true)}
            onConnectEnd={() => {
              if (!pendingConnection) setIsConnecting(false)
            }}
            onNodeClick={onNodeClick}
            onEdgeClick={onEdgeClick}
            onPaneClick={onPaneClick}
            nodeTypes={nodeTypes}
            fitView
            deleteKeyCode={['Backspace', 'Delete']}
            multiSelectionKeyCode="Shift"
            selectionKeyCode="Meta"
            className="bg-background"
          >
            <Background
              color="var(--color-muted-foreground)"
              gap={20}
              size={0.5}
              className="opacity-20"
            />
            <Controls
              className="!bg-card !border !border-border/60 !rounded-lg !shadow-sm"
              position="top-right"
            />
            <MiniMap
              className="!bg-card/80 !border !border-border/60 !rounded-lg"
              nodeColor="var(--color-primary)"
              maskColor="rgba(0,0,0,0.4)"
              position="bottom-right"
            />
          </ReactFlow>
        </DroppableCanvas>

        {/* Property Panel */}
        {selectedNode ? (
          <NodePropertyPanel
            node={selectedNode}
            agentRefs={activeWorkflowAgents}
            availableAgents={availableAgents}
            isEntry={entryNodes.includes(selectedNode.id)}
            onUpdate={(updates) => updateNode(selectedNode.id, updates)}
            onAgentChange={(template) => handleNodeAgentChange(selectedNode.id, template)}
            onToggleEntry={() => toggleEntryNode(selectedNode.id)}
            onDelete={() => {
              removeNode(selectedNode.id)
              setSelectedNodeId(null)
            }}
          />
        ) : selectedEdge ? (
          <EdgePropertyPanel
            edge={selectedEdge}
            onUpdate={(updates) => updateEdge(selectedEdge.id, updates)}
            onDelete={() => {
              removeEdge(selectedEdge.id)
              setSelectedEdgeId(null)
            }}
          />
        ) : (
          /* Empty selection placeholder */
          <div className="w-80 shrink-0 border-l border-border/40 bg-card/10 flex items-center justify-center">
            <div className="text-center px-6">
              <MousePointerClick className="h-8 w-8 text-muted-foreground/30 mx-auto mb-3" />
              <p className="text-xs text-muted-foreground">{t('workflow.selectNode')}</p>
              <p className="text-[10px] text-muted-foreground/60 mt-0.5">{t('workflow.selectNodeDesc')}</p>
            </div>
          </div>
        )}
      </div>
    </DndContext>
  )
}
