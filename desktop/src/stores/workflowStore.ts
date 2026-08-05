import { create } from 'zustand'
import { parseDocument } from 'yaml'
import type {
  AgentResponse,
  WorkflowMeta,
  WorkflowDef,
  GraphState,
  GraphNode,
  GraphEdge,
  WorkflowRunSummary,
  WorkflowRunDetail,
  WorkflowTask,
  BuiltinWorkflowView,
} from '@/types'
import { listAgents } from '@/lib/api/agent-api'
import {
  cancelWorkflowRun,
  createWorkflow as createWorkflowRequest,
  deleteWorkflow as deleteWorkflowRequest,
  getWorkflow,
  getWorkflowRun,
  listWorkflowRuns,
  listWorkflows,
  startWorkflowRun,
  updateWorkflow as updateWorkflowRequest,
  validateWorkflowYAML,
  pauseWorkflowRun,
  resumeWorkflowRun,
  restartWorkflowRun,
  abandonWorkflowRun,
  cleanupWorkflowRun,
  listBuiltinWorkflows,
  installBuiltinWorkflows,
} from '@/lib/api/workflow-api'

// ─── YAML ↔ Graph Serialization ─────────────────────────────────────────

// Serialize GraphState to YAML string
export function graphToYAML(
  name: string,
  graph: GraphState,
  agents: Record<string, { template: string; model?: string }>,
  entryNodes = graph.nodes.filter(n => !graph.edges.some(e => e.target === n.id)).map(n => n.id),
): string {
  const lines: string[] = []
  lines.push(`name: ${name}`)
  lines.push('description: ""')
  lines.push('version: "1"')
  lines.push('')
  lines.push('defaults:')
  lines.push('  node_timeout: 20m')
  lines.push('  workflow_timeout: 45m')
  lines.push('  max_node_runs: 50')
  lines.push('  max_output_bytes: 131072')
  lines.push('')
  lines.push('agents:')
  for (const [key, ref] of Object.entries(agents)) {
    lines.push(`  ${key}:`)
    lines.push(`    template: ${ref.template}`)
    if (ref.model) lines.push(`    model: ${ref.model}`)
  }
  lines.push('')
  lines.push('entry:')
  for (const entryNode of entryNodes) {
    lines.push(`  - ${entryNode}`)
  }
  lines.push('')
  lines.push('nodes:')
  for (const node of graph.nodes) {
    lines.push(`  - id: ${node.id}`)
    lines.push(`    agent: ${node.agent}`)
    lines.push(`    prompt: |`)
    const promptLines = node.prompt.split('\n')
    for (const pl of promptLines) {
      lines.push(`      ${pl}`)
    }
    if (node.timeout) lines.push(`    timeout: ${node.timeout}`)
    if (node.join) {
      lines.push('    join:')
      lines.push(`      mode: ${node.join.mode}`)
      lines.push('      from:')
      for (const f of node.join.from) {
        lines.push(`        - ${f}`)
      }
    }
    lines.push('    outputs:')
    for (const [outcome, output] of Object.entries(node.outputs)) {
      lines.push(`      ${outcome}:`)
      if (output.to.length > 0) {
        lines.push('        to:')
        for (const t of output.to) {
          lines.push(`          - ${t}`)
        }
      } else {
        lines.push('        to: []')
      }
      if (output.loop) {
        lines.push(`        loop: true`)
        lines.push(`        max_traversals: ${output.max_traversals}`)
      }
      if (output.terminal_status) lines.push(`        terminal_status: ${output.terminal_status}`)
    }
    if (node.onError && node.onError.strategy !== 'fail') {
      lines.push('    on_error:')
      lines.push(`      strategy: ${node.onError.strategy}`)
      lines.push(`      max_attempts: ${node.onError.max_attempts}`)
    }
    // Persist position as YAML comment
    lines.push(`    # @position: ${Math.round(node.position.x)},${Math.round(node.position.y)}`)
  }
  return lines.join('\n') + '\n'
}

