import { describe, it, expect, beforeEach, vi } from 'vitest'
import {
  agentAliasForTemplate,
  graphToYAML,
  unknownAgentTemplates,
  yamlToGraph,
  defaultYAMLTemplate,
  autoLayoutNodes,
  useWorkflowStore,
} from './workflowStore'
import type { AgentResponse, GraphState } from '@/types'

// ─── Fixtures ───────────────────────────────────────────────────────────

const agents = {
  reviewer: { template: 'reviewer' },
  writer: { template: 'writer', model: 'deepseek-v4-pro' },
  publisher: { template: 'publisher' },
}

let backendWorkflows: Record<string, string> = {}

function makeSimpleGraph(): GraphState {
  return {
    nodes: [
      {
        id: 'review',
        agent: 'reviewer',
        prompt: 'Review the input and provide feedback.',
        outputs: {
          approved: { to: ['write'], loop: false, max_traversals: 0 },
          rejected: { to: [], loop: false, max_traversals: 0 },
        },
        position: { x: 100, y: 200 },
      },
      {
        id: 'write',
        agent: 'writer',
        prompt: 'Write a summary based on the review.\nKeep it concise.',
        outputs: {
          done: { to: ['publish'], loop: false, max_traversals: 0 },
        },
        position: { x: 350, y: 200 },
      },
      {
        id: 'publish',
        agent: 'publisher',
        prompt: 'Publish the final document.',
        outputs: {
          done: { to: [], loop: false, max_traversals: 0 },
        },
        position: { x: 600, y: 200 },
      },
    ],
    edges: [
      {
        id: 'review:approved:write',
        source: 'review',
        target: 'write',
        outcome: 'approved',
        loop: false,
        maxTraversals: 0,
      },
      {
        id: 'write:done:publish',
        source: 'write',
        target: 'publish',
        outcome: 'done',
        loop: false,
        maxTraversals: 0,
      },
    ],
  }
}

// ─── Cleanup ────────────────────────────────────────────────────────────

beforeEach(() => {
  backendWorkflows = {}
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    const method = init?.method || 'GET'
    const nameMatch = url.match(/\/api\/workflows\/([^/]+)\//)
    const name = nameMatch ? decodeURIComponent(nameMatch[1]) : ''
    const body = init?.body ? JSON.parse(String(init.body)) : {}
    const json = (data: unknown, status = 200) => new Response(JSON.stringify(data), { status, headers: { 'Content-Type': 'application/json' } })
    if (url.endsWith('/api/workflows/') && method === 'GET') return json(Object.keys(backendWorkflows).map(name => ({ name, description: '', version: '1', valid: true })))
    if (url.endsWith('/api/workflows/') && method === 'POST') { backendWorkflows[body.name] = body.yaml; return json({ name: body.name, description: '', version: '1', valid: true }, 201) }
    if (url.endsWith('/api/workflows/validate') && method === 'POST') return body.yaml.startsWith('!!') ? json({ error: 'invalid YAML' }, 422) : json({ valid: true })
    if (name && method === 'GET') return backendWorkflows[name] ? json({ name, yaml: backendWorkflows[name], meta: { name, valid: true } }) : json({ error: 'not found' }, 404)
    if (name && method === 'PUT') { backendWorkflows[name] = body.yaml; return json({ name, valid: true }) }
    if (name && method === 'DELETE') { delete backendWorkflows[name]; return new Response(null, { status: 204 }) }
    return json({ error: 'not found' }, 404)
  })

  localStorage.clear()
  useWorkflowStore.setState({
    workflowMetas: [],
    workflowMetasLoading: false,
    workflowMetasError: null,
    availableAgents: [],
    availableAgentsLoading: false,
    availableAgentsError: null,
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
    runs: {},
    runsLoading: false,
    activeRunDetail: null,
    activeRunDetailLoading: false,
  })
})

// ─── graphToYAML ────────────────────────────────────────────────────────

