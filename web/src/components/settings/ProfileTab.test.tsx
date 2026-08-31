import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ProfileTab } from './ProfileTab'

const mocks = vi.hoisted(() => ({
  getAgentProfile: vi.fn(),
  updateAgentProfile: vi.fn(),
  getQQBotsConfig: vi.fn(),
  getWeChatBotsConfig: vi.fn(),
  listGlobalRules: vi.fn(),
  getGlobalRule: vi.fn(),
  saveGlobalRule: vi.fn(),
  deleteGlobalRule: vi.fn(),
}))

vi.mock('@/lib/api', () => mocks)

vi.mock('@/lib/i18n', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('@/components/ui/confirm-dialog', () => ({
  ConfirmDialog: ({
    open,
    onOpenChange,
    title,
    message,
    onConfirm,
    loading,
  }: {
    open: boolean
    onOpenChange: (open: boolean) => void
    title: string
    message: string
    onConfirm: () => void
    loading?: boolean
  }) =>
    open ? (
      <div role="dialog" aria-label={title}>
        <p>{message}</p>
        <button onClick={onConfirm} disabled={loading}>confirm rule deletion</button>
        <button onClick={() => onOpenChange(false)} disabled={loading}>cancel rule deletion</button>
      </div>
    ) : null,
}))

describe('ProfileTab custom rule deletion', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.getAgentProfile.mockResolvedValue({ soul: '', rules: '' })
    mocks.getQQBotsConfig.mockResolvedValue([])
    mocks.getWeChatBotsConfig.mockResolvedValue([])
    mocks.listGlobalRules.mockResolvedValue([{ filename: 'custom.md', size: 42 }])
  })

  it('uses a custom confirmation, keeps cancel safe, and prevents duplicate deletion while pending', async () => {
    let resolveDelete!: () => void
    mocks.deleteGlobalRule.mockImplementation(
      () => new Promise<void>((resolve) => { resolveDelete = resolve }),
    )
    const nativeConfirm = vi.spyOn(window, 'confirm').mockImplementation(() => {
      throw new Error('native confirm must not be called')
    })
    render(<ProfileTab />)

    const deleteButton = await screen.findByRole('button', { name: 'profile.deleteRule' })
    fireEvent.click(deleteButton)
    expect(screen.getByRole('dialog', { name: 'profile.deleteRule' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'cancel rule deletion' }))
    expect(mocks.deleteGlobalRule).not.toHaveBeenCalled()

    fireEvent.click(deleteButton)
    const confirmButton = screen.getByRole('button', { name: 'confirm rule deletion' })
    fireEvent.click(confirmButton)

    await waitFor(() => expect(confirmButton).toBeDisabled())
    fireEvent.click(confirmButton)
    expect(mocks.deleteGlobalRule).toHaveBeenCalledTimes(1)
    expect(mocks.deleteGlobalRule).toHaveBeenCalledWith('custom.md')
    expect(nativeConfirm).not.toHaveBeenCalled()

    resolveDelete()
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(mocks.listGlobalRules).toHaveBeenCalledTimes(2)
  })
})