// Applies visual changes to the existing document instead of recreating the
// whole YAML file. This preserves top-level metadata, custom defaults and
// comments unrelated to the graph.
function mergeGraphIntoYAML(
  existingYAML: string,
  name: string,
  graph: GraphState,
  agents: Record<string, { template: string; model?: string }>,
  entryNodes: string[],
): string {
  const doc = parseDocument(existingYAML)
  if (doc.errors.length > 0 || !doc.contents) return graphToYAML(name, graph, agents, entryNodes)
  doc.set('name', name)
  doc.set('agents', agents)
  doc.set('entry', entryNodes)
  const yamlNodes = doc.createNode(graph.nodes.map(node => ({
    id: node.id,
    agent: node.agent,
    prompt: node.prompt,
    ...(node.timeout ? { timeout: node.timeout } : {}),
    ...(node.join ? { join: node.join } : {}),
    outputs: node.outputs,
    ...(node.onError ? { on_error: node.onError } : {}),
  }))) as { items?: Array<{ comment?: string }> }
  yamlNodes.items?.forEach((item, index) => {
    const position = graph.nodes[index]?.position
    if (position) item.comment = ` @position: ${Math.round(position.x)},${Math.round(position.y)}`
  })
  doc.set('nodes', yamlNodes)
  return String(doc)
}

