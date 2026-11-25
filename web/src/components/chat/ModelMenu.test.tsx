import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ModelMenu } from './ModelMenu'
import type { SessionModelState } from '@/types/chat'

vi.mock('@/lib/api', () => ({
  listModels: vi.fn(async () => [
    {
      id: 'flash',
      providerId: 'deepseek',
      name: 'Flash',
      contextWindow: 1048576,
      enabled: true,
      generation: {},
      thinking: { enabled: false },
      vision: false,
    },
    {
      id: 'pro',
      providerId: 'deepseek',
      name: 'Pro',
      contextWindow: 1048576,
      enabled: true,
      generation: {},
      thinking: { enabled: true, reasoningEffort: 'high' },
      vision: true,
    },
    {
      id: 'off',
      providerId: 'deepseek',
      name: 'Disabled',
      contextWindow: 0,
      enabled: false,
      generation: {},
      thinking: { enabled: false },
    },
  ]),
  listProviders: vi.fn(async () => [{ id: 'deepseek', name: 'DeepSeek', enabled: true }]),
}))

const autoState: SessionModelState = {
  selection: null,
  effective: { provider_id: 'deepseek', model_id: 'flash', name: 'Flash' },
  source: 'default',
}

const userState: SessionModelState = {
  selection: { provider_id: 'deepseek', model_id: 'pro', reasoning_effort: 'max' },
  effective: { provider_id: 'deepseek', model_id: 'pro', name: 'Pro', reasoning_effort: 'max' },
  source: 'user',
}

function renderMenu(state: SessionModelState, onSelect = vi.fn()) {
  render(<ModelMenu state={state} onSelect={onSelect} />)
  return onSelect
}

describe('ModelMenu', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('shows the effective model for an auto session', () => {
    renderMenu(autoState)
    expect(screen.getByRole('button', { name: 'Model' })).toHaveTextContent('Flash')
  })

  it('shows the chosen model and its thinking effort', () => {
    renderMenu(userState)
    const pill = screen.getByRole('button', { name: 'Model' })
    expect(pill).toHaveTextContent('Pro')
    expect(pill).toHaveTextContent('Max')
  })

  it('lists enabled models and reports the selection', async () => {
    const user = userEvent.setup()
    const onSelect = renderMenu(autoState)

    await user.click(screen.getByRole('button', { name: 'Model' }))
    await waitFor(() => expect(screen.getByText('Pro')).toBeInTheDocument())

    // Disabled models stay hidden.
    expect(screen.queryByText('Disabled')).not.toBeInTheDocument()

    await user.click(screen.getByText('Pro'))
    expect(onSelect).toHaveBeenCalledWith({
      provider_id: 'deepseek',
      model_id: 'pro',
      reasoning_effort: '',
    })
  })

  it('resets to auto', async () => {
    const user = userEvent.setup()
    const onSelect = renderMenu(userState)

    await user.click(screen.getByRole('button', { name: 'Model' }))
    await waitFor(() => expect(screen.getByText('Auto')).toBeInTheDocument())

    await user.click(screen.getByText('Auto'))
    expect(onSelect).toHaveBeenCalledWith({ provider_id: '', model_id: '', reasoning_effort: '' })
  })

  it('offers thinking effort only for a thinking model', async () => {
    const user = userEvent.setup()
    const onSelect = renderMenu(userState)

    await user.click(screen.getByRole('button', { name: 'Model' }))
    await waitFor(() => expect(screen.getByText('Thinking effort')).toBeInTheDocument())

    await user.click(screen.getByText('Low'))
    expect(onSelect).toHaveBeenCalledWith({
      provider_id: 'deepseek',
      model_id: 'pro',
      reasoning_effort: 'low',
    })
  })
})
