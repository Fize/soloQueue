import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { getEmbeddingConfig, getLSPMCPConfig, updateEmbeddingConfig } from '@/lib/api'
import { getEmbeddingStatus } from '@/lib/api/config-api'
import type { EmbeddingConfig } from '@/types'
import { MemoryTab } from './MemoryTab'

vi.mock('@/lib/api', () => ({ getEmbeddingConfig: vi.fn(), getLSPMCPConfig: vi.fn(), updateEmbeddingConfig: vi.fn(), updateLSPMCPConfig: vi.fn() }))
vi.mock('@/lib/api/config-api', () => ({ getEmbeddingStatus: vi.fn(), runEmbeddingAction: vi.fn() }))
vi.mock('./ConfigTab/LSPMCPSection', () => ({ LSPMCPSection: () => null }))
vi.mock('@/components/ui/select', () => ({ Select: ({ label, options, value, onChange }: { label?: string; options: { value: string; label: string }[]; value: string; onChange: (v: string) => void }) => <label>{label}<select aria-label={label} value={value} onChange={e => onChange(e.target.value)}>{options.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}</select></label> }))
const local: EmbeddingConfig = { enabled: true, provider: 'onnx', modelName: '', minSimilarity: 0.65, providers: [], models: [] }
function setup(config = local) {
  vi.mocked(getEmbeddingConfig).mockResolvedValue(config)
  vi.mocked(getLSPMCPConfig).mockResolvedValue({ servers: [] })
  vi.mocked(getEmbeddingStatus).mockResolvedValue({ provider: 'onnx', state: 'downloading', model: 'multilingual-e5-small', device: 'auto', backend: '', dimension: 384, downloadedBytes: 1, totalBytes: 2, indexed: 0, pending: 3, failed: 0 })
  return render(<MemoryTab />)
}
afterEach(() => vi.clearAllMocks())
describe('embedding settings save flow', () => {
  it('saves automatic GPU selection even for a previously persisted CPU configuration without claiming readiness', async () => {
    vi.mocked(updateEmbeddingConfig).mockImplementation(async config => config)
    setup({ ...local, local: { model: 'multilingual-e5-small', device: 'cpu' } })
    await screen.findByLabelText('Local model')
    expect(screen.queryByLabelText('Device preference')).not.toBeInTheDocument()
    expect(screen.queryByRole('option', { name: 'CPU only' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Save Embedding Settings' }))
    await waitFor(() => expect(updateEmbeddingConfig).toHaveBeenCalledWith(expect.objectContaining({ provider: 'onnx', local: { model: 'multilingual-e5-small', device: 'auto' } })))
    expect(await screen.findByText(/State: Downloading/)).toBeInTheDocument()
    expect(screen.queryByText(/State: Ready/)).not.toBeInTheDocument()
    expect(screen.getByLabelText('Local model')).toHaveValue('multilingual-e5-small')
    expect(getEmbeddingConfig).toHaveBeenCalledTimes(1)
  })
  it.each([
    ['off', false, 'onnx'],
    ['none', true, 'none'],
    ['onnx', true, 'onnx'],
    ['openai', true, 'openai'],
  ] as const)('saves and reloads mutually exclusive mode %s', async (mode, enabled, provider) => {
    vi.mocked(updateEmbeddingConfig).mockImplementation(async config => config)
    const view = setup({ ...local, enabled: false })
    const source = await screen.findByLabelText('Long-term memory mode')
    expect(source).toHaveValue('off')
    expect([...source.querySelectorAll('option')].map(o => o.value)).toEqual(['off', 'none', 'onnx', 'openai'])
    expect(screen.queryByRole('switch')).not.toBeInTheDocument()
    fireEvent.change(source, { target: { value: mode } })
    fireEvent.click(screen.getByRole('button', { name: 'Save Embedding Settings' }))
    await waitFor(() => expect(updateEmbeddingConfig).toHaveBeenCalledWith(expect.objectContaining({ enabled, provider })))
    const saved = vi.mocked(updateEmbeddingConfig).mock.calls[0][0]
    view.unmount()
    setup(saved)
    expect(await screen.findByLabelText('Long-term memory mode')).toHaveValue(mode)
  })
  it.each(['onnx', 'openai', 'none', ''])('master off overrides saved provider %s', async provider => {
    setup({ ...local, provider, enabled: false })
    expect(await screen.findByLabelText('Long-term memory mode')).toHaveValue('off')
    expect(screen.queryByLabelText('Local model')).not.toBeInTheDocument()
    expect(screen.queryByRole('switch')).not.toBeInTheDocument()
  })
  it('preserves unsaved input after save failure and permits retry', async () => {
    vi.mocked(updateEmbeddingConfig).mockRejectedValueOnce(new Error('raw error')).mockImplementation(async config => config)
    setup()
    await screen.findByLabelText('Local model')
    fireEvent.change(screen.getByRole('spinbutton'), { target: { value: '0.72' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save Embedding Settings' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Your input is preserved')
    expect(screen.getByRole('spinbutton')).toHaveValue(0.72)
    fireEvent.click(screen.getByRole('button', { name: 'Save Embedding Settings' }))
    await waitFor(() => expect(updateEmbeddingConfig).toHaveBeenCalledTimes(2))
  })
  it('keeps remote configuration and model fields when switching to local and back', async () => {
    const remote = { ...local, provider: 'openai', modelName: 'remote-model', providers: [{ id: 'remote', name: 'Remote', baseUrl: 'https://remote.test', apiKeyEnv: 'KEY', enabled: true }] }
    vi.mocked(updateEmbeddingConfig).mockImplementation(async config => config)
    setup(remote)
    const provider = await screen.findByLabelText('Long-term memory mode')
    expect(screen.getByDisplayValue('remote-model')).toBeInTheDocument()
    fireEvent.change(provider, { target: { value: 'onnx' } })
    expect(screen.queryByDisplayValue('remote-model')).not.toBeInTheDocument()
    fireEvent.change(provider, { target: { value: 'openai' } })
    expect(screen.getByDisplayValue('remote-model')).toBeInTheDocument()
    expect(screen.getByDisplayValue('https://remote.test')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Save Embedding Settings' }))
    await waitFor(() => expect(updateEmbeddingConfig).toHaveBeenCalledWith(remote))
  })
})
