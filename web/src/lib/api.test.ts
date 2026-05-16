import { describe, it, expect, vi, beforeEach } from 'vitest'

// Must import after fetch is mocked in setup
import {
  listPlans,
  getPlan,
  createPlan,
  updatePlan,
  deletePlan,
  updatePlanStatus,
  toggleTodo,
  deleteTodo,
  getAgentProfile,
  updateAgentProfile,
  getAgentConfig,
  updateAgentConfig,
  getTeams,
  getConfig,
  getConfigToml,
  getTools,
  getSkills,
  getMCPConfig,
  updateMCPConfig,
  getFileUrl,
  listFiles,
  getFileRoots,
} from './api'

beforeEach(() => {
  vi.mocked(fetch).mockClear()
})

function mockResponse(data: unknown, status = 200) {
  vi.mocked(fetch).mockResolvedValueOnce(
    new Response(JSON.stringify(data), {
      status,
      headers: { 'Content-Type': 'application/json' },
    })
  )
}

function mockTextResponse(text: string, status = 200) {
  vi.mocked(fetch).mockResolvedValueOnce(new Response(text, { status }))
}

describe('api', () => {
  describe('plans', () => {
    it('listPlans', async () => {
      mockResponse({ plans: [{ id: 'p1', title: 'Test' }], total: 1 })
      const plans = await listPlans()
      expect(fetch).toHaveBeenCalledWith('/api/plans', expect.any(Object))
      expect(plans).toHaveLength(1)
      expect(plans[0].id).toBe('p1')
    })

    it('listPlans returns empty array when null', async () => {
      mockResponse({ plans: null, total: 0 })
      const plans = await listPlans()
      expect(plans).toEqual([])
    })

    it('getPlan', async () => {
      mockResponse({ id: 'p1', title: 'Detail' })
      const plan = await getPlan('p1')
      expect(fetch).toHaveBeenCalledWith('/api/plans/p1', expect.any(Object))
      expect(plan.title).toBe('Detail')
    })

    it('createPlan', async () => {
      mockResponse({ id: 'p2', title: 'New' })
      const plan = await createPlan({ title: 'New' })
      expect(fetch).toHaveBeenCalledWith(
        '/api/plans',
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ title: 'New' }),
        })
      )
    })

    it('updatePlan', async () => {
      mockResponse({ id: 'p1', title: 'Updated' })
      await updatePlan('p1', { title: 'Updated' })
      expect(fetch).toHaveBeenCalledWith(
        '/api/plans/p1',
        expect.objectContaining({
          method: 'PUT',
          body: JSON.stringify({ title: 'Updated' }),
        })
      )
    })

    it('deletePlan', async () => {
      mockResponse({ deleted: 'p1' })
      await deletePlan('p1')
      expect(fetch).toHaveBeenCalledWith(
        '/api/plans/p1',
        expect.objectContaining({
          method: 'DELETE',
        })
      )
    })

    it('updatePlanStatus', async () => {
      mockResponse({ id: 'p1', status: 'running' })
      await updatePlanStatus('p1', 'running')
      expect(fetch).toHaveBeenCalledWith(
        '/api/plans/p1/status',
        expect.objectContaining({
          method: 'PATCH',
          body: JSON.stringify({ status: 'running' }),
        })
      )
    })

    it('handles error response', async () => {
      mockResponse({ error: 'not found' }, 404)
      await expect(getPlan('unknown')).rejects.toThrow('not found')
    })
  })

  describe('todos', () => {
    it('toggleTodo', async () => {
      mockResponse({ id: 't1', completed: true })
      const result = await toggleTodo('p1', 't1')
      expect(result.completed).toBe(true)
      expect(fetch).toHaveBeenCalledWith(
        '/api/plans/p1/todos/t1/toggle',
        expect.objectContaining({ method: 'PATCH' })
      )
    })

    it('deleteTodo', async () => {
      mockResponse({})
      await deleteTodo('p1', 't1')
      expect(fetch).toHaveBeenCalledWith(
        '/api/plans/p1/todos/t1',
        expect.objectContaining({ method: 'DELETE' })
      )
    })
  })

  describe('agents', () => {
    it('getAgentProfile', async () => {
      mockResponse({ soul: 'soul', rules: 'rules' })
      const profile = await getAgentProfile('main')
      expect(fetch).toHaveBeenCalledWith('/api/agents/main/profile', expect.any(Object))
      expect(profile.soul).toBe('soul')
    })

    it('updateAgentProfile', async () => {
      mockResponse({ soul: 'new', rules: '' })
      await updateAgentProfile('main', { soul: 'new' })
      expect(fetch).toHaveBeenCalledWith(
        '/api/agents/main/profile',
        expect.objectContaining({ method: 'PUT' })
      )
    })

    it('getAgentConfig', async () => {
      mockResponse({
        raw_config: '',
        system_prompt: '',
        name: '',
        description: '',
        model: '',
        group: '',
        is_leader: false,
        mcp_servers: [],
      })
      await getAgentConfig('main')
      expect(fetch).toHaveBeenCalledWith('/api/agents/main/config', expect.any(Object))
    })

    it('getTeams', async () => {
      mockResponse({ teams: [] })
      const result = await getTeams()
      expect(result.teams).toEqual([])
    })
  })

  describe('config', () => {
    it('getConfig', async () => {
      mockResponse({ session: {} })
      await getConfig()
      expect(fetch).toHaveBeenCalledWith('/api/config', expect.any(Object))
    })

    it('getConfigToml', async () => {
      mockTextResponse('[server]\nport = 8765')
      const toml = await getConfigToml()
      expect(toml).toContain('port')
    })
  })

  describe('tools & skills', () => {
    it('getTools', async () => {
      mockResponse({ tools: [], total: 0 })
      await getTools()
      expect(fetch).toHaveBeenCalledWith('/api/tools', expect.any(Object))
    })

    it('getSkills', async () => {
      mockResponse({ skills: [], total: 0 })
      await getSkills()
      expect(fetch).toHaveBeenCalledWith('/api/skills', expect.any(Object))
    })
  })

  describe('mcp', () => {
    it('getMCPConfig', async () => {
      mockResponse({ mcpServers: {} })
      const cfg = await getMCPConfig()
      expect(cfg.mcpServers).toEqual({})
    })

    it('updateMCPConfig', async () => {
      mockResponse({ mcpServers: { srv: { command: 'npx', args: [] } } })
      await updateMCPConfig({ mcpServers: { srv: { command: 'npx', args: [] } } })
      expect(fetch).toHaveBeenCalledWith('/api/mcp', expect.objectContaining({ method: 'PATCH' }))
    })
  })

  describe('files', () => {
    it('getFileUrl', () => {
      const url = getFileUrl('/path/to/file.ts')
      expect(url).toBe('/api/files/content?path=%2Fpath%2Fto%2Ffile.ts')
    })

    it('listFiles', async () => {
      mockResponse([
        { name: 'a.ts', path: '/a.ts', size: 10, isDir: false, ext: '.ts', modTime: '' },
      ])
      const files = await listFiles('/dir')
      expect(fetch).toHaveBeenCalledWith('/api/files/list?dir=%2Fdir', expect.any(Object))
      expect(files).toHaveLength(1)
    })

    it('getFileRoots', async () => {
      mockResponse([{ label: 'root', path: '/', group: '' }])
      const roots = await getFileRoots()
      expect(roots).toHaveLength(1)
    })
  })
})
