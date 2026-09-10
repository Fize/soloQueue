import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import { ChannelsTab } from './ChannelsTab'
import type { TelegramBotConfig } from '@/types'

const api = vi.hoisted(() => ({
  getQQBotsConfig: vi.fn(), getWeChatBotsConfig: vi.fn(), getTelegramBotsConfig: vi.fn(),
  getSpeechConfig: vi.fn(), getSpeechStatus: vi.fn(), updateTelegramBotsConfig: vi.fn(),
  deleteTelegramBotConfig: vi.fn(),
}))
vi.mock('@/lib/api', () => api)
vi.mock('./ConfigTab/QQBotSection', () => ({ QQBotSection: () => null }))
vi.mock('./ConfigTab/SpeechSection', () => ({ SpeechSection: () => null }))
vi.mock('@/lib/i18n', () => ({ useTranslation: () => ({ t: (key: string) => key }) }))

const bots: TelegramBotConfig[] = [
  { id: 'a', name: 'Daily', enabled: true, credentialConfigured: true, connected: false, bind_type: 'l1' },
  { id: 'b', name: 'Research', enabled: true, credentialConfigured: true, connected: false, bind_type: 'l2', bind_agent: 'devs' },
]

beforeEach(() => {
  vi.resetAllMocks()
  api.getQQBotsConfig.mockResolvedValue([])
  api.getWeChatBotsConfig.mockResolvedValue([])
  api.getTelegramBotsConfig.mockResolvedValue(bots)
  api.getSpeechConfig.mockResolvedValue({ enabled: false, model: 'small' })
  api.getSpeechStatus.mockResolvedValue(null)
})

it('blocks other bot mutations during a toggle and restores state on failure', async () => {
  let reject!: (error: Error) => void
  api.updateTelegramBotsConfig.mockImplementation(() => new Promise((_, fail) => { reject = fail }))
  render(<ChannelsTab />)
  const daily = await screen.findByRole('switch', { name: 'Enable Daily' })
  fireEvent.click(daily)
  expect(daily).toHaveAttribute('aria-disabled', 'true')
  const research = screen.getByRole('switch', { name: 'Enable Research' })
  expect(research).toHaveAttribute('aria-disabled', 'true')
  expect(screen.getByRole('button', { name: 'Add bot' })).toBeDisabled()
  expect(screen.getByRole('button', { name: 'Remove Research' })).toBeDisabled()
  fireEvent.click(research)
  expect(api.updateTelegramBotsConfig).toHaveBeenCalledTimes(1)
  reject(new Error('save failed'))
  await waitFor(() => expect(daily).not.toHaveAttribute('aria-disabled', 'true'))
  expect(daily).toHaveAttribute('aria-checked', 'true')
  expect(research).toHaveAttribute('aria-checked', 'true')
})

it('adds a bot without dropping existing bots and prevents duplicate submission', async () => {
  let resolve!: (value: TelegramBotConfig[]) => void
  api.updateTelegramBotsConfig.mockImplementation(() => new Promise((done) => { resolve = done }))
  render(<ChannelsTab />)
  fireEvent.click(await screen.findByRole('button', { name: 'Add bot' }))
  const dialog = screen.getByRole('dialog')
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Coding' } })
  fireEvent.change(within(dialog).getByLabelText('Bot token'), { target: { value: 'fake-token' } })
  const save = within(dialog).getByRole('button', { name: 'Add bot' })
  fireEvent.click(save)
  fireEvent.click(save)
  expect(api.updateTelegramBotsConfig).toHaveBeenCalledTimes(1)
  const sent = api.updateTelegramBotsConfig.mock.calls[0][0]
  expect(sent.slice(0, 2)).toEqual(bots)
  expect(sent[2]).toMatchObject({ name: 'Coding', botToken: 'fake-token', id: '' })
  resolve([...bots, { ...bots[0], id: 'c', name: 'Coding' }])
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  expect(screen.getByText('Coding')).toBeInTheDocument()
  expect(screen.getByText('Research')).toBeInTheDocument()
})

it('preserves the other bot and omits the token when editing a name', async () => {
  api.updateTelegramBotsConfig.mockImplementation(async (value) => value)
  render(<ChannelsTab />)
  const edits = await screen.findAllByRole('button', { name: 'Edit' })
  fireEvent.click(edits[1])
  const dialog = screen.getByRole('dialog')
  expect(within(dialog).getByLabelText('Name')).toHaveValue('Research')
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Research renamed' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save changes' }))
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  const sent = api.updateTelegramBotsConfig.mock.calls[0][0]
  expect(sent[0]).toEqual(bots[0])
  expect(sent[1]).toEqual({ ...bots[1], name: 'Research renamed' })
  expect(sent[1]).not.toHaveProperty('botToken')
})
