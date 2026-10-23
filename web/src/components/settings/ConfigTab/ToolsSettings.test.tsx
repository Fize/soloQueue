import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getToolsConfig, patchToolsConfig } from '@/lib/api'
import type { ImageModelConfig, ToolsConfig } from '@/types'
import { SearchToolsSection } from './SearchToolsSection'
import { ImageModelsSection, validateImageModels } from './ImageModelsSection'

vi.mock('@/lib/api', () => ({ getToolsConfig: vi.fn(), patchToolsConfig: vi.fn() }))
const draft: ImageModelConfig = {
  id: 'one',
  name: 'One',
  provider: 'tencent',
  secretId: '',
  secretIdEnv: 'TEST_ID',
  secretKey: '',
  secretKeyEnv: 'TEST_KEY',
  apiKey: '',
  apiKeyEnv: '',
  apiBaseHost: 'aiart.tencentcloudapi.com',
  region: 'ap-guangzhou',
  enabled: true,
  isDefault: true,
}
beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(getToolsConfig).mockResolvedValue({
    tavilyApiKey: 'placeholder',
    tavilyApiKeyEnv: '',
    webSearchTimeoutMs: 5000,
    imageModels: [draft],
    maxMatches: 999,
  } as ToolsConfig)
  vi.mocked(patchToolsConfig).mockImplementation(async (patch) => patch as ToolsConfig)
})

