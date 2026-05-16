import { useMemo, useState, useCallback, useEffect, useRef, memo } from 'react'
import {
  ReactFlow,
  Background,
  useNodesState,
  useEdgesState,
  useReactFlow,
  Handle,
  Position,
  type Node,
  type NodeProps,
  type NodeTypes,
  type Edge,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import dagre from '@dagrejs/dagre'
import { useAgents } from '@/hooks/useAgents'
import { useAgentStore } from '@/stores/agentStore'
import type { AgentInfo, AgentState, AgentTemplate } from '@/types'
import { AgentDetailDialog } from './AgentDetailDialog'
import { cn } from '@/lib/utils'
import { Loader2, Users } from 'lucide-react'

const stateColors: Record<AgentState, string> = {
  processing: '#4DABF7',
  idle: '#69DB7C',
  stopping: '#FFD43B',
  stopped: '#BBBBBB',
}

type AgentNodeData = {
  agent: AgentInfo
  isPlaceholder: boolean
  isL1: boolean
  templateId?: string
  templateName?: string
}
type AppNode = Node<AgentNodeData, 'agent'>

function makePlaceholderAgent(template: AgentTemplate): AgentInfo {
  return {
    id: template.id,
    instance_id: `placeholder-${template.id}`,
    name: template.name,
    state: 'stopped',
    model_id: template.model_id,
    group: template.group,
    is_leader: template.is_leader,
    task_level: '',
    error_count: 0,
    last_error: '',
    pending_delegations: 0,
    mailbox_high: 0,
    mailbox_normal: 0,
  }
}

function AgentFlowNode({ data }: NodeProps<AppNode>) {
  const { agent, isPlaceholder } = data
  const borderColor = stateColors[agent.state] || '#BBBBBB'

  return (
    <div
      className={cn(
        'rounded-lg border shadow-sm px-3 py-2.5 min-w-[160px] transition-shadow',
        isPlaceholder
          ? 'border-dashed bg-muted/30'
          : 'border-solid bg-card cursor-pointer hover:shadow-md'
      )}
      style={{ borderLeft: `4px solid ${borderColor}` }}
    >
      <div className="flex items-center gap-1.5 mb-0.5">
        <span
          className={cn('h-2 w-2 rounded-full shrink-0', isPlaceholder && 'opacity-40')}
          style={{ backgroundColor: borderColor }}
        />
        <span
          className={cn(
            'text-xs font-semibold truncate flex-1',
            isPlaceholder ? 'text-muted-foreground/60' : 'text-card-foreground'
          )}
        >
          {agent.name}
        </span>
        {agent.is_leader && <span className="text-[9px] font-bold text-primary uppercase">L</span>}
      </div>

      {isPlaceholder ? (
        <p className="text-[10px] text-muted-foreground/40 italic mt-0.5">not started</p>
      ) : (
        <>
          <p className="text-[10px] text-muted-foreground truncate font-mono">{agent.model_id}</p>
          {agent.error_count > 0 && (
            <p className="text-[10px] text-destructive font-medium mt-0.5">✗{agent.error_count}</p>
          )}
        </>
      )}

      <Handle type="target" position={Position.Top} className="!opacity-0" />
      <Handle type="source" position={Position.Bottom} className="!opacity-0" />
    </div>
  )
}

const defaultNodeTypes: NodeTypes = { agent: AgentFlowNode }

const NODE_WIDTH = 180
const NODE_HEIGHT = 78

function layoutElements(nodes: AppNode[], edges: Edge[]): AppNode[] {
  const g = new dagre.graphlib.Graph()
  g.setDefaultEdgeLabel(() => ({}))
  g.setGraph({ rankdir: 'TB', nodesep: 60, ranksep: 90, marginx: 20, marginy: 20 })

  nodes.forEach((n) => g.setNode(n.id, { width: NODE_WIDTH, height: NODE_HEIGHT }))
  edges.forEach((e) => g.setEdge(e.source, e.target))

  dagre.layout(g)

  return nodes.map((n) => {
    const pos = g.node(n.id)
    return {
      ...n,
      position: { x: pos.x - NODE_WIDTH / 2, y: pos.y - NODE_HEIGHT / 2 },
    }
  })
}

function FitViewOnChange({ count }: { count: number }) {
  const { fitView } = useReactFlow()
  const prevRef = useRef(0)

  useEffect(() => {
    if (count > 0 && prevRef.current !== count) {
      prevRef.current = count
      requestAnimationFrame(() => fitView({ duration: 0, maxZoom: 1.5, padding: 0.2 }))
    }
  }, [count])

  return null
}

interface FlowViewProps {
  nodes: AppNode[]
  edges: Edge[]
  onNodeClick: (_: unknown, node: Node) => void
}

const FlowView = memo(function FlowView({
  nodes: initialNodes,
  edges: initialEdges,
  onNodeClick,
}: FlowViewProps) {
  const [flowNodes, setFlowNodes, onNodesChange] = useNodesState(initialNodes)
  const [flowEdges, setFlowEdges, onEdgesChange] = useEdgesState(initialEdges)

  useEffect(() => {
    setFlowNodes(initialNodes)
    setFlowEdges(initialEdges)
  }, [initialNodes, initialEdges])

  return (
    <div className="h-full rounded-xl border bg-card overflow-hidden relative">
      <ReactFlow
        nodes={flowNodes}
        edges={flowEdges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        nodeTypes={defaultNodeTypes}
        onNodeClick={onNodeClick}
        nodesDraggable
        nodesConnectable={false}
        elementsSelectable={false}
        panOnDrag
        zoomOnScroll
        preventScrolling
        minZoom={0.4}
        maxZoom={2}
      >
        <Background gap={24} size={1} color="#E6EBF1" />
        <FitViewOnChange count={flowNodes.length} />
      </ReactFlow>
    </div>
  )
})

const DialogHost = memo(function DialogHost({
  open,
  onOpenChange,
  agent,
  templateId,
  templateName,
  isL1,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  agent: AgentInfo | null
  templateId: string | null
  templateName: string | null
  isL1: boolean
}) {
  return (
    <AgentDetailDialog
      agent={agent}
      templateId={templateId}
      templateName={templateName}
      isL1={isL1}
      open={open}
      onOpenChange={onOpenChange}
    />
  )
})

export function AgentFlow() {
  const agentsData = useAgents()
  const teamsData = useAgentStore((s) => s.teams)
  const teamsLoading = useAgentStore((s) => s.teamsLoading)
  const fetchTeams = useAgentStore((s) => s.fetchTeams)

  const [selectedAgent, setSelectedAgent] = useState<AgentInfo | null>(null)
  const [selectedTemplateId, setSelectedTemplateId] = useState<string | null>(null)
  const [selectedTemplateName, setSelectedTemplateName] = useState<string | null>(null)
  const [isDetailOpen, setIsDetailOpen] = useState(false)
  const [isL1, setIsL1] = useState(false)

  const onNodeClick = useCallback((_: unknown, node: Node) => {
    const data = node.data as AgentNodeData
    setIsL1(data.isL1)
    if (data.isPlaceholder) {
      setSelectedAgent(null)
      setSelectedTemplateId(data.templateId ?? null)
      setSelectedTemplateName(data.templateName ?? null)
    } else {
      setSelectedAgent(data.agent)
      setSelectedTemplateId(null)
      setSelectedTemplateName(null)
    }
    setIsDetailOpen(true)
  }, [])

  useEffect(() => {
    fetchTeams()
  }, [fetchTeams])

  const { initialNodes, initialEdges } = useMemo(() => {
    const runningAgents = agentsData?.agents ?? []
    const supervisors = agentsData?.supervisors ?? []
    const teams = teamsData?.teams ?? []

    const l2Ids = new Set(supervisors.map((sv) => sv.leader_id).filter(Boolean))
    const l3Ids = new Set(supervisors.flatMap((sv) => sv.children_ids))

    const l1Running = runningAgents.find(
      (a) => !l2Ids.has(a.instance_id) && !l3Ids.has(a.instance_id)
    )

    const nodeMap = new Map<string, AppNode>()
    const edgeList: Edge[] = []

    const l1NodeId = l1Running?.instance_id ?? 'placeholder-l1'

    if (l1Running) {
      nodeMap.set(l1Running.instance_id, {
        id: l1Running.instance_id,
        type: 'agent',
        data: { agent: l1Running, isPlaceholder: false, isL1: true },
        position: { x: 0, y: 0 },
      })
    } else {
      nodeMap.set('placeholder-l1', {
        id: 'placeholder-l1',
        type: 'agent',
        data: {
          agent: {
            id: 'main',
            instance_id: 'placeholder-l1',
            name: 'Main Agent',
            state: 'stopped',
            model_id: '',
            group: '',
            is_leader: true,
            task_level: '',
            error_count: 0,
            last_error: '',
            pending_delegations: 0,
            mailbox_high: 0,
            mailbox_normal: 0,
          },
          isPlaceholder: true,
          isL1: true,
          templateId: 'main',
          templateName: 'Main Agent',
        },
        position: { x: 0, y: 0 },
      })
    }

    const sortedTeams = [...teams].sort((a, b) => a.name.localeCompare(b.name))

    for (const team of sortedTeams) {
      const leaderNodeIds: string[] = []
      const childNodeIds: string[] = []

      for (const template of team.agents) {
        const matchingAgents = runningAgents.filter((a) => a.id === template.id)

        if (matchingAgents.length > 0) {
          for (const agent of matchingAgents) {
            if (!nodeMap.has(agent.instance_id)) {
              nodeMap.set(agent.instance_id, {
                id: agent.instance_id,
                type: 'agent',
                data: { agent, isPlaceholder: false, isL1: false },
                position: { x: 0, y: 0 },
              })
            }
            if (template.is_leader) {
              leaderNodeIds.push(agent.instance_id)
            } else {
              childNodeIds.push(agent.instance_id)
            }
          }
        } else {
          const pid = `placeholder-${template.id}`
          if (!nodeMap.has(pid)) {
            nodeMap.set(pid, {
              id: pid,
              type: 'agent',
              data: {
                agent: makePlaceholderAgent(template),
                isPlaceholder: true,
                isL1: false,
                templateId: template.id,
                templateName: template.name,
              },
              position: { x: 0, y: 0 },
            })
          }
          if (template.is_leader) {
            leaderNodeIds.push(pid)
          } else {
            childNodeIds.push(pid)
          }
        }
      }

      for (const leaderId of leaderNodeIds) {
        const lidPair = `${l1NodeId}|${leaderId}`
        if (!edgeList.some((e) => e.source === l1NodeId && e.target === leaderId)) {
          edgeList.push({
            id: `t-${lidPair}`,
            source: l1NodeId,
            target: leaderId,
            type: 'smoothstep',
            style: { stroke: '#C0C8D0', strokeWidth: 1, strokeDasharray: '5,5' },
          })
        }

        for (const childId of childNodeIds) {
          const lcPair = `${leaderId}|${childId}`
          if (!edgeList.some((e) => e.source === leaderId && e.target === childId)) {
            edgeList.push({
              id: `t-${lcPair}`,
              source: leaderId,
              target: childId,
              type: 'smoothstep',
              style: { stroke: '#C0C8D0', strokeWidth: 1, strokeDasharray: '5,5' },
            })
          }
        }
      }
    }

    for (const sv of supervisors) {
      for (const childId of sv.children_ids) {
        const child = runningAgents.find((a) => a.instance_id === childId)
        const existingIdx = edgeList.findIndex(
          (e) => e.source === sv.leader_id && e.target === childId
        )
        const solidEdge: Edge = {
          id: `s-${sv.leader_id}|${childId}`,
          source: sv.leader_id,
          target: childId,
          type: 'smoothstep',
          animated: child?.state === 'processing',
          style: { stroke: '#8898AA', strokeWidth: 1.5 },
        }
        if (existingIdx >= 0) {
          edgeList[existingIdx] = solidEdge
        } else {
          edgeList.push(solidEdge)
        }
      }
    }

    if (l1Running) {
      for (const sv of supervisors) {
        const pair = `${l1Running.instance_id}|${sv.leader_id}`
        if (
          !edgeList.some((e) => e.source === l1Running.instance_id && e.target === sv.leader_id)
        ) {
          const leader = runningAgents.find((a) => a.instance_id === sv.leader_id)
          const existingIdx = edgeList.findIndex(
            (e) => e.source === l1Running.instance_id && e.target === sv.leader_id
          )
          const solidEdge: Edge = {
            id: `s-${pair}`,
            source: l1Running.instance_id,
            target: sv.leader_id,
            type: 'smoothstep',
            animated: leader?.state === 'processing',
            style: { stroke: '#8898AA', strokeWidth: 1.5 },
          }
          if (existingIdx >= 0) {
            edgeList[existingIdx] = solidEdge
          } else {
            edgeList.push(solidEdge)
          }
        }
      }
    }

    const allNodes = Array.from(nodeMap.values())
    const laidOut = layoutElements(allNodes, edgeList)

    return { initialNodes: laidOut, initialEdges: edgeList }
  }, [agentsData, teamsData])

  const loading = agentsData === null && teamsLoading
  if (loading) {
    return (
      <div className="flex h-full items-center justify-center rounded-xl border bg-card/30">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (initialNodes.length === 0) {
    return (
      <div className="flex h-full items-center justify-center rounded-xl border border-dashed bg-card/30">
        <div className="flex flex-col items-center gap-2 text-muted-foreground">
          <Users className="h-10 w-10" />
          <p className="text-sm font-medium">No agents configured</p>
          <p className="text-xs">Define agents in settings to get started</p>
        </div>
      </div>
    )
  }

  return (
    <>
      <FlowView nodes={initialNodes} edges={initialEdges} onNodeClick={onNodeClick} />
      <DialogHost
        agent={selectedAgent}
        templateId={selectedTemplateId}
        templateName={selectedTemplateName}
        isL1={isL1}
        open={isDetailOpen}
        onOpenChange={setIsDetailOpen}
      />
    </>
  )
}