describe('graphToYAML', () => {
  it('produces valid YAML with name and defaults', () => {
    const yaml = graphToYAML('test-wf', makeSimpleGraph(), agents)
    expect(yaml).toContain('name: test-wf')
    expect(yaml).toContain('version: "1"')
    expect(yaml).toContain('node_timeout: 20m')
    expect(yaml).toContain('workflow_timeout: 45m')
  })

  it('includes agents section with model override', () => {
    const yaml = graphToYAML('test-wf', makeSimpleGraph(), agents)
    expect(yaml).toContain('agents:')
    expect(yaml).toContain('reviewer:')
    expect(yaml).toContain('template: reviewer')
    expect(yaml).toContain('model: deepseek-v4-pro')
  })

  it('detects entry nodes (no incoming edges)', () => {
    const yaml = graphToYAML('test-wf', makeSimpleGraph(), agents)
    expect(yaml).toContain('entry:')
    // Only 'review' has no incoming edges
    const entrySection = yaml.split('entry:')[1].split('nodes:')[0]
    expect(entrySection).toContain('review')
    expect(entrySection).not.toContain('write')
    expect(entrySection).not.toContain('publish')
  })

  it('keeps explicitly selected entry nodes even when they have incoming edges', () => {
    const yaml = graphToYAML('test-wf', makeSimpleGraph(), agents, ['write'])
    const entrySection = yaml.split('entry:')[1].split('nodes:')[0]
    expect(entrySection).toContain('write')
    expect(entrySection).not.toContain('review')
  })

  it('serializes all nodes with prompts', () => {
    const yaml = graphToYAML('test-wf', makeSimpleGraph(), agents)
    expect(yaml).toContain('id: review')
    expect(yaml).toContain('agent: reviewer')
    expect(yaml).toContain('Review the input')
    expect(yaml).toContain('id: write')
    expect(yaml).toContain('agent: writer')
  })

  it('serializes multiple outcome outputs', () => {
    const yaml = graphToYAML('test-wf', makeSimpleGraph(), agents)
    expect(yaml).toContain('approved:')
    expect(yaml).toContain('rejected:')
  })

  it('serializes terminal outputs as to: []', () => {
    const yaml = graphToYAML('test-wf', makeSimpleGraph(), agents)
    const lines = yaml.split('\n')
    const toEmpty = lines.filter(l => l.trim() === 'to: []')
    // review.rejected and publish.done are both terminal
    expect(toEmpty.length).toBeGreaterThanOrEqual(2)
  })

  it('persists positions as YAML comments', () => {
    const yaml = graphToYAML('test-wf', makeSimpleGraph(), agents)
    expect(yaml).toContain('# @position: 100,200')
    expect(yaml).toContain('# @position: 350,200')
    expect(yaml).toContain('# @position: 600,200')
  })

  it('handles empty graph', () => {
    const yaml = graphToYAML('empty', { nodes: [], edges: [] }, {})
    expect(yaml).toContain('name: empty')
    expect(yaml).toContain('nodes:')
  })
})

// ─── yamlToGraph ────────────────────────────────────────────────────────

