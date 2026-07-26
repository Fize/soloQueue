import { useCallback, useMemo, useState } from 'react'
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  useReactFlow,
  type Connection,
  type NodeChange,
  type EdgeChange,
  type Node,
  type Edge,
  applyNodeChanges,
  applyEdgeChanges,
  MarkerType,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { DndContext, useDroppable, type DragEndEvent } from '@dnd-kit/core'
import { WorkflowNode } from './WorkflowNodeComponent'
import { WorkflowEdge } from './WorkflowEdgeComponent'
import { AgentPalette } from './AgentPalette'
import { NodePropertyPanel } from './NodePropertyPanel'
import { EdgePropertyPanel } from './EdgePropertyPanel'
import { useWorkflowStore } from '@/stores/workflowStore'
import { MousePointerClick } from 'lucide-react'
import { useTranslation } from '@/lib/i18n'
import type { GraphNode, GraphEdge } from '@/types'

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

function toRFNodes(graphNodes: GraphNode[], entryNodes: string[]): Node[] {
  return graphNodes.map((gn) => ({
    id: gn.id,
    type: 'workflowNode',
    position: gn.position,
    data: {
      ...gn,
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
    sourceHandle: ge.outcome || 'default',
    type: 'workflowEdge',
    markerEnd: { type: MarkerType.ArrowClosed, width: 16, height: 16, color: ge.loop ? 'var(--color-chart-2)' : 'var(--color-chart-1)' },
    data: { outcome: ge.outcome, loop: ge.loop },
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
  const {
    activeWorkflowGraph,
    activeWorkflowAgents,
    setGraph,
    addNode,
    updateNode,
    removeNode,
    addEdge: addGraphEdge,
    updateEdge,
    removeEdge,
  } = useWorkflowStore()

  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null)
  const [selectedEdgeId, setSelectedEdgeId] = useState<string | null>(null)
  const [nodeIdCounter, setNodeIdCounter] = useState(0)

  // Compute entry nodes (nodes with no incoming edges)
  const entryNodes = useMemo(() => {
    const hasIncoming = new Set(activeWorkflowGraph.edges.map(e => e.target))
    return activeWorkflowGraph.nodes.filter(n => !hasIncoming.has(n.id)).map(n => n.id)
  }, [activeWorkflowGraph])

  // ReactFlow nodes/edges
  const rfNodes = useMemo(
    () => toRFNodes(activeWorkflowGraph.nodes, entryNodes),
    [activeWorkflowGraph.nodes, entryNodes]
  )

  const rfEdges = useMemo(
    () => toRFEdges(activeWorkflowGraph.edges),
    [activeWorkflowGraph.edges]
  )

  // Node types
  const nodeTypes = useMemo(() => ({ workflowNode: WorkflowNode }), [])
  const edgeTypes = useMemo(() => ({ workflowEdge: WorkflowEdge }), [])

  // Handle node changes (position updates)
  const onNodesChange = useCallback(
    (changes: NodeChange[]) => {
      const updated = applyNodeChanges(changes, rfNodes)
      // Sync positions back to graph
      const newGraphNodes = activeWorkflowGraph.nodes.map(gn => {
        const updatedNode = updated.find(un => un.id === gn.id)
        if (updatedNode) {
          return { ...gn, position: updatedNode.position }
        }
        return gn
      })
      setGraph({ ...activeWorkflowGraph, nodes: newGraphNodes })
    },
    [rfNodes, activeWorkflowGraph, setGraph]
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
      const edgeId = `${connection.source}:${outcome}:${connection.target}`
      const newEdge: GraphEdge = {
        id: edgeId,
        source: connection.source,
        target: connection.target,
        outcome,
        loop: false,
        maxTraversals: 0,
      }
      addGraphEdge(newEdge)
    },
    [addGraphEdge]
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
      const { active } = event
      if (!active?.data?.current) return
      const data = active.data.current as { type?: string; agentKey?: string; template?: string; model?: string }
      if (data.type !== 'agent') return

      // Convert screen position to flow coordinates
      // Use the pointer event position from the DndContext
      const pointerEvent = event.activatorEvent as PointerEvent | MouseEvent | TouchEvent
      let screenX = 0
      let screenY = 0
      if ('clientX' in pointerEvent) {
        screenX = pointerEvent.clientX
        screenY = pointerEvent.clientY
      }

      // Convert screen coordinates to flow coordinates
      const flowPos = reactFlowInstance.screenToFlowPosition({ x: screenX, y: screenY })

      // Create new node
      const counter = nodeIdCounter + 1
      setNodeIdCounter(counter)
      const nodeId = `${data.agentKey || 'node'}_${counter}`
      const newNode: GraphNode = {
        id: nodeId,
        agent: data.agentKey || '',
        prompt: '',
        outputs: { done: { to: [], loop: false, max_traversals: 0 } },
        position: { x: flowPos.x - 80, y: flowPos.y - 28 },
      }
      addNode(newNode)
    },
    [nodeIdCounter, addNode, reactFlowInstance]
  )

  // Auto layout
  const handleAutoLayout = useCallback(() => {
    const laidOut = autoLayoutNodes(activeWorkflowGraph.nodes)
    setGraph({ ...activeWorkflowGraph, nodes: laidOut })
  }, [activeWorkflowGraph, setGraph])

  // Add a node for an existing agent. Agent references belong to the workflow
  // YAML and must not be fabricated here, otherwise server validation rejects
  // the saved definition.
  const handleAddAgent = useCallback(() => {
    const agentKey = Object.keys(activeWorkflowAgents)[0]
    if (!agentKey) return
    const counter = nodeIdCounter + 1
    setNodeIdCounter(counter)
    const nodeId = `${agentKey}_${counter}`
    const newNode: GraphNode = {
      id: nodeId,
      agent: agentKey,
      prompt: '',
      outputs: { done: { to: [], loop: false, max_traversals: 0 } },
      position: { x: Math.random() * 400 + 200, y: Math.random() * 300 + 100 },
    }
    addNode(newNode)
  }, [nodeIdCounter, addNode, activeWorkflowAgents])

  // Selected items
  const selectedNode = useMemo(
    () => selectedNodeId ? activeWorkflowGraph.nodes.find(n => n.id === selectedNodeId) : null,
    [selectedNodeId, activeWorkflowGraph.nodes]
  )
  const selectedEdge = useMemo(
    () => selectedEdgeId ? activeWorkflowGraph.edges.find(e => e.id === selectedEdgeId) : null,
    [selectedEdgeId, activeWorkflowGraph.edges]
  )

  const agentKeys = Object.keys(activeWorkflowAgents)

  return (
    <DndContext onDragEnd={handleDragEnd}>
      <div className="flex flex-1 min-h-0 overflow-hidden bg-background">
        {/* Agent Palette */}
        <AgentPalette
          agents={activeWorkflowAgents}
          entryNodes={entryNodes}
          onAddAgent={handleAddAgent}
          onAutoLayout={handleAutoLayout}
        />

        {/* Canvas */}
        <DroppableCanvas>
          <ReactFlow
            nodes={rfNodes}
            edges={rfEdges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={onConnect}
            onNodeClick={onNodeClick}
            onEdgeClick={onEdgeClick}
            onPaneClick={onPaneClick}
            nodeTypes={nodeTypes}
            edgeTypes={edgeTypes}
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
            agentKeys={agentKeys}
            onUpdate={(updates) => updateNode(selectedNode.id, updates)}
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