describe('search settings', () => {
  it('only patches search fields and retains the draft on failure', async () => {
    render(<SearchToolsSection />)
    const key = await screen.findByLabelText('Tavily API Key (optional)')
    fireEvent.change(key, { target: { value: '' } })
    vi.mocked(patchToolsConfig).mockRejectedValueOnce(new Error('Save failed'))
    fireEvent.click(screen.getByRole('button', { name: 'Save', exact: true }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Save failed')
    expect(key).toHaveValue('')
    expect(patchToolsConfig).toHaveBeenCalledWith({
      tavilyApiKey: '',
      tavilyApiKeyEnv: '',
      webSearchTimeoutMs: 5000,
    })
  })
})

describe('image settings', () => {
  it('validates credentials, duplicates and defaults while allowing disabled drafts', () => {
    expect(validateImageModels([draft])).toBeNull()
    expect(
      validateImageModels([
        { ...draft, enabled: false, isDefault: false, secretIdEnv: '', secretKeyEnv: '' },
      ])
    ).toBeNull()
    expect(validateImageModels([draft, draft])).toBe('config.imageDuplicate')
    expect(validateImageModels([{ ...draft, secretKeyEnv: '' }])).toBe(
      'config.imageCredentialsError'
    )
    expect(validateImageModels([{ ...draft, isDefault: false }])).toBe('config.imageDefaultError')
  })
  it('keeps failed edit draft and saves only imageModels', async () => {
    render(<ImageModelsSection />)
    fireEvent.click(await screen.findByRole('button', { name: 'Edit', exact: true }))
    fireEvent.change(screen.getByLabelText('Display name'), { target: { value: 'Updated' } })
    vi.mocked(patchToolsConfig).mockRejectedValueOnce(new Error('Disk unavailable'))
    fireEvent.click(screen.getByRole('button', { name: 'Save', exact: true }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Disk unavailable')
    expect(screen.getByLabelText('Display name')).toHaveValue('Updated')
    expect(patchToolsConfig).toHaveBeenCalledWith({ imageModels: [{ ...draft, name: 'Updated' }] })
    fireEvent.click(screen.getByRole('button', { name: 'Save', exact: true }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(screen.getByText('Updated')).toBeInTheDocument()
  })
  it('requires confirmation before deleting and persists an explicit empty list', async () => {
    render(<ImageModelsSection />)
    fireEvent.click(await screen.findByRole('button', { name: 'Delete', exact: true }))
    expect(patchToolsConfig).not.toHaveBeenCalled()
    const dialog = screen.getByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Confirm' }))
    await waitFor(() => expect(patchToolsConfig).toHaveBeenCalledWith({ imageModels: [] }))
  })
  it('offers replacement or disabling all before disabling an active default', async () => {
    vi.mocked(getToolsConfig).mockResolvedValue({
      imageModels: [draft, { ...draft, id: 'two', name: 'Two', isDefault: false }],
    } as ToolsConfig)
    render(<ImageModelsSection />)
    fireEvent.click(await screen.findByRole('switch', { name: 'Enabled One' }))
    expect(patchToolsConfig).not.toHaveBeenCalled()
    expect(
      screen.getByText(
        'Choose a replacement default, or disable all image generation configurations.'
      )
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Confirm', exact: true })).toBeDisabled()
  })
})

describe('pending settings saves', () => {
  it.each(['success', 'failure'] as const)(
    'locks search inputs until %s and then unlocks',
    async (outcome) => {
      const user = userEvent.setup()
      let resolve!: (value: ToolsConfig) => void
      let reject!: (reason: Error) => void
      vi.mocked(patchToolsConfig).mockImplementationOnce(
        () =>
          new Promise((res, rej) => {
            resolve = res
            reject = rej
          })
      )
      render(<SearchToolsSection />)
      const key = await screen.findByLabelText('Tavily API Key (optional)')
      await user.click(screen.getByRole('button', { name: 'Save', exact: true }))
      expect(key).toBeDisabled()
      for (const input of screen.getAllByRole('textbox')) expect(input).toBeDisabled()
      expect(screen.getByRole('spinbutton')).toBeDisabled()
      await user.type(key, 'later-edit')
      expect(key).toHaveValue('placeholder')
      expect(screen.queryByRole('status')).not.toBeInTheDocument()
      await act(async () => {
        if (outcome === 'success') resolve({} as ToolsConfig)
        else reject(new Error('Pending save failed'))
      })
      expect(key).toBeEnabled()
      expect(screen.getByRole('spinbutton')).toBeEnabled()
      if (outcome === 'success')
        expect(screen.getByRole('status')).toHaveTextContent('Saved successfully')
      else expect(screen.getByRole('alert')).toHaveTextContent('Pending save failed')
      await user.type(key, '-after')
      expect(key).toHaveValue('placeholder-after')
      expect(screen.queryByRole('status')).not.toBeInTheDocument()
      expect(patchToolsConfig).toHaveBeenCalledWith({
        tavilyApiKey: 'placeholder',
        tavilyApiKeyEnv: '',
        webSearchTimeoutMs: 5000,
      })
    }
  )

  it.each(['success', 'failure'] as const)(
    'locks image switches until %s without losing changes',
    async (outcome) => {
      const user = userEvent.setup()
      let resolve!: (value: ToolsConfig) => void
      let reject!: (reason: Error) => void
      vi.mocked(patchToolsConfig).mockImplementationOnce(
        () =>
          new Promise((res, rej) => {
            resolve = res
            reject = rej
          })
      )
      render(<ImageModelsSection />)
      await user.click(await screen.findByRole('button', { name: 'Edit', exact: true }))
      const dialog = screen.getByRole('dialog')
      const enabled = within(dialog).getByRole('switch', { name: 'Enabled', exact: true })
      const defaultSwitch = within(dialog).getByRole('switch', { name: 'Default', exact: true })
      await user.click(within(dialog).getByRole('button', { name: 'Save', exact: true }))
      expect(enabled).toHaveAttribute('aria-disabled', 'true')
      expect(defaultSwitch).toHaveAttribute('aria-disabled', 'true')
      await user.click(enabled)
      await user.click(defaultSwitch)
      expect(enabled).toBeChecked()
      expect(defaultSwitch).toBeChecked()
      await act(async () => {
        if (outcome === 'success') resolve({ imageModels: [draft] } as ToolsConfig)
        else reject(new Error('Pending image save failed'))
      })
      if (outcome === 'success') {
        expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
        await user.click(screen.getByRole('button', { name: 'Edit', exact: true }))
      } else expect(screen.getByRole('alert')).toHaveTextContent('Pending image save failed')
      const reopened = screen.getByRole('dialog')
      expect(
        within(reopened).getByRole('switch', { name: 'Enabled', exact: true })
      ).not.toHaveAttribute('aria-disabled', 'true')
      expect(
        within(reopened).getByRole('switch', { name: 'Default', exact: true })
      ).not.toHaveAttribute('aria-disabled', 'true')
      expect(within(reopened).getByRole('switch', { name: 'Enabled', exact: true })).toBeChecked()
      expect(within(reopened).getByRole('switch', { name: 'Default', exact: true })).toBeChecked()
      expect(patchToolsConfig).toHaveBeenCalledWith({ imageModels: [draft] })
    }
  )

  it('locks the replacement selector while its change is pending and unlocks after failure', async () => {
    const user = userEvent.setup()
    let reject!: (reason: Error) => void
    vi.mocked(patchToolsConfig).mockImplementationOnce(
      () =>
        new Promise((_res, rej) => {
          reject = rej
        })
    )
    vi.mocked(getToolsConfig).mockResolvedValue({
      imageModels: [draft, { ...draft, id: 'two', name: 'Two', isDefault: false }],
    } as ToolsConfig)
    render(<ImageModelsSection />)
    await user.click(await screen.findByRole('switch', { name: 'Enabled One' }))
    const selector = screen.getByRole('combobox')
    await user.click(selector)
    await user.click(await screen.findByRole('option', { name: 'Two', exact: true }))
    await user.click(screen.getByRole('button', { name: 'Confirm', exact: true }))
    expect(selector).toBeDisabled()
    await act(async () => {
      reject(new Error('Replacement failed'))
    })
    expect(selector).toBeEnabled()
    expect(selector).toHaveTextContent('Two')
    expect(screen.getByRole('alert')).toHaveTextContent('Replacement failed')
  })
})
