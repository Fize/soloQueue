import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { useConnectionStore } from '@/stores/connectionStore'
import { Sidebar } from './Sidebar'
import { SafetyTab } from './settings/SafetyTab'

vi.mock('./SessionTree', () => ({ SessionTree: () => null }))
vi.mock('@/lib/i18n', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

function CurrentRoute() {
  return <output data-testid="current-route">{useLocation().pathname}</output>
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

it('keeps core navigation and safety settings usable without simulation', async () => {
  useConnectionStore.setState({
    backendReady: true,
    backendStatus: { running: true, pid: null, uptime: 0 },
  })
  const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    const url = String(input)
    const data = url.endsWith('/providers') || url.endsWith('/models') ? [] : {}
    return new Response(JSON.stringify(data), {
      headers: { 'Content-Type': 'application/json' },
    })
  })
  const user = userEvent.setup()
  render(
    <MemoryRouter initialEntries={['/chat']}>
      <Sidebar narrow={false} onToggleCollapse={() => {}} />
      <CurrentRoute />
      <SafetyTab />
    </MemoryRouter>
  )

  for (const [name, path] of [
    ['sidebar.assistant', '/assistant'],
    ['sidebar.scheduledTasks', '/cron'],
    ['sidebar.usageStats', '/stats'],
    ['chat.newChat', '/chat'],
  ]) {
    await user.click(screen.getByRole('button', { name, exact: true }))
    expect(screen.getByTestId('current-route')).toHaveTextContent(path)
  }
  expect.soft(screen.queryByRole('button', { name: 'sidebar.simulations' })).not.toBeInTheDocument()

  await user.click(screen.getByTitle('sidebar.settings'))
  for (const name of [
    'general',
    'connection',
    'projects',
    'models',
    'memory',
    'safety',
    'agents',
    'capabilities',
    'channels',
  ]) {
    expect(screen.getByRole('button', { name: `sidebar.${name}`, exact: true })).toBeInTheDocument()
  }
  await user.click(screen.getByRole('button', { name: 'sidebar.safety', exact: true }))
  expect(screen.getByTestId('current-route')).toHaveTextContent('/settings/safety')
  expect(await screen.findByRole('heading', { name: 'config.toolsTitle' })).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: 'config.sessionConfig' })).toBeInTheDocument()
  expect.soft(screen.queryByRole('heading', { name: 'config.simTitle' })).not.toBeInTheDocument()
  expect.soft(fetchSpy.mock.calls.some(([url]) => String(url).includes('/simulation'))).toBe(false)

  await user.click(screen.getByRole('button', { name: 'config.toolsSave' }))
  await waitFor(() =>
    expect(fetchSpy).toHaveBeenCalledWith(
      '/api/config/tools',
      expect.objectContaining({ method: 'PUT' })
    )
  )
  await screen.findByRole('button', { name: 'config.sessionSave' })
  await user.click(screen.getByRole('button', { name: 'config.sessionSave' }))
  await waitFor(() =>
    expect(fetchSpy).toHaveBeenCalledWith(
      '/api/config/session',
      expect.objectContaining({ method: 'PUT' })
    )
  )
  await screen.findByRole('heading', { name: 'config.sessionConfig' })
  expect([...new Set(fetchSpy.mock.calls.map(([url]) => String(url)))].sort()).toEqual([
    '/api/config/session',
    '/api/config/tools',
  ])
})
