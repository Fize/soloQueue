import { useState } from 'react'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { getClassifierStatus, runClassifierAction, updateClassifierConfig } from '@/lib/api/config-api'
import type { ClassifierConfig, ClassifierStatus } from '@/types'
import { useUIStore } from '@/stores/uiStore'
import { ClassifierEngineControl } from './ClassifierEngineControl'

vi.mock('@/lib/api/config-api', () => ({
  getClassifierStatus: vi.fn(),
  runClassifierAction: vi.fn(),
  updateClassifierConfig: vi.fn(),
}))

const ready: ClassifierStatus = {
  state: 'ready', model: 'laya-multilingual-fp32', device: 'cpu', backend: 'cpu',
  downloadedBytes: 100, totalBytes: 100,
}

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(getClassifierStatus).mockResolvedValue(ready)
  vi.mocked(runClassifierAction).mockResolvedValue(ready)
  vi.mocked(updateClassifierConfig).mockResolvedValue({ engine: 'laya' })
})
afterEach(() => {
  cleanup()
  useUIStore.setState({ language: 'en' })
  vi.clearAllMocks()
})

const view = (engine: 'llm' | 'laya' = 'laya', onChange = vi.fn()) => (
  <ClassifierEngineControl config={{ engine }} onChange={onChange} />
)

describe('classifier engine control', () => {
  it('reports the worker state once the local engine is selected', async () => {
    render(view())
    expect(await screen.findByText(/ready/)).toBeInTheDocument()
    expect(getClassifierStatus).toHaveBeenCalled()
  })

  it('shows an unknown-size download without a percentage', async () => {
    vi.mocked(getClassifierStatus).mockResolvedValue({ ...ready, state: 'downloading', totalBytes: 0 })
    render(view())
    expect(await screen.findByText(/downloading/)).toBeInTheDocument()
    expect(screen.queryByText(/%/)).not.toBeInTheDocument()
  })

  it('surfaces a status error without hiding the retry control', async () => {
    vi.mocked(getClassifierStatus).mockRejectedValue(new Error('worker missing'))
    render(view())
    expect(await screen.findByText(/worker missing/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument()
  })

  it('does not poll the worker while the LLM engine is selected', () => {
    render(view('llm'))
    expect(getClassifierStatus).not.toHaveBeenCalled()
  })

  it('persists the engine switch immediately', async () => {
    const onChange = vi.fn()
    render(view('llm', onChange))
    fireEvent.click(screen.getByRole('button', { name: 'Local laya model' }))
    await waitFor(() => expect(updateClassifierConfig).toHaveBeenCalledWith({ engine: 'laya' }))
    expect(onChange).toHaveBeenCalledWith({ engine: 'laya' })
  })

  it('refreshes worker status after the optimistic engine switch is persisted', async () => {
    let save!: (config: ClassifierConfig) => void
    vi.mocked(updateClassifierConfig).mockReturnValue(new Promise((resolve) => { save = resolve }))
    vi.mocked(getClassifierStatus)
      .mockResolvedValueOnce({ ...ready, state: 'disabled' })
      .mockResolvedValue(ready)
    function Controlled() {
      const [config, setConfig] = useState<ClassifierConfig>({ engine: 'llm' })
      return <ClassifierEngineControl config={config} onChange={setConfig} />
    }
    render(<Controlled />)
    fireEvent.click(screen.getByRole('button', { name: 'Local laya model' }))
    expect(await screen.findByText(/^disabled/)).toBeInTheDocument()
    await act(async () => { save({ engine: 'laya' }) })
    expect(await screen.findByText(/^ready/)).toBeInTheDocument()
  })

  it('resumes polling after an action instead of freezing on its transient state', async () => {
    vi.mocked(getClassifierStatus)
      .mockResolvedValueOnce({ ...ready, state: 'error', error: 'worker missing' })
      .mockResolvedValue({ ...ready, state: 'ready' })
    vi.mocked(runClassifierAction).mockResolvedValue({ ...ready, state: 'preparing', downloadedBytes: 0, totalBytes: 0 })
    render(view('laya'))
    expect(await screen.findByText(/worker missing/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByText(/^ready/)).toBeInTheDocument()
  })

  it.each(['retry', 'cancel'] as const)('ignores an older status response while %s is pending', async (action) => {
    let finishPoll!: (status: ClassifierStatus) => void
    let finishAction!: (status: ClassifierStatus) => void
    const result: ClassifierStatus = { ...ready, state: action === 'cancel' ? 'disabled' : 'error' }
    vi.mocked(getClassifierStatus)
      .mockReturnValueOnce(new Promise((resolve) => { finishPoll = resolve }))
      .mockResolvedValue(result)
    vi.mocked(runClassifierAction).mockReturnValue(new Promise((resolve) => { finishAction = resolve }))
    render(view('laya'))
    fireEvent.click(screen.getByRole('button', { name: action === 'cancel' ? 'Stop now' : 'Retry' }))
    await act(async () => { finishPoll(ready) })
    expect(screen.queryByText(/^ready/)).not.toBeInTheDocument()
    await act(async () => { finishAction(result) })
    expect(await screen.findByText(new RegExp(`^${result.state}`))).toBeInTheDocument()
  })

  it.each(['retry', 'cancel'] as const)('keeps observing the active worker after %s is rejected', async (action) => {
    let finishPoll!: (status: ClassifierStatus) => void
    vi.mocked(getClassifierStatus)
      .mockResolvedValueOnce({ ...ready, state: 'loading' })
      .mockReturnValueOnce(new Promise((resolve) => { finishPoll = resolve }))
    vi.mocked(runClassifierAction).mockRejectedValue(new Error(`${action} request failed`))
    render(view('laya'))
    expect(await screen.findByText(/^loading/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: action === 'cancel' ? 'Stop now' : 'Retry' }))
    expect(await screen.findByText(new RegExp(`${action} request failed`))).toBeInTheDocument()
    await waitFor(() => expect(getClassifierStatus).toHaveBeenCalledTimes(2))
    await act(async () => { finishPoll(ready) })
    expect(await screen.findByText(/^ready/)).toBeInTheDocument()
    expect(screen.getByText(new RegExp(`${action} request failed`))).toBeInTheDocument()
  })

  it('restores the previous engine when saving fails', async () => {
    const onChange = vi.fn()
    vi.mocked(updateClassifierConfig).mockRejectedValue(new Error('write failed'))
    render(view('llm', onChange))
    fireEvent.click(screen.getByRole('button', { name: 'Local laya model' }))
    await waitFor(() => expect(onChange).toHaveBeenLastCalledWith({ engine: 'llm' }))
  })
})