describe('yamlToGraph', () => {
  it('parses flow-style output targets into graph edges', () => {
    const yaml = `name: flow-style
entry: [start]
nodes:
  - id: start
    agent: reviewer
    prompt: Start
    outputs:
      done:
        to: [finish]
  - id: finish
    agent: writer
    prompt: Finish
    outputs:
      done:
        to: []
`

    const result = yamlToGraph(yaml)

    expect(result?.graph.edges).toEqual([{
      id: 'start:done:finish',
      source: 'start',
      target: 'finish',
      outcome: 'done',
      loop: false,
      maxTraversals: 0,
    }])
  })

  it('assigns a readable topology layout when positions are not persisted', () => {
    const yaml = `name: no-positions
entry: [start]
nodes:
  - id: start
    agent: reviewer
    prompt: Start
    outputs:
      done:
        to: [finish]
  - id: finish
    agent: writer
    prompt: Finish
    outputs:
      done:
        to: []
`

    const result = yamlToGraph(yaml)

    expect(result?.graph.nodes.map(node => node.position)).toEqual([
      { x: 120, y: 120 },
      { x: 480, y: 120 },
    ])
  })

  it('round-trips name correctly', () => {
    const yaml = graphToYAML('parse-test', makeSimpleGraph(), agents)
    const result = yamlToGraph(yaml)
    expect(result).not.toBeNull()
    expect(result!.name).toBe('parse-test')
  })

  it('extracts all nodes', () => {
    const yaml = graphToYAML('parse-test', makeSimpleGraph(), agents)
    const result = yamlToGraph(yaml)
    expect(result!.graph.nodes).toHaveLength(3)
    const ids = result!.graph.nodes.map(n => n.id).sort()
    expect(ids).toEqual(['publish', 'review', 'write'])
  })

  it('extracts edges from outputs', () => {
    const yaml = graphToYAML('parse-test', makeSimpleGraph(), agents)
    const result = yamlToGraph(yaml)
    expect(result!.graph.edges.length).toBeGreaterThanOrEqual(2)
    const reviewEdge = result!.graph.edges.find(e => e.source === 'review')!
    expect(reviewEdge).toBeDefined()
    expect(reviewEdge.target).toBe('write')
    expect(reviewEdge.outcome).toBe('approved')
  })

  it('preserves node outputs', () => {
    const yaml = graphToYAML('parse-test', makeSimpleGraph(), agents)
    const result = yamlToGraph(yaml)
    const review = result!.graph.nodes.find(n => n.id === 'review')!
    expect(Object.keys(review.outputs)).toHaveLength(2)
    expect(review.outputs.approved.to).toEqual(['write'])
    expect(review.outputs.rejected.to).toEqual([])
  })

  it('restores positions from comments', () => {
    const yaml = graphToYAML('parse-test', makeSimpleGraph(), agents)
    const result = yamlToGraph(yaml)
    const review = result!.graph.nodes.find(n => n.id === 'review')!
    expect(review.position.x).toBe(100)
    expect(review.position.y).toBe(200)
  })

  it('reads explicit entry nodes from YAML', () => {
    const yaml = graphToYAML('parse-test', makeSimpleGraph(), agents, ['write'])
    expect(yamlToGraph(yaml)!.entryNodes).toEqual(['write'])
  })

  it('handles loop edges', () => {
    const graph: GraphState = {
      nodes: [
        {
          id: 'loop_node',
          agent: 'reviewer',
          prompt: 'Keep reviewing.',
          outputs: {
            again: { to: ['loop_node'], loop: true, max_traversals: 3 },
            done: { to: [], loop: false, max_traversals: 0 },
          },
          position: { x: 200, y: 200 },
        },
      ],
      edges: [
        {
          id: 'loop_node:again:loop_node',
          source: 'loop_node',
          target: 'loop_node',
          outcome: 'again',
          loop: true,
          maxTraversals: 3,
        },
      ],
    }
    const yaml = graphToYAML('loop-test', graph, { reviewer: { template: 'reviewer' } })
    const result = yamlToGraph(yaml)
    const node = result!.graph.nodes.find(n => n.id === 'loop_node')!
    expect(node.outputs.again.loop).toBe(true)
    expect(node.outputs.again.max_traversals).toBe(3)
  })

  it('handles join configuration', () => {
    const graph: GraphState = {
      nodes: [
        {
          id: 'a', agent: 'r1', prompt: 'A',
          outputs: { done: { to: ['c'], loop: false, max_traversals: 0 } },
          position: { x: 100, y: 100 },
        },
        {
          id: 'b', agent: 'r2', prompt: 'B',
          outputs: { done: { to: ['c'], loop: false, max_traversals: 0 } },
          position: { x: 100, y: 300 },
        },
        {
          id: 'c', agent: 'w1', prompt: 'Join',
          join: { mode: 'all', from: ['a', 'b'] },
          outputs: { done: { to: [], loop: false, max_traversals: 0 } },
          position: { x: 400, y: 200 },
        },
      ],
      edges: [
        { id: 'a:done:c', source: 'a', target: 'c', outcome: 'done', loop: false, maxTraversals: 0 },
        { id: 'b:done:c', source: 'b', target: 'c', outcome: 'done', loop: false, maxTraversals: 0 },
      ],
    }
    const yaml = graphToYAML('join-test', graph, { r1: { template: 'r1' }, r2: { template: 'r2' }, w1: { template: 'w1' } })
    expect(yaml).toContain('join:')
    expect(yaml).toContain('mode: all')

    const result = yamlToGraph(yaml)
    const joinNode = result!.graph.nodes.find(n => n.id === 'c')!
    expect(joinNode.join).toBeDefined()
    expect(joinNode.join!.mode).toBe('all')
  })

  it('returns null for invalid YAML', () => {
    const result = yamlToGraph('not: valid: yaml: :: :')
    expect(result).toBeNull()
  })

  it('handles empty yaml string', () => {
    const result = yamlToGraph('')
    // Empty string has no nodes and no name, returns null
    expect(result).toBeNull()
  })
})

