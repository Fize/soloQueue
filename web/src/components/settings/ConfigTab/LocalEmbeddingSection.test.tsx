import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { getEmbeddingStatus, runEmbeddingAction } from '@/lib/api/config-api'
import type { EmbeddingConfig, EmbeddingStatus } from '@/types'
import { useUIStore } from '@/stores/uiStore'
import { LocalEmbeddingSection } from './LocalEmbeddingSection'

vi.mock('@/lib/api/config-api', () => ({ getEmbeddingStatus: vi.fn(), runEmbeddingAction: vi.fn() }))
const config: EmbeddingConfig = { enabled: true, provider: 'onnx', modelName: '', providers: [], models: [], minSimilarity: 0.65, local: { model: 'multilingual-e5-small', device: 'auto' } }
const ready: EmbeddingStatus = { provider: 'onnx', state: 'ready', model: 'multilingual-e5-small', device: 'auto', backend: 'cpu', dimension: 384, downloadedBytes: 100, totalBytes: 100, indexed: 8, pending: 2, failed: 1 }
beforeEach(() => { vi.resetAllMocks(); vi.mocked(getEmbeddingStatus).mockResolvedValue(ready) })
afterEach(() => { cleanup(); useUIStore.setState({ language: 'en' }); vi.clearAllMocks(); vi.useRealTimers() })
const view = (draft = config, saved = config) => <LocalEmbeddingSection config={draft} savedConfig={saved} onChange={vi.fn()} />

describe('local embedding lifecycle', () => {
  it('reports automatic CPU fallback without exposing device controls and preserves memories when rebuilding', async () => {
    vi.mocked(getEmbeddingStatus).mockResolvedValue({ ...ready, fallbackReason: 'GPU unavailable' })
    vi.mocked(runEmbeddingAction).mockResolvedValue({ ...ready, state: 'reindexing' })
    render(view())
    expect(await screen.findByText('Ready', { exact: false })).toBeInTheDocument()
    expect(screen.queryByLabelText('Device preference')).not.toBeInTheDocument()
    expect(screen.queryByRole('option', { name: 'CPU only' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /CPU|GPU/ })).not.toBeInTheDocument()
    expect(screen.getByText(/Actual backend: cpu/)).toBeInTheDocument()
    expect(screen.getByText(/CPU fallback reason: GPU unavailable/)).toBeInTheDocument()
    expect(screen.getByText('Indexed: 8 · Pending: 2 · Failed: 1')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Rebuild local index' }))
    await waitFor(() => expect(runEmbeddingAction).toHaveBeenCalledWith('reindex', expect.objectContaining({ signal: expect.any(AbortSignal) })))
    expect(await screen.findByRole('button', { name: 'Cancel preparation' })).toBeInTheDocument()
  })

  it('shows unknown-size download and cancel then retry states', async () => {
    vi.mocked(getEmbeddingStatus).mockResolvedValue({ ...ready, state: 'downloading', totalBytes: 0 })
    vi.mocked(runEmbeddingAction).mockResolvedValueOnce({ ...ready, state: 'cancelled' }).mockResolvedValueOnce({ ...ready, state: 'loading' })
    render(view())
    expect(await screen.findByText(/100 \/ unknown bytes/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel preparation' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Retry' }))
    await waitFor(() => expect(runEmbeddingAction).toHaveBeenLastCalledWith('retry', expect.anything()))
    expect(await screen.findByText(/Loading model/)).toBeInTheDocument()
  })

  it('recovers status errors and keeps action errors actionable', async () => {
    vi.mocked(getEmbeddingStatus).mockRejectedValueOnce(new Error('raw network secret')).mockResolvedValue(ready)
    vi.mocked(runEmbeddingAction).mockRejectedValue(new Error('raw backend error'))
    render(view())
    expect(await screen.findByRole('alert')).toHaveTextContent('Cannot load embedding status')
    expect(screen.queryByText(/raw network/)).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Rebuild local index' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('The operation failed')
    expect(screen.getByRole('button', { name: 'Rebuild local index' })).toBeEnabled()
  })

  it('shows runtime failures with a bilingual retry path', async () => {
    useUIStore.setState({ language: 'zh' })
    vi.mocked(getEmbeddingStatus).mockResolvedValue({ ...ready, state: 'error', error: 'Worker not installed' })
    vi.mocked(runEmbeddingAction).mockResolvedValue({ ...ready, state: 'preparing' })
    render(view())
    expect(await screen.findByRole('alert')).toHaveTextContent('本地向量检索不可用')
    expect(screen.getByText('Worker not installed')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '重建本地索引' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    expect(await screen.findByText(/状态: 准备中/)).toBeInTheDocument()
  })

  it('aborts requests on persisted config change and unmount and ignores stale responses', async () => {
    vi.useFakeTimers()
    let resolveOld!: (status: EmbeddingStatus) => void
    vi.mocked(getEmbeddingStatus).mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve })).mockResolvedValue({ ...ready, state: 'disabled', provider: 'none' })
    const { rerender, unmount } = render(view())
    const firstSignal = vi.mocked(getEmbeddingStatus).mock.calls[0][0]?.signal
    rerender(view(config, { ...config, enabled: false }))
    expect(firstSignal?.aborted).toBe(true)
    await act(async () => { resolveOld(ready) })
    expect(screen.queryByText(/State: Ready/)).not.toBeInTheDocument()
    const lastSignal = vi.mocked(getEmbeddingStatus).mock.calls.at(-1)?.[0]?.signal
    unmount()
    expect(lastSignal?.aborted).toBe(true)
    await act(async () => { vi.advanceTimersByTime(10000) })
    expect(getEmbeddingStatus).toHaveBeenCalledTimes(2)
  })

  it('does not overlap polling or accept a status request superseded by an action', async () => {
    vi.useFakeTimers()
    let resolvePoll!: (status: EmbeddingStatus) => void
    vi.mocked(getEmbeddingStatus).mockResolvedValueOnce(ready).mockImplementationOnce(() => new Promise(resolve => { resolvePoll = resolve }))
    vi.mocked(runEmbeddingAction).mockResolvedValue({ ...ready, state: 'reindexing' })
    render(view())
    await act(async () => {})
    await act(async () => { vi.advanceTimersByTime(2000) })
    await act(async () => { vi.advanceTimersByTime(6000) })
    expect(getEmbeddingStatus).toHaveBeenCalledTimes(2)
    fireEvent.click(screen.getByRole('button', { name: 'Rebuild local index' }))
    await act(async () => {})
    await act(async () => { resolvePoll(ready) })
    expect(screen.getByText(/Indexing memories/)).toBeInTheDocument()
  })
})
