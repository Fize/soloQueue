const CACHE_NAME = 'soloqueue-shell-v2'
const APP_SHELL = ['/index.html', '/']
const STATIC_DESTINATIONS = new Set(['script', 'style', 'image', 'font', 'manifest'])
const STATIC_PATHS = new Set(['/manifest.webmanifest', '/favicon.ico', '/logo.png', '/sw.js'])
const STATIC_MIME_TYPES = {
  '/manifest.webmanifest': ['application/manifest+json', 'application/json'],
  '/favicon.ico': ['image/'],
  '/logo.png': ['image/'],
}

function isSameOriginResponse(response) {
  if (!response.url) return false
  return new URL(response.url).origin === self.location.origin
}

function expectedStaticMimeTypes(request, url) {
  if (STATIC_MIME_TYPES[url.pathname]) return STATIC_MIME_TYPES[url.pathname]
  if (url.pathname === '/sw.js') return ['text/javascript', 'application/javascript']
  if (!url.pathname.startsWith('/assets/')) return []

  if (request.destination === 'script') return ['text/javascript', 'application/javascript']
  if (request.destination === 'style') return ['text/css']
  if (request.destination === 'image') return ['image/']
  if (request.destination === 'font') return ['font/', 'application/font']
  if (request.destination === 'manifest') return ['application/manifest+json', 'application/json']

  const extension = url.pathname.split('.').pop()?.toLowerCase()
  if (extension === 'js' || extension === 'mjs') return ['text/javascript', 'application/javascript']
  if (extension === 'css') return ['text/css']
  if (['png', 'jpg', 'jpeg', 'gif', 'svg', 'webp', 'avif', 'ico'].includes(extension)) return ['image/']
  if (['woff', 'woff2', 'ttf', 'otf', 'eot'].includes(extension)) return ['font/', 'application/font']
  return []
}

function isCacheableStaticResponse(request, url, response) {
  if (!response.ok || !isSameOriginResponse(response)) return false
  const contentType = response.headers.get('content-type')?.toLowerCase() || ''
  const expectedTypes = expectedStaticMimeTypes(request, url)
  return expectedTypes.length > 0 && expectedTypes.some((type) => contentType.startsWith(type))
}

function isStaticAssetRequest(request, url) {
  return (
    request.method === 'GET' &&
    url.origin === self.location.origin &&
    url.pathname !== '/healthz' &&
    url.pathname !== '/api' &&
    !url.pathname.startsWith('/api/') &&
    url.pathname !== '/ws' &&
    (url.pathname.startsWith('/assets/') || STATIC_PATHS.has(url.pathname))
  )
}

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => cache.addAll(APP_SHELL)).then(() => self.skipWaiting())
  )
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((key) => key !== CACHE_NAME).map((key) => caches.delete(key))))
      .then(() => self.clients.claim())
  )
})

self.addEventListener('fetch', (event) => {
  const { request } = event
  const url = new URL(request.url)

  // Keep user data, sockets, mutations, and third-party resources out of the cache.
  if (
    request.method !== 'GET' ||
    url.origin !== self.location.origin ||
    url.pathname === '/healthz' ||
    url.pathname === '/api' ||
    url.pathname.startsWith('/api/') ||
    url.pathname === '/ws'
  ) {
    return
  }

  if (event.request.mode === 'navigate' && APP_SHELL.includes(url.pathname)) {
    event.respondWith(
      fetch(request)
        .then((response) => {
          if (
            response.ok &&
            isSameOriginResponse(response) &&
            response.headers.get('content-type')?.toLowerCase().startsWith('text/html')
          ) {
            const copy = response.clone()
            void caches.open(CACHE_NAME).then((cache) => cache.put('/index.html', copy))
          }
          return response
        })
        .catch(() => caches.match('/index.html'))
    )
    return
  }

  if (!isStaticAssetRequest(request, url)) return

  event.respondWith(
    caches.match(request).then((cached) => {
      if (cached) return cached
      return fetch(request).then((response) => {
        if (isCacheableStaticResponse(request, url, response)) {
          const copy = response.clone()
          void caches.open(CACHE_NAME).then((cache) => cache.put(request, copy))
        }
        return response
      })
    })
  )
})