describe('autoLayoutNodes', () => {
  it('places a linear workflow left-to-right without node overlap', () => {
    const nodes: GraphState['nodes'] = [
      { id: 'start', agent: 'reviewer', prompt: '', outputs: { done: { to: ['middle'], loop: false, max_traversals: 0 } }, position: { x: 0, y: 0 } },
      { id: 'middle', agent: 'writer', prompt: '', outputs: { done: { to: ['finish'], loop: false, max_traversals: 0 } }, position: { x: 0, y: 0 } },
      { id: 'finish', agent: 'publisher', prompt: '', outputs: { done: { to: [], loop: false, max_traversals: 0 } }, position: { x: 0, y: 0 } },
    ]

    const laidOut = autoLayoutNodes(nodes)

    expect(laidOut.map(node => node.id)).toEqual(['start', 'middle', 'finish'])
    expect(laidOut[0].position.y).toBe(laidOut[1].position.y)
    expect(laidOut[1].position.y).toBe(laidOut[2].position.y)
    expect(laidOut[1].position.x - laidOut[0].position.x).toBeGreaterThanOrEqual(340)
    expect(laidOut[2].position.x - laidOut[1].position.x).toBeGreaterThanOrEqual(340)
  })
})

// ─── Round-trip ─────────────────────────────────────────────────────────

describe('graphToYAML ↔ yamlToGraph round-trip', () => {
  it('preserves node count, edge count, and all outputs', () => {
    const original = makeSimpleGraph()
    const yaml = graphToYAML('roundtrip', original, agents)
    const parsed = yamlToGraph(yaml)

    expect(parsed!.graph.nodes).toHaveLength(original.nodes.length)
    expect(parsed!.graph.edges).toHaveLength(original.edges.length)

    for (const origNode of original.nodes) {
      const parsedNode = parsed!.graph.nodes.find(n => n.id === origNode.id)
      expect(parsedNode).toBeDefined()
      expect(Object.keys(parsedNode!.outputs).sort()).toEqual(
        Object.keys(origNode.outputs).sort()
      )
      for (const outcome of Object.keys(origNode.outputs)) {
        const origOut = origNode.outputs[outcome]
        const parsedOut = parsedNode!.outputs[outcome]
        expect(parsedOut.to.sort()).toEqual(origOut.to.sort())
        expect(parsedOut.loop).toBe(origOut.loop)
      }
    }
  })

  it('preserves agent and prompt for all nodes', () => {
    const original = makeSimpleGraph()
    const yaml = graphToYAML('roundtrip', original, agents)
    const parsed = yamlToGraph(yaml)

    for (const origNode of original.nodes) {
      const parsedNode = parsed!.graph.nodes.find(n => n.id === origNode.id)!
      expect(parsedNode.agent).toBe(origNode.agent)
      // Prompt may have minor whitespace differences
      expect(parsedNode.prompt.replace(/\s+/g, ' ')).toContain(
        origNode.prompt.split('\n')[0].replace(/\s+/g, ' ')
      )
    }
  })

  it('preserves all edges with correct outcome labels', () => {
    const original = makeSimpleGraph()
    const yaml = graphToYAML('edges-rt', original, agents)
    const parsed = yamlToGraph(yaml)
    expect(parsed!.graph.edges).toHaveLength(original.edges.length)

    for (const origEdge of original.edges) {
      const found = parsed!.graph.edges.find(
        e => e.source === origEdge.source && e.target === origEdge.target
      )
      expect(found).toBeDefined()
      expect(found!.outcome).toBe(origEdge.outcome)
    }
  })
})

