import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useSessionModel } from './useSessionModel'
import { getSessionModel, setSessionModel } from '@/lib/api'
import { useChatStore } from '@/stores/chatStore'

vi.mock('@/lib/api', () => ({
  getSessionModel: vi.fn(),
  setSessionModel: vi.fn(),
}))

const serverState = {
  selection: { provider_id: 'tencent', model_id: 'kimi-k3', reasoning_efforts: ['low', 'high'] },
  effective: { provider_id: 'tencent', model_id: 'kimi-k3-pro', name: 'Kimi K3.1 Pro' },
  source: 'user' as const,
}

beforeEach(() => {
  vi.clearAllMocks()
  useChatStore.setState({ sessions: [], drafts: [], pendingModel: null, activeSessionId: null })
  vi.mocked(getSessionModel).mockResolvedValue({
    selection: null,
    effective: { provider_id: 'tencent', model_id: 'kimi-k3', name: 'Kimi K3.1 Pro' },
    source: 'default',
  })
})

describe('useSessionModel', () => {
  it('never asks the server for a chat that does not exist yet', async () => {
    const draftId = useChatStore.getState().beginDraft('engineering')

    const draft = renderHook(() => useSessionModel(draftId))
    const welcome = renderHook(() => useSessionModel(undefined))

    await waitFor(() => expect(getSessionModel).not.toHaveBeenCalled())
    expect(draft.result.current.state).toBeNull()
    expect(welcome.result.current.state).toBeNull()
  })

  it('keeps a selection made on the welcome screen for the next chat', async () => {
    const { result } = renderHook(() => useSessionModel(undefined))

    await act(async () => {
      await result.current.select({ provider_id: 'tencent', model_id: 'kimi-k3' })
    })

    expect(setSessionModel).not.toHaveBeenCalled()
    expect(useChatStore.getState().pendingModel).toEqual({
      provider_id: 'tencent',
      model_id: 'kimi-k3',
    })
    expect(result.current.state?.source).toBe('user')
    expect(result.current.state?.effective?.model_id).toBe('kimi-k3')
  })

  it('keeps a placeholder selection on the draft row', async () => {
    const draftId = useChatStore.getState().beginDraft('engineering')

    const { result } = renderHook(() => useSessionModel(draftId))

    await act(async () => {
      await result.current.select({ provider_id: 'tencent', model_id: 'kimi-k3' })
    })

    // The placeholder has no session id to write to; the first message applies it.
    expect(setSessionModel).not.toHaveBeenCalled()
    expect(useChatStore.getState().drafts[0].model).toEqual({
      provider_id: 'tencent',
      model_id: 'kimi-k3',
    })
    // The welcome-screen choice is a different slot and stays untouched.
    expect(useChatStore.getState().pendingModel).toBeNull()
    expect(result.current.state?.effective?.model_id).toBe('kimi-k3')
  })

  it('clears the draft override when auto is selected', async () => {
    const draftId = useChatStore.getState().beginDraft('engineering')
    useChatStore.getState().setDraftModel(draftId, { provider_id: 'tencent', model_id: 'kimi-k3' })

    const { result } = renderHook(() => useSessionModel(draftId))

    expect(result.current.state?.selection?.model_id).toBe('kimi-k3')

    await act(async () => {
      await result.current.select({ provider_id: '', model_id: '' })
    })

    expect(useChatStore.getState().drafts[0].model).toBeUndefined()
    expect(result.current.state).toBeNull()
  })

  it('mirrors the server state for a real session', async () => {
    vi.mocked(getSessionModel).mockResolvedValue(serverState)

    const { result } = renderHook(() => useSessionModel('l2:abc'))

    await waitFor(() => expect(result.current.state?.effective?.name).toBe('Kimi K3.1 Pro'))
    expect(getSessionModel).toHaveBeenCalledWith('l2:abc')

    vi.mocked(setSessionModel).mockResolvedValue(serverState)
    await act(async () => {
      await result.current.select({ provider_id: 'tencent', model_id: 'kimi-k3' })
    })
    expect(setSessionModel).toHaveBeenCalledWith('l2:abc', {
      provider_id: 'tencent',
      model_id: 'kimi-k3',
    })
  })
})
