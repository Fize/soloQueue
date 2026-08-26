import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { MCPTab } from './MCPTab'

const mocks = vi.hoisted(() => ({
  fetch: vi.fn(),
  save: vi.fn(),
  getMCPPolicies: vi.fn(),
}))

vi.mock('@/stores/mcpConfigStore', () => ({
  useMCPConfigStore: (selector: (state: unknown) => unknown) =>
    selector({
      config: {
        mcpServers: {
          existing: {
            command: 'existing-command',
            args: ['--existing'],
            transport: 'stdio',
            enabled: true,
          },
        },
      },
      fetch: mocks.fetch,
      save: mocks.save,
      saving: false,
    }),
}))

vi.mock('@/lib/api', () => ({
  approveMCPPolicy: vi.fn(),
  getMCPPolicies: mocks.getMCPPolicies,
  revokeMCPPolicy: vi.fn(),
}))

vi.mock('@/lib/i18n', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

describe('MCPTab', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.fetch.mockResolvedValue(undefined)
    mocks.save.mockResolvedValue(undefined)
    mocks.getMCPPolicies.mockResolvedValue({ policies: [] })
  })

  it('adds an expanded server without losing servers loaded from config', () => {
    render(<MCPTab />)

    fireEvent.click(screen.getByRole('button', { name: /mcp\.addServer/ }))

    expect(screen.getByRole('button', { name: /existing/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /mcp\.unnamedServer/ })).toBeInTheDocument()
    expect(screen.getByPlaceholderText('mcp.namePlaceholder')).toBeInTheDocument()
  })

  it('edits a server on the first local mutation', () => {
    render(<MCPTab />)

    fireEvent.click(screen.getByRole('button', { name: /existing/ }))
    fireEvent.change(screen.getByDisplayValue('existing-command'), {
      target: { value: 'updated-command' },
    })

    expect(screen.getByDisplayValue('updated-command')).toBeInTheDocument()
  })

  it('toggles a server on the first local mutation', () => {
    render(<MCPTab />)

    const enabledSwitch = screen.getByRole('switch')
    fireEvent.click(enabledSwitch)

    expect(enabledSwitch).toHaveAttribute('aria-checked', 'false')
  })

  it('removes only the intended server on the first local mutation', () => {
    render(<MCPTab />)

    fireEvent.click(screen.getByRole('button', { name: '' }))

    expect(screen.queryByRole('button', { name: /existing/ })).not.toBeInTheDocument()
    expect(screen.getByText('mcp.noServersDesc')).toBeInTheDocument()
  })

  it('saves the complete configuration after the first edit', async () => {
    render(<MCPTab />)

    fireEvent.click(screen.getByRole('button', { name: /existing/ }))
    fireEvent.change(screen.getByDisplayValue('existing-command'), {
      target: { value: 'updated-command' },
    })
    fireEvent.click(screen.getByRole('button', { name: /mcp\.save$/ }))

    await waitFor(() =>
      expect(mocks.save).toHaveBeenCalledWith({
        mcpServers: {
          existing: {
            command: 'updated-command',
            args: ['--existing'],
            transport: 'stdio',
            enabled: true,
            env: undefined,
          },
        },
      })
    )
  })
})
