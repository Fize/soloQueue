import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { getChatGPTStatus, startChatGPTLogin } from '@/lib/api/config-api'
import type { LLMProvider } from '@/types'

import { LLMSection, normalizeProviderTimeoutMs, validateProviderForm } from './LLMSection'

vi.mock('@/lib/api/config-api', () => ({
  getChatGPTStatus: vi.fn(),
  listChatGPTModels: vi.fn(),
  listProviderRemoteModels: vi.fn(),
  logoutChatGPT: vi.fn(),
  startChatGPTLogin: vi.fn(),
}))

function renderLLMSection(
  onCreateProvider = vi.fn().mockResolvedValue(undefined),
  providers: LLMProvider[] = []
) {
  render(
    <LLMSection
      providers={providers}
      models={[]}
      defaultModels={{ general: '', engineering: '', research: '', classifier: '', fallback: '' }}
      providerFilter="all"
      onProviderFilterChange={vi.fn()}
      onSaveDefaults={vi.fn()}
      onDefaultModelsChange={vi.fn()}
      onCreateProvider={onCreateProvider}
      onUpdateProvider={vi.fn().mockResolvedValue(undefined)}
      onDeleteProvider={vi.fn()}
      onToggleProviderStatus={vi.fn().mockResolvedValue(undefined)}
      onSetProviderAsDefault={vi.fn().mockResolvedValue(undefined)}
      onCreateModel={vi.fn().mockResolvedValue(undefined)}
      onUpdateModel={vi.fn().mockResolvedValue(undefined)}
      onDeleteModel={vi.fn()}
      onToggleModelStatus={vi.fn().mockResolvedValue(undefined)}
    />
  )
  return onCreateProvider
}

afterEach(() => {
  vi.useRealTimers()
  vi.clearAllMocks()
})

describe('normalizeProviderTimeoutMs', () => {
  it('preserves zero as disabled instead of restoring a 30 second cap', () => {
    expect(normalizeProviderTimeoutMs(0)).toBe(0)
    expect(normalizeProviderTimeoutMs(undefined)).toBe(0)
  })

  it('preserves an explicit nonzero legacy cap', () => {
    expect(normalizeProviderTimeoutMs(120_000)).toBe(120_000)
  })
})

describe('validateProviderForm', () => {
  it('rejects an empty provider without needing a backend request', () => {
    const result = validateProviderForm({}, '{}')
    expect(result.fieldErrors).toEqual({
      id: 'Provider ID is required.',
      name: 'Display name is required.',
      baseUrl: 'API base URL is required.',
    })
  })

  it('requires the explicit subscription option for the reserved ChatGPT provider ID', () => {
    expect(validateProviderForm({ id: 'chatgpt', name: 'ChatGPT' }, '{}').fieldErrors.id).toBe(
      'The "chatgpt" ID is reserved for subscription sign-in.'
    )
    expect(
      validateProviderForm({ id: 'chatgpt', name: 'ChatGPT' }, '{}', true).fieldErrors
    ).toEqual({})
  })

  it('rejects malformed or non-object custom headers', () => {
    expect(validateProviderForm({ id: 'demo', name: 'Demo', baseUrl: 'https://example.test' }, '{').fieldErrors.headers)
      .toContain('Headers must be valid JSON object')
    expect(validateProviderForm({ id: 'demo', name: 'Demo', baseUrl: 'https://example.test' }, '[]').fieldErrors.headers)
      .toBe('Headers must be a JSON object')
  })
})

