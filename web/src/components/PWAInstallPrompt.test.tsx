import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { PWAInstallPrompt } from '@/components/PWAInstallPrompt'

describe('PWAInstallPrompt', () => {
  beforeEach(() => {
    localStorage.clear()
    Object.defineProperty(navigator, 'serviceWorker', { configurable: true, value: {} })
    Object.defineProperty(navigator, 'standalone', { configurable: true, value: false })
    window.matchMedia = () => ({ matches: false }) as MediaQueryList
  })

  it('offers an accessible manual guide and persists dismissal', async () => {
    const user = userEvent.setup()
    render(<PWAInstallPrompt />)

    expect(await screen.findByRole('region', { name: /install soloqueue/i })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /install guide/i }))
    expect(screen.getByRole('dialog', { name: /install soloqueue/i })).toBeInTheDocument()
    expect(screen.getByText(/desktop browser/i)).toBeInTheDocument()
    expect(screen.getByText(/mobile browser/i)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /not now/i }))
    await waitFor(() => expect(screen.queryByRole('region', { name: /install soloqueue/i })).not.toBeInTheDocument())
    expect(localStorage.getItem('soloqueue_pwa_install_dismissed')).toBe('true')
  })

  it('uses the native install action when the browser provides it', async () => {
    const user = userEvent.setup()
    const prompt = vi.fn().mockResolvedValue(undefined)
    const event = new Event('beforeinstallprompt') as Event & {
      prompt: typeof prompt
      userChoice: Promise<{ outcome: 'accepted'; platform: string }>
    }
    event.prompt = prompt
    event.userChoice = Promise.resolve({ outcome: 'accepted', platform: 'web' })
    render(<PWAInstallPrompt />)
    window.dispatchEvent(event)

    const button = await screen.findByRole('button', { name: /^install$/i })
    await user.click(button)
    await waitFor(() => expect(screen.queryByRole('region', { name: /install soloqueue/i })).not.toBeInTheDocument())
    expect(prompt).toHaveBeenCalledOnce()
  })
})