// ─── defaultYAMLTemplate ────────────────────────────────────────────────

describe('defaultYAMLTemplate', () => {
  it('uses the selected existing agent instead of fabricated sample agents', () => {
    const yaml = defaultYAMLTemplate('my-workflow', 'reviewer')
    expect(yaml).toContain('name: my-workflow')
    expect(yaml).toContain('version: "1"')
    expect(yaml).toContain('reviewer:')
    expect(yaml).toContain('template: "reviewer"')
    expect(yaml).toContain('- start')
    expect(yaml).toContain('id: start')
    expect(yaml).not.toContain('analyzer')
    expect(yaml).not.toContain('writer')
  })

  it('is parseable by yamlToGraph', () => {
    const yaml = defaultYAMLTemplate('parse-me', 'reviewer')
    const result = yamlToGraph(yaml)
    expect(result).not.toBeNull()
    expect(result!.graph.nodes).toHaveLength(1)
    expect(result!.name).toBe('parse-me')
  })

  it('does not invent agents when none is supplied', () => {
    const yaml = defaultYAMLTemplate('empty')
    expect(yaml).toContain('agents: {}')
    expect(yaml).toContain('nodes: []')
    expect(yaml).not.toContain('analyzer')
    expect(yaml).not.toContain('writer')
  })

  it('maps an existing agent ID with spaces to a safe YAML alias', () => {
    expect(agentAliasForTemplate('Andrej Karpathy')).toBe('Andrej_Karpathy')
    const yaml = defaultYAMLTemplate('spaced-agent', 'Andrej Karpathy')
    expect(yaml).toContain('Andrej_Karpathy:')
    expect(yaml).toContain('template: "Andrej Karpathy"')
    expect(yaml).toContain('agent: Andrej_Karpathy')
  })
})

describe('workflow agent references', () => {
  const existingAgent = {
    id: 'reviewer',
    name: 'reviewer',
  } as AgentResponse

  it('reports YAML templates that are not backed by existing agents', () => {
    expect(unknownAgentTemplates(
      { review: { template: 'reviewer' }, write: { template: 'writer' } },
      [existingAgent],
    )).toEqual(['writer'])
  })

  it('adds a YAML agent ref only for the selected existing template', () => {
    useWorkflowStore.setState({
      activeWorkflowName: 'agent-ref',
      activeWorkflowYAML: defaultYAMLTemplate('agent-ref'),
      activeWorkflowAgents: {},
    })
    const ref = useWorkflowStore.getState().ensureAgentReference('reviewer')
    expect(ref).toBe('reviewer')
    expect(useWorkflowStore.getState().activeWorkflowAgents).toEqual({
      reviewer: { template: 'reviewer' },
    })
    expect(useWorkflowStore.getState().activeWorkflowYAML).toContain('template: reviewer')
  })
})

// ─── Store: CRUD operations ─────────────────────────────────────────────

