import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { useMCPConfig } from './useMCPConfig'
import * as api from '@/lib/api'

vi.mock('@/lib/api')

beforeEach(() => {
  vi.clearAllMocks()
})

describe('useMCPConfig', () => {
  it('fetches config on mount', async () => {
    const cfg = { mcpServers: {} }
    vi.mocked(api.getMCPConfig).mockResolvedValue(cfg)
    const { result } = renderHook(() => useMCPConfig())
    await waitFor(() => expect(result.current.config).toEqual(cfg))
  })

  it('handles fetch error', async () => {
    vi.mocked(api.getMCPConfig).mockRejectedValue(new Error('fail'))
    const { result } = renderHook(() => useMCPConfig())
    await waitFor(() => expect(result.current.config).toBeNull())
  })

  it('save updates config', async () => {
    const mountCfg = { mcpServers: {} }
    const savedCfg = { mcpServers: { srv: { command: 'npx', args: [] } } }
    vi.mocked(api.getMCPConfig).mockResolvedValue(mountCfg)
    vi.mocked(api.updateMCPConfig).mockResolvedValue(savedCfg)
    const { result } = renderHook(() => useMCPConfig())
    await waitFor(() => expect(result.current.config).toEqual(mountCfg))
    await result.current.save(savedCfg)
    await waitFor(() => expect(result.current.config).toEqual(savedCfg))
  })

  it('save handles error', async () => {
    vi.mocked(api.getMCPConfig).mockResolvedValue({ mcpServers: {} })
    vi.mocked(api.updateMCPConfig).mockRejectedValue(new Error('save fail'))
    const { result } = renderHook(() => useMCPConfig())
    await expect(result.current.save({ mcpServers: {} })).rejects.toThrow('save fail')
    expect(result.current.saving).toBe(false)
  })

  it('refresh fetches config again', async () => {
    const cfg1 = { mcpServers: {} }
    const cfg2 = { mcpServers: { srv: { command: 'npx', args: [] } } }
    vi.mocked(api.getMCPConfig).mockResolvedValueOnce(cfg1).mockResolvedValueOnce(cfg2)
    const { result } = renderHook(() => useMCPConfig())
    await waitFor(() => expect(result.current.config).toEqual(cfg1))
    await result.current.refresh()
    await waitFor(() => expect(result.current.config).toEqual(cfg2))
  })
})
