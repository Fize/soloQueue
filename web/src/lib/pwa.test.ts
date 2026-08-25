import { describe, expect, it, vi } from 'vitest'
import {
  captureBeforeInstallPrompt,
  consumeBeforeInstallPrompt,
  isStandaloneDisplayMode,
  registerServiceWorker,
} from '@/lib/pwa'

describe('PWA static contract', () => {
  it('ships a standalone manifest with the app shell metadata', async () => {
    const manifest = await import('../../public/manifest.webmanifest?raw')
    const parsed = JSON.parse(manifest.default) as Record<string, unknown>

    expect(parsed.name).toBe('SoloQueue')
    expect(parsed.short_name).toBe('SoloQueue')
    expect(parsed.start_url).toBe('/')
    expect(parsed.scope).toBe('/')
    expect(parsed.display).toBe('standalone')
    expect(parsed.theme_color).toBeTypeOf('string')
    expect(parsed.background_color).toBeTypeOf('string')
    expect(parsed.icons).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ src: '/logo.png', sizes: '1024x1024', type: 'image/png' }),
      ])
    )
  })

  it('detects Chromium and iOS standalone display modes', () => {
    const matchMedia = (matches: boolean) => vi.fn().mockReturnValue({ matches })
    expect(isStandaloneDisplayMode({ matchMedia, navigator: {} } as never)).toBe(false)
    expect(isStandaloneDisplayMode({ matchMedia: matchMedia(true), navigator: {} } as never)).toBe(true)
    expect(
      isStandaloneDisplayMode({ matchMedia: matchMedia(false), navigator: { standalone: true } } as never)
    ).toBe(true)
  })

  it('registers the root worker and treats registration failure as non-fatal', async () => {
    const registration = {} as ServiceWorkerRegistration
    const register = vi.fn().mockResolvedValue(registration)
    const target = { navigator: { serviceWorker: { register } }, matchMedia: vi.fn() } as never

    await expect(registerServiceWorker(target)).resolves.toBe(registration)
    expect(register).toHaveBeenCalledWith('/sw.js')

    const failingTarget = {
      navigator: { serviceWorker: { register: vi.fn().mockRejectedValue(new Error('blocked')) } },
      matchMedia: vi.fn(),
    } as never
    await expect(registerServiceWorker(failingTarget)).resolves.toBeUndefined()
  })

  it('buffers the one-shot install event for a hook mounted after bootstrap', () => {
    const target = new EventTarget()
    captureBeforeInstallPrompt(target as never)
    const event = new Event('beforeinstallprompt', { cancelable: true })

    target.dispatchEvent(event)

    expect(event.defaultPrevented).toBe(true)
    expect(consumeBeforeInstallPrompt(target as never)).toBe(event)
    expect(consumeBeforeInstallPrompt(target as never)).toBeNull()
  })

  it('keeps the service worker cache boundary explicit', async () => {
    const worker = await import('../../public/sw.js?raw')
    expect(worker.default).toContain("CACHE_NAME = 'soloqueue-shell-v2'")
    expect(worker.default).toContain('function isStaticAssetRequest')
    expect(worker.default).toContain("STATIC_DESTINATIONS")
    expect(worker.default).toContain("request.destination")
    expect(worker.default).toContain("url.pathname.startsWith('/assets/')")
    expect(worker.default).toContain('STATIC_PATHS.has(url.pathname)')
    expect(worker.default).toContain("url.pathname === '/sw.js'")
    expect(worker.default).toContain('APP_SHELL.includes(url.pathname)')
    expect(worker.default).toContain('isCacheableStaticResponse')
    expect(worker.default).toContain('content-type')
    expect(worker.default).not.toContain('STATIC_DESTINATIONS.has(request.destination)')
    expect(worker.default).not.toContain("if (event.request.mode === 'navigate') {")
    expect(worker.default).toContain("request.method !== 'GET'")
    expect(worker.default).toContain("url.pathname === '/healthz'")
    expect(worker.default).toContain("url.pathname === '/api'")
    expect(worker.default).toContain("url.pathname.startsWith('/api/')")
    expect(worker.default).toContain("url.pathname === '/ws'")
    expect(worker.default).toContain('url.origin !== self.location.origin')
    expect(worker.default).toContain("if (!isStaticAssetRequest(request, url))")
    expect(worker.default).toContain("event.request.mode === 'navigate'")
  })

  it('exposes manifest and mobile install metadata from the document head', async () => {
    const index = await import('../../index.html?raw')
    expect(index.default).toContain('rel="manifest" href="/manifest.webmanifest"')
    expect(index.default).toContain('name="theme-color"')
    expect(index.default).toContain('name="mobile-web-app-capable" content="yes"')
    expect(index.default).toContain('name="apple-mobile-web-app-capable" content="yes"')
    expect(index.default).toContain('name="apple-mobile-web-app-status-bar-style"')
    expect(index.default).toContain('name="apple-mobile-web-app-title" content="SoloQueue"')
    expect(index.default).toContain('rel="apple-touch-icon" href="/logo.png"')
  })

  it('registers the worker only for production startup', async () => {
    const main = await import('../../src/main.tsx?raw')
    expect(main.default).toContain('import.meta.env.PROD')
    expect(main.default).toContain('registerServiceWorker')
    expect(main.default.indexOf('captureBeforeInstallPrompt()')).toBeLessThan(
      main.default.indexOf('loadConfig()')
    )
  })
})