describe('workflowStore CRUD', () => {
  it('creates workflow through the backend API', async () => {
    const store = useWorkflowStore.getState()
    const yaml = defaultYAMLTemplate('test-wf')
    const ok = await store.createWorkflow('test-wf', yaml)
    expect(ok).toBe(true)

    expect(backendWorkflows['test-wf']).toBe(yaml)

    // Check metas updated
    const metas = useWorkflowStore.getState().workflowMetas
    expect(metas.some(m => m.name === 'test-wf')).toBe(true)
  })

  it('updates workflow through the backend API', async () => {
    const store = useWorkflowStore.getState()
    // Set up editor state
    useWorkflowStore.setState({ activeWorkflowYAML: 'name: test-wf\nversion: "1"\nupdated yaml' })
    const ok = await store.updateWorkflow('test-wf')
    expect(ok).toBe(true)

    expect(backendWorkflows['test-wf']).toContain('updated yaml')
  })

  it('deletes workflow through the backend API', async () => {
    // Create first
    const store = useWorkflowStore.getState()
    await store.createWorkflow('to-delete', defaultYAMLTemplate('to-delete'))

    // Delete
    const ok = await store.deleteWorkflow('to-delete')
    expect(ok).toBe(true)

    expect(backendWorkflows['to-delete']).toBeUndefined()
  })

  it('fetchWorkflowMetas reads from the backend', async () => {
    backendWorkflows = {
      'wf-a': defaultYAMLTemplate('wf-a'),
      'wf-b': defaultYAMLTemplate('wf-b'),
    }
    await useWorkflowStore.getState().fetchWorkflowMetas()
    const metas = useWorkflowStore.getState().workflowMetas
    expect(metas).toHaveLength(2)
    const names = metas.map(m => m.name).sort()
    expect(names).toEqual(['wf-a', 'wf-b'])
    expect(metas.every(m => m.valid)).toBe(true)
  })

  it('setActiveWorkflow loads from the backend', async () => {
    const yaml = defaultYAMLTemplate('existing-wf', 'reviewer')
    backendWorkflows['existing-wf'] = yaml

    await useWorkflowStore.getState().setActiveWorkflow('existing-wf')
    const state = useWorkflowStore.getState()
    expect(state.activeWorkflowYAML).toBe(yaml)
    expect(state.activeWorkflowGraph.nodes).toHaveLength(1)
  })

  it('does not invent a missing workflow', async () => {
    await expect(useWorkflowStore.getState().setActiveWorkflow('brand-new')).rejects.toThrow('not found')
    const state = useWorkflowStore.getState()
    expect(state.activeWorkflowLoadError).toContain('not found')
    expect(state.activeWorkflowLoading).toBe(false)
  })

  it('validateWorkflow passes for valid YAML', async () => {
    useWorkflowStore.setState({ activeWorkflowYAML: defaultYAMLTemplate('valid') })
    const result = await useWorkflowStore.getState().validateWorkflow()
    expect(result.valid).toBe(true)
    expect(result.error).toBeUndefined()
  })

  it('validateWorkflow fails for invalid YAML', async () => {
    useWorkflowStore.setState({ activeWorkflowYAML: '!!not valid yaml!!' })
    const result = await useWorkflowStore.getState().validateWorkflow()
    expect(result.valid).toBe(false)
  })
})

// ─── Store: Graph mutations ────────────────────────────────────────────

