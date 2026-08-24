import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { usePWAInstall } from '@/hooks/usePWAInstall'

describe('usePWAInstall', () => {
  beforeEach(() => {
    localStorage.clear()
    window.matchMedia = () => ({ matches: false }) as MediaQueryList
    Object.defineProperty(navigator, 'serviceWorker', { configurable: true, value: {} })
    Object.defineProperty(navigator, 'standalone', { configurable: true, value: false })
  })

  it('reports installed when launched in standalone mode', async () => {
    window.matchMedia = () => ({ matches: true }) as MediaQueryList

    const { result } = renderHook(() => usePWAInstall())

    await waitFor(() => expect(result.current.status).toBe('installed'))
    expect(result.current.install).toBeTypeOf('function')
  })

  it('captures beforeinstallprompt and resolves an accepted native install', async () => {
    const prompt = vi.fn().mockResolvedValue(undefined)
    const beforeInstallPrompt = new Event('beforeinstallprompt') as Event & {
      prompt: typeof prompt
      userChoice: Promise<{ outcome: 'accepted'; platform: string }>
    }
    beforeInstallPrompt.prompt = prompt
    beforeInstallPrompt.userChoice = Promise.resolve({ outcome: 'accepted', platform: 'web' })
    const { result } = renderHook(() => usePWAInstall())

    window.dispatchEvent(beforeInstallPrompt)
    await waitFor(() => expect(result.current.status).toBe('available'))
    await act(async () => {
      await expect(result.current.install()).resolves.toBe(true)
    })

    expect(prompt).toHaveBeenCalledOnce()
    expect(result.current.status).toBe('installed')
  })

  it('returns to manual guidance when the native prompt is dismissed', async () => {
    const event = new Event('beforeinstallprompt') as Event & {
      prompt: () => Promise<void>
      userChoice: Promise<{ outcome: 'dismissed'; platform: string }>
    }
    event.prompt = vi.fn().mockResolvedValue(undefined)
    event.userChoice = Promise.resolve({ outcome: 'dismissed', platform: 'web' })
    const { result } = renderHook(() => usePWAInstall())
    window.dispatchEvent(event)
    await waitFor(() => expect(result.current.status).toBe('available'))

    await act(async () => {
      await expect(result.current.install()).resolves.toBe(false)
    })
    expect(result.current.status).toBe('manual')
  })

  it('hides itself after appinstalled', async () => {
    const { result } = renderHook(() => usePWAInstall())
    await waitFor(() => expect(result.current.status).toBe('manual'))

    window.dispatchEvent(new Event('appinstalled'))
    await waitFor(() => expect(result.current.status).toBe('installed'))
  })

  it('persists dismissal and does not show the prompt on the next mount', async () => {
    const first = renderHook(() => usePWAInstall())
    await waitFor(() => expect(first.result.current.status).toBe('manual'))
    act(() => first.result.current.dismiss())
    expect(first.result.current.status).toBe('dismissed')

    first.unmount()
    const second = renderHook(() => usePWAInstall())
    await waitFor(() => expect(second.result.current.status).toBe('dismissed'))
  })

  it('ignores a later install event after dismissal', async () => {
    const { result } = renderHook(() => usePWAInstall())
    await waitFor(() => expect(result.current.status).toBe('manual'))
    act(() => result.current.dismiss())

    const prompt = vi.fn().mockResolvedValue(undefined)
    const event = new Event('beforeinstallprompt') as Event & {
      prompt: typeof prompt
      userChoice: Promise<{ outcome: 'accepted'; platform: string }>
    }
    event.prompt = prompt
    event.userChoice = Promise.resolve({ outcome: 'accepted', platform: 'web' })
    act(() => window.dispatchEvent(event))

    expect(result.current.status).toBe('dismissed')
    expect(prompt).not.toHaveBeenCalled()
  })

  it('marks browsers without service workers as unsupported', async () => {
    Object.defineProperty(navigator, 'serviceWorker', { configurable: true, value: undefined })
    const { result } = renderHook(() => usePWAInstall())
    await waitFor(() => expect(result.current.status).toBe('unsupported'))
  })

  it('cleans up browser event listeners on unmount', () => {
    const remove = vi.spyOn(window, 'removeEventListener')
    const { unmount } = renderHook(() => usePWAInstall())
    unmount()
    expect(remove).toHaveBeenCalledWith('beforeinstallprompt', expect.any(Function))
    expect(remove).toHaveBeenCalledWith('appinstalled', expect.any(Function))
    remove.mockRestore()
  })

  it('keeps manual guidance available when the native prompt rejects', async () => {
    const event = new Event('beforeinstallprompt') as Event & {
      prompt: () => Promise<void>
      userChoice: Promise<{ outcome: 'accepted'; platform: string }>
    }
    event.prompt = vi.fn().mockRejectedValue(new Error('prompt unavailable'))
    event.userChoice = Promise.resolve({ outcome: 'accepted', platform: 'web' })
    const { result } = renderHook(() => usePWAInstall())
    window.dispatchEvent(event)
    await waitFor(() => expect(result.current.status).toBe('available'))

    await act(async () => {
      await expect(result.current.install()).resolves.toBe(false)
    })
    expect(result.current.status).toBe('manual')
  })
})
