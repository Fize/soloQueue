import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ModelPicker } from './ModelPicker'
import type { SessionModelState } from '@/types/chat'

const toastError = vi.fn()
vi.mock('sonner', () => ({ toast: { error: (...args: unknown[]) => toastError(...args), success: vi.fn(), info: vi.fn() } }))

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
      thinking: { enabled: true, reasoningEfforts: ['low', 'medium', 'high', 'xhigh', 'max'] },
      vision: true,
    },
    {
      id: 'solo',
      providerId: 'deepseek',
      name: 'Solo',
      contextWindow: 128000,
      enabled: true,
      generation: {},
      thinking: { enabled: true, reasoningEfforts: ['high'] },
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

const proState: SessionModelState = {
  selection: {
    provider_id: 'deepseek',
    model_id: 'pro',
    reasoning_effort: 'high',
    reasoning_efforts: ['low', 'medium', 'high', 'xhigh', 'max'],
  },
  effective: {
    provider_id: 'deepseek',
    model_id: 'pro',
    name: 'Pro',
    reasoning_effort: 'high',
    reasoning_efforts: ['low', 'medium', 'high', 'xhigh', 'max'],
  },
  source: 'user',
}

const soloState: SessionModelState = {
  selection: {
    provider_id: 'deepseek',
    model_id: 'solo',
    reasoning_efforts: ['high'],
  },
  effective: {
    provider_id: 'deepseek',
    model_id: 'solo',
    name: 'Solo',
    reasoning_efforts: ['high'],
  },
  source: 'user',
}

const proWithoutEffort: SessionModelState = {
  selection: {
    provider_id: 'deepseek',
    model_id: 'pro',
    reasoning_efforts: ['low', 'medium', 'high', 'xhigh', 'max'],
  },
  effective: {
    provider_id: 'deepseek',
    model_id: 'pro',
    name: 'Pro',
    reasoning_efforts: ['low', 'medium', 'high', 'xhigh', 'max'],
  },
  source: 'user',
}

function renderPicker(state: SessionModelState, onSelect = vi.fn(async () => true)) {
  render(<ModelPicker state={state} onSelect={onSelect} />)
  return { onSelect, trigger: screen.getByRole('button', { name: 'Model' }) }
}

describe('ModelPicker', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('shows the effective model for an auto session', () => {
    const { trigger } = renderPicker(autoState)
    expect(trigger).toHaveTextContent('Flash')
    // No effort chosen for an auto session, so no suffix separator.
    expect(trigger.textContent).not.toContain('·')
  })

  it('renders the efforts the model declares, in declaration order', async () => {
    const user = userEvent.setup()
    const { trigger } = renderPicker(proState)
    expect(trigger).toHaveTextContent('Pro')
    expect(trigger).toHaveTextContent('High')

    await user.click(trigger)
    const group = await screen.findByRole('radiogroup')
    const labels = within(group)
      .getAllByRole('radio')
      .map((radio) => radio.textContent)
    expect(labels).toEqual(['Low', 'Medium', 'High', 'XHigh', 'Max'])
    expect(within(group).getByRole('radio', { name: 'High' })).toHaveAttribute('aria-checked', 'true')
  })

  it('renders no effort section until an effort is selected', async () => {
    const user = userEvent.setup()
    const { trigger } = renderPicker(proWithoutEffort)

    await user.click(trigger)
    const group = await screen.findByRole('radiogroup')
    const radios = within(group).getAllByRole('radio')
    expect(radios.map((radio) => radio.textContent)).toEqual(['Low', 'Medium', 'High', 'XHigh', 'Max'])
    expect(radios.every((radio) => radio.getAttribute('aria-checked') === 'false')).toBe(true)
  })

  it('hides the effort section when the session model declares fewer than two efforts', async () => {
    const user = userEvent.setup()
    const { trigger } = renderPicker(soloState)

    await user.click(trigger)
    await screen.findByRole('option', { name: /Solo/ })

    expect(screen.queryByRole('radiogroup')).not.toBeInTheDocument()
    expect(screen.queryByRole('separator')).not.toBeInTheDocument()
  })

  it('hides the effort section for a session model without thinking', async () => {
    const user = userEvent.setup()
    const { trigger } = renderPicker(autoState)

    await user.click(trigger)
    await screen.findByRole('option', { name: /Flash/ })

    expect(screen.queryByRole('radiogroup')).not.toBeInTheDocument()
  })

  it('walks the list with the keyboard and commits the active model', async () => {
    const user = userEvent.setup()
    const { onSelect, trigger } = renderPicker(autoState)

    await user.click(trigger)
    const listbox = await screen.findByRole('listbox')
    await waitFor(() => expect(listbox).toHaveFocus())
    expect(listbox).toHaveAttribute('aria-activedescendant', 'model-option-0')

    await user.keyboard('{ArrowDown}{ArrowDown}')
    expect(listbox).toHaveAttribute('aria-activedescendant', 'model-option-2')

    await user.keyboard('{Enter}')
    expect(onSelect).toHaveBeenCalledWith({
      provider_id: 'deepseek',
      model_id: 'pro',
      reasoning_effort: '',
    })
    // Selecting never closes the panel.
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('supports Home/End and typeahead', async () => {
    const user = userEvent.setup()
    const { trigger } = renderPicker(autoState)

    await user.click(trigger)
    const listbox = await screen.findByRole('listbox')
    await user.keyboard('{End}')
    expect(listbox).toHaveAttribute('aria-activedescendant', 'model-option-3')
    await user.keyboard('{Home}')
    expect(listbox).toHaveAttribute('aria-activedescendant', 'model-option-0')
    await user.keyboard('pr')
    expect(listbox).toHaveAttribute('aria-activedescendant', 'model-option-2')
  })

  it('commits an effort for the session model without touching the model', async () => {
    const user = userEvent.setup()
    const { onSelect, trigger } = renderPicker(proWithoutEffort)

    await user.click(trigger)
    await user.click(within(await screen.findByRole('radiogroup')).getByRole('radio', { name: 'Max' }))

    await waitFor(() =>
      expect(onSelect).toHaveBeenCalledWith({
        provider_id: 'deepseek',
        model_id: 'pro',
        reasoning_effort: 'max',
      })
    )
  })

  it('keeps the panel open and reports a rejected save', async () => {
    const user = userEvent.setup()
    const onSelect = vi.fn(async () => false)
    const { trigger } = renderPicker(proState, onSelect)

    await user.click(trigger)
    await user.click(within(await screen.findByRole('radiogroup')).getByRole('radio', { name: 'Low' }))

    await waitFor(() => expect(toastError).toHaveBeenCalledWith('Could not change the model'))
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.getByText('Could not change the model')).toBeInTheDocument()
    // The trigger keeps the server value instead of the rejected one.
    expect(trigger).toHaveTextContent('High')
  })

  it('shows an empty state when no model is enabled', async () => {
    const user = userEvent.setup()
    const { listModels } = await import('@/lib/api')
    vi.mocked(listModels).mockResolvedValueOnce([])
    const { trigger } = renderPicker(autoState)

    await user.click(trigger)
    expect(await screen.findByText('No enabled models')).toBeInTheDocument()
  })

  it('applies an effort to the session model, not to the hovered row', async () => {
    const user = userEvent.setup()
    const { onSelect, trigger } = renderPicker(proState)

    await user.click(trigger)
    const solo = await screen.findByRole('option', { name: /Solo/ })
    await user.hover(solo)
    await user.click(within(await screen.findByRole('radiogroup')).getByRole('radio', { name: 'Low' }))

    await waitFor(() =>
      expect(onSelect).toHaveBeenCalledWith({
        provider_id: 'deepseek',
        model_id: 'pro',
        reasoning_effort: 'low',
      })
    )
  })

  it('closes when the pointer goes down outside the panel', async () => {
    const user = userEvent.setup()
    const { trigger } = renderPicker(proState)

    await user.click(trigger)
    await screen.findByRole('dialog')

    await user.click(document.body)

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('opens with ⌘⇧K and closes with Escape', async () => {
    const user = userEvent.setup()
    renderPicker(autoState)

    await user.keyboard('{Meta>}{Shift>}k{/Shift}{/Meta}')
    const listbox = await screen.findByRole('listbox')
    await waitFor(() => expect(listbox).toHaveFocus())

    await user.keyboard('{Escape}')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Model' })).toHaveFocus()
  })
})