describe('workflowStore graph mutations', () => {
  beforeEach(() => {
    useWorkflowStore.setState({
      activeWorkflowName: 'test-graph',
      activeWorkflowYAML: 'name: test-graph\nversion: "1"\n',
      activeWorkflowGraph: { nodes: [], edges: [] },
      activeWorkflowAgents: { r1: { template: 'reviewer' } },
    })
  })

  it('addNode adds a node and syncs YAML', () => {
    const node = {
      id: 'n1', agent: 'r1', prompt: 'test',
      outputs: { done: { to: [], loop: false, max_traversals: 0 } },
      position: { x: 100, y: 100 },
    }
    useWorkflowStore.getState().addNode(node)
    const state = useWorkflowStore.getState()
    expect(state.activeWorkflowGraph.nodes).toHaveLength(1)
    expect(state.activeWorkflowGraph.nodes[0].id).toBe('n1')
    expect(state.activeWorkflowYAML).toContain('id: n1')
    expect(state.dirtySource).toBe('visual')
  })

  it('preserves layout comments when visual edits rewrite the YAML document', () => {
    const node = {
      id: 'n1', agent: 'r1', prompt: 'test',
      outputs: { done: { to: [], loop: false, max_traversals: 0 } },
      position: { x: 100, y: 100 },
    }
    useWorkflowStore.setState({
      activeWorkflowGraph: { nodes: [node], edges: [] },
      activeWorkflowEntryNodes: ['n1'],
      activeWorkflowYAML: graphToYAML('test-graph', { nodes: [node], edges: [] }, { r1: { template: 'reviewer' } }, ['n1']),
    })

    useWorkflowStore.getState().setGraph({
      nodes: [{ ...node, position: { x: 320, y: 240 } }],
      edges: [],
    })

    const yaml = useWorkflowStore.getState().activeWorkflowYAML
    expect(yaml).toContain('# @position: 320,240')
    expect(yamlToGraph(yaml)!.graph.nodes[0].position).toEqual({ x: 320, y: 240 })
  })

  it('updates the explicit entry list through the visual inspector mutation', () => {
    const node = {
      id: 'n1', agent: 'r1', prompt: '',
      outputs: { done: { to: [], loop: false, max_traversals: 0 } },
      position: { x: 0, y: 0 },
    }
    useWorkflowStore.setState({
      activeWorkflowGraph: { nodes: [node], edges: [] },
      activeWorkflowEntryNodes: [],
      activeWorkflowYAML: graphToYAML('test-graph', { nodes: [node], edges: [] }, { r1: { template: 'reviewer' } }, []),
    })

    useWorkflowStore.getState().toggleEntryNode('n1')
    expect(useWorkflowStore.getState().activeWorkflowEntryNodes).toEqual(['n1'])
    expect(yamlToGraph(useWorkflowStore.getState().activeWorkflowYAML)!.entryNodes).toEqual(['n1'])
  })

  it('removeNode removes node and its edges', () => {
    // Setup: two nodes with an edge
    const node1 = { id: 'a', agent: 'r1', prompt: '', outputs: { done: { to: ['b'], loop: false, max_traversals: 0 } }, position: { x: 0, y: 0 } }
    const node2 = { id: 'b', agent: 'r1', prompt: '', outputs: { done: { to: [], loop: false, max_traversals: 0 } }, position: { x: 200, y: 0 } }
    const edge = { id: 'a:done:b', source: 'a', target: 'b', outcome: 'done', loop: false, maxTraversals: 0 }
    useWorkflowStore.setState({
      activeWorkflowGraph: { nodes: [node1, node2], edges: [edge] },
    })

    useWorkflowStore.getState().removeNode('a')
    const state = useWorkflowStore.getState()
    expect(state.activeWorkflowGraph.nodes).toHaveLength(1)
    expect(state.activeWorkflowGraph.nodes[0].id).toBe('b')
    expect(state.activeWorkflowGraph.edges).toHaveLength(0)
  })

  it('updateNode modifies node properties', () => {
    const node = { id: 'n1', agent: 'r1', prompt: 'old', outputs: { done: { to: [], loop: false, max_traversals: 0 } }, position: { x: 0, y: 0 } }
    useWorkflowStore.setState({ activeWorkflowGraph: { nodes: [node], edges: [] } })

    useWorkflowStore.getState().updateNode('n1', { prompt: 'new prompt', agent: 'r2' })
    const state = useWorkflowStore.getState()
    expect(state.activeWorkflowGraph.nodes[0].prompt).toBe('new prompt')
    expect(state.activeWorkflowGraph.nodes[0].agent).toBe('r2')
  })

  it('addEdge creates edge and updates source outputs', () => {
    const nodeA = { id: 'a', agent: 'r1', prompt: '', outputs: { done: { to: [], loop: false, max_traversals: 0 } }, position: { x: 0, y: 0 } }
    const nodeB = { id: 'b', agent: 'r1', prompt: '', outputs: {}, position: { x: 200, y: 0 } }
    useWorkflowStore.setState({ activeWorkflowGraph: { nodes: [nodeA, nodeB], edges: [] } })

    useWorkflowStore.getState().addEdge({ id: 'a:done:b', source: 'a', target: 'b', outcome: 'done', loop: false, maxTraversals: 0 })
    const state = useWorkflowStore.getState()
    expect(state.activeWorkflowGraph.edges).toHaveLength(1)
    // Source node's output should now include 'b' as a target
    const sourceNode = state.activeWorkflowGraph.nodes.find(n => n.id === 'a')!
    expect(sourceNode.outputs.done.to).toContain('b')
  })

  it('removeEdge updates source outputs', () => {
    const nodeA = { id: 'a', agent: 'r1', prompt: '', outputs: { done: { to: ['b'], loop: false, max_traversals: 0 } }, position: { x: 0, y: 0 } }
    const nodeB = { id: 'b', agent: 'r1', prompt: '', outputs: {}, position: { x: 200, y: 0 } }
    const edge = { id: 'a:done:b', source: 'a', target: 'b', outcome: 'done', loop: false, maxTraversals: 0 }
    useWorkflowStore.setState({ activeWorkflowGraph: { nodes: [nodeA, nodeB], edges: [edge] } })

    useWorkflowStore.getState().removeEdge('a:done:b')
    const state = useWorkflowStore.getState()
    expect(state.activeWorkflowGraph.edges).toHaveLength(0)
    const sourceNode = state.activeWorkflowGraph.nodes.find(n => n.id === 'a')!
    expect(sourceNode.outputs.done.to).not.toContain('b')
  })

  it('sync guard prevents re-entry when setting graph', () => {
    // If _syncing is true, setGraph should be a no-op
    useWorkflowStore.setState({ _syncing: true })
    const originalYAML = useWorkflowStore.getState().activeWorkflowYAML

    const node = { id: 'x', agent: 'r1', prompt: '', outputs: { done: { to: [], loop: false, max_traversals: 0 } }, position: { x: 0, y: 0 } }
    useWorkflowStore.getState().setGraph({ nodes: [node], edges: [] })

    // YAML should NOT have changed (guard was on)
    expect(useWorkflowStore.getState().activeWorkflowYAML).toBe(originalYAML)
  })

  it('setYAML updates graph from YAML string', () => {
    useWorkflowStore.getState().setYAML(defaultYAMLTemplate('from-yaml', 'reviewer'))
    const state = useWorkflowStore.getState()
    expect(state.activeWorkflowGraph.nodes).toHaveLength(1)
    expect(state.dirtySource).toBe('yaml')
  })
})

