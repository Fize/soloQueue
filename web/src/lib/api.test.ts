import { describe, it, expect, vi, beforeEach } from 'vitest'

import {
  getAgentProfile,
  updateAgentProfile,
  getAgentConfig,
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
