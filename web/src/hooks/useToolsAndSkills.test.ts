import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { useTools, useSkills } from './useToolsAndSkills'
import * as api from '@/lib/api'

vi.mock('@/lib/api')

beforeEach(() => {
  vi.clearAllMocks()
})

describe('useTools', () => {
  it('fetches tools on mount', async () => {
    const toolsData = { tools: [{ name: 'Bash', description: '', parameters: null }], total: 1 }
    vi.mocked(api.getTools).mockResolvedValue(toolsData)
    const { result } = renderHook(() => useTools())
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.tools).toEqual(toolsData)
  })

  it('handles fetch error', async () => {
    vi.mocked(api.getTools).mockRejectedValue(new Error('fail'))
    const { result } = renderHook(() => useTools())
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.tools).toBeNull()
  })
})

describe('useSkills', () => {
  it('fetches skills on mount', async () => {
    const skillsData = { skills: [], total: 0 }
    vi.mocked(api.getSkills).mockResolvedValue(skillsData)
    const { result } = renderHook(() => useSkills())
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.skills).toEqual(skillsData)
  })

  it('handles fetch error', async () => {
    vi.mocked(api.getSkills).mockRejectedValue(new Error('fail'))
    const { result } = renderHook(() => useSkills())
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.skills).toBeNull()
  })
})