// ─── Edge cases ─────────────────────────────────────────────────────────

describe('edge cases', () => {
  it('graphToYAML handles nodes with no outputs', () => {
    const graph: GraphState = {
      nodes: [{ id: 'orphan', agent: 'r1', prompt: '', outputs: {}, position: { x: 0, y: 0 } }],
      edges: [],
    }
    const yaml = graphToYAML('orphan-test', graph, { r1: { template: 'r1' } })
    expect(yaml).toContain('id: orphan')
  })

  it('round-trip preserves empty prompt', () => {
    const graph: GraphState = {
      nodes: [{ id: 'n1', agent: 'r1', prompt: '', outputs: { done: { to: [], loop: false, max_traversals: 0 } }, position: { x: 0, y: 0 } }],
      edges: [],
    }
    const yaml = graphToYAML('empty-prompt', graph, { r1: { template: 'r1' } })
    const result = yamlToGraph(yaml)
    expect(result!.graph.nodes[0].prompt).toBe('')
  })

  it('handles special characters in node IDs', () => {
    const graph: GraphState = {
      nodes: [{ id: 'my-node_v2', agent: 'r1', prompt: '', outputs: { done: { to: [], loop: false, max_traversals: 0 } }, position: { x: 0, y: 0 } }],
      edges: [],
    }
    const yaml = graphToYAML('special', graph, { r1: { template: 'r1' } })
    const result = yamlToGraph(yaml)
    expect(result!.graph.nodes[0].id).toBe('my-node_v2')
  })
})