describe('ChatGPT provider creation flow', () => {
  it('lets the user choose subscription sign-in within Add Provider and preserves the regular draft', async () => {
    const onCreateProvider = renderLLMSection()

    fireEvent.click(screen.getByRole('button', { name: 'Add Provider' }))
    fireEvent.change(screen.getByPlaceholderText('e.g. deepseek'), { target: { value: 'custom' } })
    fireEvent.change(screen.getByPlaceholderText('e.g. DeepSeek Official'), {
      target: { value: 'Custom API' },
    })

    const subscriptionOption = screen.getByRole('checkbox', { name: 'Use a ChatGPT subscription' })
    fireEvent.click(subscriptionOption)

    expect(screen.getByDisplayValue('chatgpt')).toBeDisabled()
    expect(screen.getByDisplayValue('ChatGPT')).toBeInTheDocument()
    expect(screen.queryByText('API Base URL')).not.toBeInTheDocument()
    expect(screen.queryByText('Custom Headers (JSON)')).not.toBeInTheDocument()
    expect(
      screen.getByText(
        'Add ChatGPT as a provider using your subscription account. You can sign in after creating it.'
      )
    ).toBeInTheDocument()

    fireEvent.click(subscriptionOption)
    expect(screen.getByDisplayValue('custom')).toBeInTheDocument()
    expect(screen.getByDisplayValue('Custom API')).toBeInTheDocument()

    fireEvent.click(subscriptionOption)
    fireEvent.click(screen.getByRole('button', { name: 'Create Provider' }))

    await waitFor(() => expect(onCreateProvider).toHaveBeenCalledOnce())
    expect(onCreateProvider).toHaveBeenCalledWith(
      expect.objectContaining({
        id: 'chatgpt',
        name: 'ChatGPT',
        baseUrl: 'https://api.openai.com/v1',
        apiKey: '',
        apiKeyEnv: '',
        headers: {},
      })
    )
  })
})

describe('ChatGPT connection status', () => {
  it('polls while connected, clears stale identity on failure, and does not overlap requests', async () => {
    vi.useFakeTimers()
    let rejectSecond: ((reason?: unknown) => void) | undefined
    vi.mocked(getChatGPTStatus)
      .mockResolvedValueOnce({ connected: true, email: 'user@example.test' })
      .mockImplementationOnce(
        () =>
          new Promise((_, reject) => {
            rejectSecond = reject
          })
      )
      .mockResolvedValueOnce({ connected: false })

    renderLLMSection(vi.fn().mockResolvedValue(undefined), [
      {
        id: 'chatgpt',
        name: 'ChatGPT',
        baseUrl: 'https://api.openai.com/v1',
        enabled: true,
        timeoutMs: 0,
        retry: { maxAttempts: 3, initialBackoffMs: 1000, maxBackoffMs: 8000 },
      },
    ])

    await act(async () => {})
    expect(screen.getByText('Connected · user@example.test')).toBeInTheDocument()

    await act(async () => {
      vi.advanceTimersByTime(2500)
    })
    await act(async () => {
      vi.advanceTimersByTime(5000)
    })
    expect(getChatGPTStatus).toHaveBeenCalledTimes(2)

    await act(async () => {
      rejectSecond?.(new Error('offline'))
    })
    expect(screen.getByText('Not connected')).toBeInTheDocument()
    expect(screen.queryByText(/user@example\.test/)).not.toBeInTheDocument()

    await act(async () => {
      vi.advanceTimersByTime(2500)
    })
    await act(async () => {})
    expect(getChatGPTStatus).toHaveBeenCalledTimes(3)
    expect(screen.getByText('Not connected')).toBeInTheDocument()
  })

  it('starts login through the API transport and navigates the popup to the returned authorization URL', async () => {
    vi.mocked(getChatGPTStatus).mockResolvedValue({ connected: false })
    vi.mocked(startChatGPTLogin).mockResolvedValue({
      authorizationUrl: 'https://auth.openai.com/authorize?state=test',
    })
    const popup = { opener: window, location: { href: 'about:blank' }, close: vi.fn() }
    vi.spyOn(window, 'open').mockReturnValue(popup as unknown as Window)

    renderLLMSection(vi.fn().mockResolvedValue(undefined), [
      {
        id: 'chatgpt',
        name: 'ChatGPT',
        baseUrl: 'https://api.openai.com/v1',
        enabled: true,
        timeoutMs: 0,
        retry: { maxAttempts: 3, initialBackoffMs: 1000, maxBackoffMs: 8000 },
      },
    ])
    fireEvent.click(await screen.findByRole('button', { name: 'Sign in with ChatGPT' }))
    await waitFor(() => expect(startChatGPTLogin).toHaveBeenCalledOnce())
    expect(popup.opener).toBeNull()
    expect(popup.location.href).toBe('https://auth.openai.com/authorize?state=test')
  })
})
