import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import { WecomSection } from './WecomSection'
import type { WecomBotConfig } from '@/types'

const api = vi.hoisted(() => ({ getWecomBotsConfig: vi.fn(), updateWecomBotsConfig: vi.fn(), deleteWecomBotConfig: vi.fn() }))
vi.mock('@/lib/api', () => api)

const bots: WecomBotConfig[] = [{ id: 'a', name: 'Work', botId: 'bot-a', enabled: true, credentialConfigured: true, connected: true, state: 'connected', bind_type: 'l1' }]
beforeEach(() => { vi.resetAllMocks(); api.getWecomBotsConfig.mockResolvedValue(bots) })

it('adds a bot without dropping other accounts and blocks duplicate submission', async () => {
  let resolve!: (value: WecomBotConfig[]) => void
  api.updateWecomBotsConfig.mockImplementation(() => new Promise(done => { resolve = done }))
  render(<WecomSection />)
  const add = await screen.findByRole('button', { name: 'Add WeCom bot' })
  await waitFor(() => expect(add).not.toBeDisabled())
  fireEvent.click(add)
  const dialog = screen.getByRole('dialog')
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Research' } })
  fireEvent.change(within(dialog).getByLabelText('Bot ID'), { target: { value: 'bot-b' } })
  fireEvent.change(within(dialog).getByLabelText('Secret'), { target: { value: 'fake-secret' } })
  const save = within(dialog).getByRole('button', { name: 'Add WeCom bot' })
  fireEvent.click(save); fireEvent.click(save)
  expect(api.updateWecomBotsConfig).toHaveBeenCalledTimes(1)
  expect(api.updateWecomBotsConfig.mock.calls[0][0][0]).toEqual(bots[0])
  expect(api.updateWecomBotsConfig.mock.calls[0][0][1]).toMatchObject({ botId: 'bot-b', secret: 'fake-secret' })
  resolve(bots)
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
})

it('preserves omitted Secret when editing an existing bot', async () => {
  api.updateWecomBotsConfig.mockImplementation(async bots => bots)
  render(<WecomSection />)
  fireEvent.click(await screen.findByRole('button', { name: 'Edit WeCom Work' }))
  const dialog = screen.getByRole('dialog')
  expect(within(dialog).getByLabelText('Secret')).toHaveValue('')
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Renamed' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save changes' }))
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  expect(api.updateWecomBotsConfig.mock.calls[0][0][0]).not.toHaveProperty('secret')
  expect(screen.getByText('Renamed')).toBeInTheDocument()
})

it('requires a new Secret when the bot identity changes', async () => {
  render(<WecomSection />)
  fireEvent.click(await screen.findByRole('button', { name: 'Edit WeCom Work' }))
  const dialog = screen.getByRole('dialog')
  fireEvent.change(within(dialog).getByLabelText('Bot ID'), { target: { value: 'different-bot' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save changes' }))
  expect(await within(dialog).findByRole('alert')).toHaveTextContent('Changing Bot ID requires a new Secret')
  expect(api.updateWecomBotsConfig).not.toHaveBeenCalled()
})

it('restores a failed toggle and prevents concurrent mutations', async () => {
  let reject!: (error: Error) => void
  api.updateWecomBotsConfig.mockImplementation(() => new Promise((_, fail) => { reject = fail }))
  render(<WecomSection />)
  const toggle = await screen.findByRole('switch', { name: 'Enable WeCom Work' })
  fireEvent.click(toggle)
  expect(screen.getByRole('button', { name: 'Add WeCom bot' })).toBeDisabled()
  expect(screen.getByRole('button', { name: 'Remove WeCom Work' })).toBeDisabled()
  reject(new Error('save failed'))
  await waitFor(() => expect(toggle).toHaveAttribute('aria-checked', 'true'))
  await waitFor(() => expect(screen.getByRole('button', { name: 'Add WeCom bot' })).not.toBeDisabled())
})

it('deletes an account after confirmation', async () => {
  api.deleteWecomBotConfig.mockResolvedValue(undefined)
  render(<WecomSection />)
  fireEvent.click(await screen.findByRole('button', { name: 'Remove WeCom Work' }))
  fireEvent.click(screen.getByRole('button', { name: 'Remove', exact: true }))
  await waitFor(() => expect(api.deleteWecomBotConfig).toHaveBeenCalledWith('a'))
  await waitFor(() => expect(screen.queryByText('Work')).not.toBeInTheDocument())
  expect(screen.getByText('No WeCom bots configured')).toBeInTheDocument()
})

it('shows a load failure with a usable refresh action', async () => {
  api.getWecomBotsConfig.mockRejectedValueOnce(new Error('load failed'))
  render(<WecomSection />)
  expect(await screen.findByRole('alert')).toHaveTextContent('load failed')
  fireEvent.click(screen.getByRole('button', { name: 'Refresh WeCom status' }))
  expect(await screen.findByText('Work')).toBeInTheDocument()
})