// Parse YAML string to GraphState
// Simplified parser — extracts nodes and edges from workflow YAML
export function yamlToGraph(yaml: string): { graph: GraphState; name: string; agents: Record<string, { template: string; model?: string }>; entryNodes: string[] } | null {
  try {
    const document = parseDocument(yaml)
    if (document.errors.length > 0) return null
    const documentValue = document.toJS() as {
      name?: unknown
      entry?: unknown
      agents?: Record<string, { template?: unknown; model?: unknown }>
    }
    const entryNodes = Array.isArray(documentValue.entry)
      ? documentValue.entry.filter((entry): entry is string => typeof entry === 'string')
      : []
    let nodes: GraphNode[] = []
    const edges: GraphEdge[] = []
    const agents: Record<string, { template: string; model?: string }> = Object.fromEntries(
      Object.entries(documentValue.agents || {}).flatMap(([key, ref]) => {
        if (!ref || typeof ref.template !== 'string') return []
        return [[key, {
          template: ref.template,
          ...(typeof ref.model === 'string' && ref.model ? { model: ref.model } : {}),
        }]]
      })
    )
    let name = typeof documentValue.name === 'string' ? documentValue.name : ''
    let currentNode: Partial<GraphNode> | null = null
    const positionedNodeIDs = new Set<string>()

    const lines = yaml.split('\n')
    let i = 0

    // Helper: get indent level (number of leading spaces)
    const getIndent = (line: string) => line.search(/\S/)

    while (i < lines.length) {
      const line = lines[i]
      // Skip empty lines and plain comments
      const trimmed = line.trim()
      if (trimmed === '' || (trimmed.startsWith('#') && !trimmed.startsWith('# @position:'))) {
        i++
        continue
      }

      // Capture position comment
      const posMatch = trimmed.match(/#\s*@position:\s*(-?\d+\.?\d*),\s*(-?\d+\.?\d*)/)
      if (posMatch) {
        const savedPosition = { x: parseFloat(posMatch[1]), y: parseFloat(posMatch[2]) }
        if (currentNode?.id) {
          currentNode.position = savedPosition
          positionedNodeIDs.add(currentNode.id)
        }
        i++
        continue
      }

      const indent = getIndent(line)
      const content = trimmed

      // --- Top-level fields (indent 0) ---
      if (indent === 0) {
        if (content.startsWith('name:')) {
          name = content.replace(/^name:\s*"?/, '').replace(/"$/, '').trim()
        }
        // sections: agents, nodes
        // These are section markers, handled by indent-based context
        i++
        continue
      }

      // --- nodes: section ---
      // Node entries start with "- id:" at indent 2
      if (indent === 2 && content.startsWith('- id:')) {
        // Finalize previous node
        if (currentNode && currentNode.id) {
          finalizeNode()
        }
        currentNode = { outputs: {} }
        currentNode.id = content.replace(/^- id:\s*/, '').trim()
        i++
        continue
      }

      // Node properties at indent 4
      if (indent === 4 && currentNode) {
        if (content.startsWith('agent:')) {
          currentNode.agent = content.replace(/^agent:\s*/, '').trim()
          i++
          continue
        }
        if (content.startsWith('prompt: |') || content === 'prompt: |') {
          // Multi-line prompt
          const promptLines: string[] = []
          i++
          while (i < lines.length) {
            const pline = lines[i]
            const pindent = getIndent(pline)
            if (pindent >= 6 && pline.trim() !== '' && !pline.trim().startsWith('#')) {
              promptLines.push(pline.trim())
            } else if (pline.trim() === '') {
              // Empty line in prompt
              promptLines.push('')
            } else {
              break
            }
            i++
          }
          currentNode.prompt = promptLines.join('\n')
          continue
        }
        if (content.startsWith('timeout:')) {
          currentNode.timeout = content.replace(/^timeout:\s*/, '').trim()
          i++
          continue
        }
        if (content === 'outputs:') {
          currentNode.outputs = {}
          // Start production rule: skip the outputs section marker
          i++
          // Now process outputs at indent 6
          while (i < lines.length) {
            const oline = lines[i]
            const oindent = getIndent(oline)
            const ocontent = oline.trim()
            if (oindent < 6 || ocontent.startsWith('# @position:') || (oindent === 4 && ocontent !== '')) break
            if (ocontent === '' || ocontent.startsWith('#')) { i++; continue }

            if (oindent === 6) {
              // New outcome key
              const outcomeMatch = ocontent.match(/^(\w+):$/)
              if (outcomeMatch) {
                const outcomeName = outcomeMatch[1]
                // Initialize output
                if (!currentNode.outputs) currentNode.outputs = {}
                currentNode.outputs[outcomeName] = { to: [], loop: false, max_traversals: 0 }
                i++
                // Process sub-properties at indent >= 8
                while (i < lines.length) {
                  const subline = lines[i]
                  const subindent = getIndent(subline)
                  const subcontent = subline.trim()
                  if (subindent < 8 || subcontent.startsWith('# @position:')) break
                  if (subcontent === '' || subcontent.startsWith('#')) { i++; continue }

                  if (subcontent.startsWith('to:')) {
                    if (subcontent === 'to: []') {
                      // Terminal output
                      currentNode.outputs![outcomeName].to = []
                    } else if (/^to:\s*\[.*\]\s*$/.test(subcontent)) {
                      const inlineTargets = subcontent
                        .replace(/^to:\s*\[/, '')
                        .replace(/\]\s*$/, '')
                        .split(',')
                        .map(target => target.trim().replace(/^['"]|['"]$/g, ''))
                        .filter(Boolean)
                      currentNode.outputs![outcomeName].to = inlineTargets
                    } else {
                      // to: followed by list items at indent 10
                      i++
                      const toTargets: string[] = []
                      while (i < lines.length) {
                        const tline = lines[i]
                        const tindent = getIndent(tline)
                        const tcontent = tline.trim()
                        if (tindent < 10 || tcontent.startsWith('# @position:')) break
                        if (tcontent === '' || tcontent.startsWith('#')) { i++; continue }
                        if (tcontent.startsWith('- ')) {
                          toTargets.push(tcontent.replace(/^-\s*/, '').trim())
                        }
                        i++
                      }
                      currentNode.outputs![outcomeName].to = toTargets
                      continue // i already incremented in inner loop
                    }
                  }
                  if (subcontent === 'loop: true') {
                    currentNode.outputs![outcomeName].loop = true
                  }
                  if (subcontent.startsWith('max_traversals:')) {
                    currentNode.outputs![outcomeName].max_traversals = parseInt(subcontent.replace(/^max_traversals:\s*/, '').trim()) || 1
                  }
                  if (subcontent.startsWith('terminal_status:')) {
                    const value = subcontent.replace(/^terminal_status:\s*/, '').trim()
                    if (value === 'completed' || value === 'blocked' || value === 'failed') {
                      currentNode.outputs![outcomeName].terminal_status = value
                    }
                  }
                  i++
                }
                continue
              }
            }
            i++
          }
          continue
        }
        if (content === 'on_error:') {
          currentNode.onError = { strategy: 'fail', max_attempts: 1 }
          i++
          while (i < lines.length) {
            const eline = lines[i]
            const eindent = getIndent(eline)
            const econtent = eline.trim()
            if (eindent < 6 || econtent.startsWith('# @position:')) break
            if (econtent.startsWith('strategy:')) {
              const strat = econtent.replace(/^strategy:\s*/, '').trim()
              currentNode.onError!.strategy = strat as 'fail' | 'retry'
            }
            if (econtent.startsWith('max_attempts:')) {
              currentNode.onError!.max_attempts = parseInt(econtent.replace(/^max_attempts:\s*/, '').trim()) || 1
            }
            i++
          }
          continue
        }
        if (content === 'join:') {
          currentNode.join = { mode: 'all', from: [] }
          i++
          while (i < lines.length) {
            const jline = lines[i]
            const jindent = getIndent(jline)
            const jcontent = jline.trim()
            if (jindent < 6 || jcontent.startsWith('# @position:')) break
            if (jcontent.startsWith('mode:')) {
              currentNode.join!.mode = jcontent.replace(/^mode:\s*/, '').trim() as 'all'
            }
            if (jcontent.startsWith('- ') && jindent >= 8) {
              currentNode.join!.from.push(jcontent.replace(/^-\s*/, '').trim())
            }
            i++
          }
          continue
        }
        i++
        continue
      }

      i++
    }

    // Finalize last node
    function finalizeNode() {
      if (currentNode && currentNode.id) {
        nodes.push({
          id: currentNode.id!,
          agent: currentNode.agent || '',
          prompt: currentNode.prompt || '',
          timeout: currentNode.timeout,
          join: currentNode.join && currentNode.join.from.length >= 2 ? currentNode.join : undefined,
          outputs: currentNode.outputs || {},
          onError: currentNode.onError && currentNode.onError.strategy !== 'fail' ? currentNode.onError : undefined,
          position: currentNode.position || { x: nodes.length * 200, y: 100 },
        })
      }
    }
    finalizeNode()

    // Build edges from node outputs
    for (const node of nodes) {
      for (const [outcome, output] of Object.entries(node.outputs)) {
        for (const targetId of output.to) {
          edges.push({
            id: `${node.id}:${outcome}:${targetId}`,
            source: node.id,
            target: targetId,
            outcome,
            loop: output.loop || false,
            maxTraversals: output.max_traversals || 0,
          })
        }
        if (output.loop) {
          // Also add self-loop edges (when to includes the node itself)
          const hasSelf = output.to.includes(node.id)
          if (!hasSelf) {
            // Loop edge exists but to list may not include self
            // The loop flag means it's a back-edge; the to list should include the target
          }
        }
      }
    }

    if (nodes.length > 0 && positionedNodeIDs.size === 0) {
      nodes = autoLayoutNodes(nodes)
    }

    // Return null for garbage input that produces no valid content
    if (nodes.length === 0 && !name) {
      return null
    }

    return { graph: { nodes, edges }, name, agents, entryNodes }
  } catch {
    return null
  }
}

// ─── Default YAML template ──────────────────────────────────────────────

export function agentAliasForTemplate(template: string): string {
  let alias = template.trim().replace(/[^A-Za-z0-9_-]+/g, '_')
  if (!/^[A-Za-z]/.test(alias)) alias = `agent_${alias}`
  alias = alias.replace(/_+/g, '_').slice(0, 64).replace(/[_-]+$/, '')
  return alias || 'agent'
}

export function defaultYAMLTemplate(name: string, agentID?: string): string {
  if (!agentID) {
    return `name: ${name}
description: ""
version: "1"

defaults:
  node_timeout: 20m
  workflow_timeout: 45m
  max_node_runs: 50
  max_output_bytes: 131072

agents: {}
entry: []
nodes: []
`
  }

  const agentAlias = agentAliasForTemplate(agentID)
  return `name: ${name}
description: ""
version: "1"

defaults:
  node_timeout: 20m
  workflow_timeout: 45m
  max_node_runs: 50
  max_output_bytes: 131072

agents:
  ${agentAlias}:
    template: ${JSON.stringify(agentID)}

entry:
  - start

nodes:
  - id: start
    agent: ${agentAlias}
    prompt: |
      Describe what this step should accomplish.
    outputs:
      done:
        to: []
`
}

export function unknownAgentTemplates(
  agents: Record<string, { template: string; model?: string }>,
  availableAgents: AgentResponse[],
): string[] {
  const available = new Set(availableAgents.map(agent => agent.id))
  return [...new Set(
    Object.values(agents)
      .map(ref => ref.template)
      .filter(template => !available.has(template))
  )]
}

function edgesFromNodes(nodes: GraphNode[]): GraphEdge[] {
  return nodes.flatMap(node => Object.entries(node.outputs).flatMap(([outcome, output]) =>
    output.to.map(target => ({
      id: `${node.id}:${outcome}:${target}`,
      source: node.id,
      target,
      outcome,
      loop: output.loop,
      maxTraversals: output.max_traversals,
    }))
  ))
}

// Keep initial graph coordinates deterministic and readable without adding a
// layout dependency. Saved YAML positions still take precedence in yamlToGraph.
export function autoLayoutNodes(nodes: GraphNode[]): GraphNode[] {
  const nodeIDs = new Set(nodes.map(node => node.id))
  const outgoing = new Map<string, string[]>()
  const indegree = new Map<string, number>()
  const layers = new Map<string, number>()

  for (const node of nodes) {
    const targets = Object.values(node.outputs || {})
      .flatMap(output => output.to)
      .filter(target => nodeIDs.has(target))
    outgoing.set(node.id, targets)
    indegree.set(node.id, 0)
  }
  for (const targets of outgoing.values()) {
    for (const target of targets) indegree.set(target, (indegree.get(target) || 0) + 1)
  }

  const roots = nodes.filter(node => indegree.get(node.id) === 0).map(node => node.id)
  const queue = roots.length > 0 ? roots : nodes.slice(0, 1).map(node => node.id)
  queue.forEach(nodeID => layers.set(nodeID, 0))
  const visited = new Set<string>()
  for (let index = 0; index < queue.length; index += 1) {
    const source = queue[index]
    if (visited.has(source)) continue
    visited.add(source)
    const sourceLayer = layers.get(source) || 0
    for (const target of outgoing.get(source) || []) {
      // Assign each node once from the entry side. A loop back-edge therefore
      // points to an existing layer instead of shifting the whole graph.
      if (layers.has(target)) continue
      layers.set(target, sourceLayer + 1)
      queue.push(target)
    }
  }

  const highestLayer = Math.max(-1, ...layers.values())
  nodes.forEach((node, index) => {
    if (!layers.has(node.id)) layers.set(node.id, highestLayer + 1 + index)
  })

  const rowByLayer = new Map<number, number>()
  return nodes.map(node => {
    const layer = layers.get(node.id) || 0
    const row = rowByLayer.get(layer) || 0
    rowByLayer.set(layer, row + 1)
    return {
      ...node,
      position: {
        x: 120 + layer * 360,
        y: 120 + row * 220,
      },
    }
  })
}

// ─── Store ──────────────────────────────────────────────────────────────

export type DirtySource = 'visual' | 'yaml' | null

interface WorkflowState {
  // Existing DB-backed agents are the only source for workflow agent refs.
  availableAgents: AgentResponse[]
  availableAgentsLoading: boolean
  availableAgentsError: string | null
  fetchAvailableAgents: () => Promise<void>

  // Workflow definitions list
  workflowMetas: WorkflowMeta[]
  workflowMetasLoading: boolean
  workflowMetasError: string | null
  fetchWorkflowMetas: () => Promise<void>
  builtinWorkflows: BuiltinWorkflowView[]
  fetchBuiltinWorkflows: () => Promise<void>
  installBuiltinWorkflow: (name: string) => Promise<boolean>

  // Active workflow editor state
  activeWorkflowName: string | null
  activeWorkflowYAML: string
  activeWorkflowGraph: GraphState
  activeWorkflowEntryNodes: string[]
  activeWorkflowAgents: Record<string, { template: string; model?: string }>
  activeWorkflowLoading: boolean
  activeWorkflowLoadError: string | null
  activeWorkflowParsed: WorkflowDef | null
  activeWorkflowValidationError: string | null
  dirtySource: DirtySource
  editorMode: 'visual' | 'yaml'

  setActiveWorkflow: (name: string) => Promise<void>
  setEditorMode: (mode: 'visual' | 'yaml') => void

  // Graph mutations (visual mode)
  setGraph: (graph: GraphState) => void
  addNode: (node: GraphNode) => void
  updateNode: (nodeId: string, updates: Partial<GraphNode>) => void
  removeNode: (nodeId: string) => void
  addEdge: (edge: GraphEdge) => void
  updateEdge: (edgeId: string, updates: Partial<GraphEdge>) => void
  removeEdge: (edgeId: string) => void
  toggleEntryNode: (nodeId: string) => void
  ensureAgentReference: (template: string) => string

  // YAML mutations (yaml mode)
  setYAML: (yaml: string) => void

  // CRUD operations
  createWorkflow: (name: string, yaml: string) => Promise<boolean>
  updateWorkflow: (name: string) => Promise<boolean>
  deleteWorkflow: (name: string) => Promise<boolean>
  validateWorkflow: () => Promise<{ valid: boolean; error?: string }>

  // Runs list for a specific workflow
  runs: Record<string, WorkflowRunSummary[]>
  runsLoading: boolean
  fetchRuns: (workflowName: string) => Promise<void>

  // Active run detail
  activeRunDetail: WorkflowRunDetail | null
  activeRunDetailLoading: boolean
  fetchRunDetail: (workflowName: string, runId: string) => Promise<void>
  clearActiveRunDetail: () => void

  // Run controls
  startRun: (workflowName: string, task: WorkflowTask) => Promise<string | null>
  cancelRun: (workflowName: string, runId: string) => Promise<void>
  pauseRun: (workflowName: string, runId: string, mode?: 'graceful' | 'force') => Promise<void>
  resumeRun: (workflowName: string, runId: string, allowDirty?: boolean) => Promise<void>
  restartRun: (workflowName: string, runId: string) => Promise<string | null>
  abandonRun: (workflowName: string, runId: string) => Promise<void>
  cleanupRun: (workflowName: string, runId: string, force?: boolean) => Promise<void>

  // Sync guard
  _syncing: boolean
}

// Inflight promise dedup
let inflightMetasLoad: Promise<void> | null = null

export const useWorkflowStore = create<WorkflowState>((set, get) => ({
  availableAgents: [],
  availableAgentsLoading: false,
  availableAgentsError: null,

  fetchAvailableAgents: async () => {
    set({ availableAgentsLoading: true, availableAgentsError: null })
    try {
      const agents = await listAgents()
      set({ availableAgents: agents, availableAgentsLoading: false })
    } catch (err: any) {
      set({
        availableAgents: [],
        availableAgentsLoading: false,
        availableAgentsError: err.message || 'Failed to load agents',
      })
    }
  },

  workflowMetas: [],
  workflowMetasLoading: false,
  workflowMetasError: null,
  builtinWorkflows: [],

  fetchBuiltinWorkflows: async () => {
    try {
      const builtins = await listBuiltinWorkflows()
      set({ builtinWorkflows: builtins || [] })
    } catch {
      set({ builtinWorkflows: [] })
    }
  },

  installBuiltinWorkflow: async (name) => {
    try {
      await installBuiltinWorkflows([name])
      await get().fetchWorkflowMetas()
      await get().fetchBuiltinWorkflows()
      return true
    } catch {
      return false
    }
  },

  fetchWorkflowMetas: async () => {
    if (inflightMetasLoad) {
      await inflightMetasLoad
      return
    }
    set({ workflowMetasLoading: true, workflowMetasError: null })
    const promise = (async () => {
      try {
        const metas = await listWorkflows()
        set({ workflowMetas: metas, workflowMetasLoading: false })
      } catch (err: any) {
        set({ workflowMetasError: err.message, workflowMetasLoading: false })
      } finally {
        inflightMetasLoad = null
      }
    })()
    inflightMetasLoad = promise
    await promise
  },

  activeWorkflowName: null,
  activeWorkflowYAML: '',
  activeWorkflowGraph: { nodes: [], edges: [] },
  activeWorkflowEntryNodes: [],
  activeWorkflowAgents: {},
  activeWorkflowLoading: false,
  activeWorkflowLoadError: null,
  activeWorkflowParsed: null,
  activeWorkflowValidationError: null,
  dirtySource: null,
  editorMode: 'visual',
  _syncing: false,

  setActiveWorkflow: async (name: string) => {
    set({
      activeWorkflowName: name,
      activeWorkflowValidationError: null,
      activeWorkflowLoadError: null,
      activeWorkflowLoading: true,
      activeWorkflowGraph: { nodes: [], edges: [] },
      activeWorkflowEntryNodes: [],
      activeWorkflowAgents: {},
    })
    try {
      const { yaml } = await getWorkflow(name)
      const parsed = yamlToGraph(yaml)
      if (!parsed) throw new Error('Unable to parse workflow definition')
      set({
        activeWorkflowYAML: yaml,
        activeWorkflowGraph: parsed.graph,
        activeWorkflowEntryNodes: parsed.entryNodes,
        activeWorkflowAgents: parsed.agents,
        dirtySource: null,
        activeWorkflowLoading: false,
      })
    } catch (err: any) {
      set({ activeWorkflowLoadError: err.message || 'Failed to load workflow', activeWorkflowLoading: false })
      throw err
    }
  },

  setEditorMode: (mode) => set({ editorMode: mode }),

  // Graph mutations — from visual mode
  setGraph: (graph) => {
    const state = get()
    if (state._syncing) return
    set({ _syncing: true })

    const normalizedGraph = { nodes: graph.nodes, edges: edgesFromNodes(graph.nodes) }
    const yaml = mergeGraphIntoYAML(
      state.activeWorkflowYAML,
      state.activeWorkflowName || 'untitled',
      normalizedGraph,
      state.activeWorkflowAgents,
      state.activeWorkflowEntryNodes,
    )
    set({
      activeWorkflowGraph: normalizedGraph,
      activeWorkflowYAML: yaml,
      dirtySource: 'visual',
      _syncing: false,
    })
  },

  addNode: (node) => {
    const state = get()
    const graph = { ...state.activeWorkflowGraph, nodes: [...state.activeWorkflowGraph.nodes, node] }
    state.setGraph(graph)
  },

  updateNode: (nodeId, updates) => {
    const state = get()
    const nextID = updates.id?.trim() || nodeId
    if (nextID !== nodeId && state.activeWorkflowGraph.nodes.some(n => n.id === nextID)) return
    const rewriteRef = (value: string) => value === nodeId ? nextID : value
    const nodes = state.activeWorkflowGraph.nodes.map(n => {
      const updated = n.id === nodeId ? { ...n, ...updates, id: nextID } : n
      return {
        ...updated,
        outputs: Object.fromEntries(Object.entries(updated.outputs).map(([outcome, output]) => [outcome, { ...output, to: output.to.map(rewriteRef) }])),
        join: updated.join ? { ...updated.join, from: updated.join.from.map(rewriteRef) } : undefined,
      }
    })
    const edges = state.activeWorkflowGraph.edges.map(e => ({
      ...e,
      source: rewriteRef(e.source),
      target: rewriteRef(e.target),
      id: `${rewriteRef(e.source)}:${e.outcome}:${rewriteRef(e.target)}`,
    }))
    if (nextID !== nodeId) set({ activeWorkflowEntryNodes: state.activeWorkflowEntryNodes.map(rewriteRef) })
    get().setGraph({ nodes, edges })
  },

  removeNode: (nodeId) => {
    const state = get()
    const nodes = state.activeWorkflowGraph.nodes
      .filter(n => n.id !== nodeId)
      .map(n => ({
        ...n,
        outputs: Object.fromEntries(Object.entries(n.outputs).map(([outcome, output]) => [outcome, { ...output, to: output.to.filter(target => target !== nodeId) }])),
        join: n.join ? { ...n.join, from: n.join.from.filter(source => source !== nodeId) } : undefined,
      }))
    const edges = state.activeWorkflowGraph.edges.filter(
      e => e.source !== nodeId && e.target !== nodeId
    )
    set({ activeWorkflowEntryNodes: state.activeWorkflowEntryNodes.filter(entry => entry !== nodeId) })
    get().setGraph({ nodes, edges })
  },

  addEdge: (edge) => {
    const state = get()
    // Update the source node's outputs
    const nodes = state.activeWorkflowGraph.nodes.map(n => {
      if (n.id === edge.source) {
        const outputs = { ...n.outputs }
        const existing = outputs[edge.outcome]
        const to = existing ? [...new Set([...existing.to, edge.target])] : [edge.target]
        outputs[edge.outcome] = {
          to,
          loop: edge.loop || existing?.loop || false,
          max_traversals: edge.maxTraversals || existing?.max_traversals || 0,
        }
        return { ...n, outputs }
      }
      return n
    })
    // Avoid duplicate edges
    const existingEdge = state.activeWorkflowGraph.edges.find(
      e => e.source === edge.source && e.target === edge.target && e.outcome === edge.outcome
    )
    const edges = existingEdge
      ? state.activeWorkflowGraph.edges
      : [...state.activeWorkflowGraph.edges, edge]
    state.setGraph({ nodes, edges })
  },

  updateEdge: (edgeId, updates) => {
    const state = get()
    const edge = state.activeWorkflowGraph.edges.find(e => e.id === edgeId)
    if (!edge) return
    const next = { ...edge, ...updates }
    const nodes = state.activeWorkflowGraph.nodes.map(n => {
      if (n.id !== edge.source) return n
      const outputs = { ...n.outputs }
      const oldOutput = outputs[edge.outcome]
      if (!oldOutput) return n
      const nextOutcome = next.outcome || edge.outcome
      const targets = oldOutput.to.map(target => target === edge.target ? next.target : target)
      delete outputs[edge.outcome]
      outputs[nextOutcome] = { to: targets, loop: next.loop, max_traversals: next.maxTraversals }
      return { ...n, outputs }
    })
    const edges = state.activeWorkflowGraph.edges.map(e => e.id === edgeId ? { ...next, id: `${next.source}:${next.outcome}:${next.target}` } : e)
    state.setGraph({ nodes, edges })
  },

  removeEdge: (edgeId) => {
    const state = get()
    const edge = state.activeWorkflowGraph.edges.find(e => e.id === edgeId)
    if (!edge) return
    const edges = state.activeWorkflowGraph.edges.filter(e => e.id !== edgeId)
    // Also remove from source node's outputs
    const nodes = state.activeWorkflowGraph.nodes.map(n => {
      if (n.id === edge.source && n.outputs[edge.outcome]) {
        const outputs = { ...n.outputs }
        const output = { ...outputs[edge.outcome] }
        output.to = output.to.filter(t => t !== edge.target)
        outputs[edge.outcome] = output
        return { ...n, outputs }
      }
      return n
    })
    state.setGraph({ nodes, edges })
  },

  toggleEntryNode: (nodeId) => {
    const state = get()
    if (!state.activeWorkflowGraph.nodes.some(node => node.id === nodeId)) return
    const entryNodes = state.activeWorkflowEntryNodes.includes(nodeId)
      ? state.activeWorkflowEntryNodes.filter(id => id !== nodeId)
      : [...state.activeWorkflowEntryNodes, nodeId]
    set({ activeWorkflowEntryNodes: entryNodes })
    get().setGraph(state.activeWorkflowGraph)
  },

  ensureAgentReference: (template) => {
    const state = get()
    const existing = Object.entries(state.activeWorkflowAgents)
      .find(([, ref]) => ref.template === template)
    if (existing) return existing[0]

    const baseKey = agentAliasForTemplate(template)
    let key = baseKey
    let suffix = 2
    while (state.activeWorkflowAgents[key] && state.activeWorkflowAgents[key].template !== template) {
      const suffixText = `_${suffix}`
      key = `${baseKey.slice(0, 64 - suffixText.length)}${suffixText}`
      suffix += 1
    }
    set({ activeWorkflowAgents: { ...state.activeWorkflowAgents, [key]: { template } } })
    get().setGraph(get().activeWorkflowGraph)
    return key
  },

  // YAML mutations — from yaml mode
  setYAML: (yaml) => {
    const state = get()
    if (state._syncing) return
    set({ _syncing: true })

    const parsed = yamlToGraph(yaml)
    set({
      activeWorkflowYAML: yaml,
      activeWorkflowGraph: parsed?.graph || state.activeWorkflowGraph,
      activeWorkflowEntryNodes: parsed?.entryNodes || state.activeWorkflowEntryNodes,
      activeWorkflowAgents: parsed?.agents || state.activeWorkflowAgents,
      dirtySource: 'yaml',
      _syncing: false,
    })
  },

  // CRUD
  createWorkflow: async (name, yaml) => {
    try {
      await createWorkflowRequest(name, yaml)
      await get().fetchWorkflowMetas()
      return true
    } catch {
      return false
    }
  },

  updateWorkflow: async (name) => {
    try {
      const state = get()
      const yaml = state.activeWorkflowYAML

      await updateWorkflowRequest(name, yaml)
      await get().fetchWorkflowMetas()
      return true
    } catch {
      return false
    }
  },

  deleteWorkflow: async (name) => {
    try {
      await deleteWorkflowRequest(name)
      await get().fetchWorkflowMetas()
      return true
    } catch {
      return false
    }
  },

  validateWorkflow: async () => {
    const state = get()
    try {
      const data = await validateWorkflowYAML(state.activeWorkflowYAML)
      set({ activeWorkflowValidationError: data.valid ? null : data.error || 'Invalid workflow' })
      return data
    } catch (err: any) {
      set({ activeWorkflowValidationError: err.message || 'Invalid workflow', activeWorkflowParsed: null })
      return { valid: false, error: err.message || 'Invalid workflow' }
    }

  },

  // Runs
  runs: {},
  runsLoading: false,

  fetchRuns: async (workflowName) => {
    set({ runsLoading: true })
    try {
      const data = await listWorkflowRuns(workflowName)
      set((s) => ({ runs: { ...s.runs, [workflowName]: data || [] }, runsLoading: false }))
    } catch {
      set({ runsLoading: false })
    }
  },

  activeRunDetail: null,
  activeRunDetailLoading: false,

  fetchRunDetail: async (workflowName, runId) => {
    set({ activeRunDetailLoading: true })
    try {
      const data = await getWorkflowRun(workflowName, runId)
      set({ activeRunDetail: data, activeRunDetailLoading: false })
    } catch {
      set({ activeRunDetailLoading: false })
    }
  },

  clearActiveRunDetail: () => set({ activeRunDetail: null }),

  startRun: async (workflowName, task) => {
    try {
      const data = await startWorkflowRun(workflowName, task)
      return data.run_id || null
    } catch { /* handled by caller */ }
    return null
  },

  cancelRun: async (workflowName, runId) => {
    await cancelWorkflowRun(workflowName, runId)
  },

  pauseRun: async (workflowName, runId, mode = 'graceful') => {
    await pauseWorkflowRun(workflowName, runId, mode)
  },

  resumeRun: async (workflowName, runId, allowDirty = false) => {
    await resumeWorkflowRun(workflowName, runId, allowDirty)
  },

  restartRun: async (workflowName, runId) => {
    const data = await restartWorkflowRun(workflowName, runId)
    return data.run_id || null
  },

  abandonRun: async (workflowName, runId) => {
    await abandonWorkflowRun(workflowName, runId)
  },

  cleanupRun: async (workflowName, runId, force = false) => {
    await cleanupWorkflowRun(workflowName, runId, force)
  },
}))
