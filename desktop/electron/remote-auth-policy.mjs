export function normalizeRemoteOrigin(value) {
  if (typeof value !== 'string' || !value.trim()) return null

  try {
    const url = new URL(value.trim())
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return null
    if (url.username || url.password) return null
    if (url.pathname !== '/' && url.pathname !== '') return null
    if (url.search || url.hash) return null
    return url.origin
  } catch {
    return null
  }
}

export function shouldInjectRemoteAuth(requestUrl, remoteOrigin) {
  if (!remoteOrigin || typeof requestUrl !== 'string') return false

  try {
    const url = new URL(requestUrl)
    if (url.origin !== remoteOrigin) return false
    return url.pathname === '/healthz' || url.pathname === '/api' || url.pathname.startsWith('/api/')
  } catch {
    return false
  }
}

export function hasAuthorizationHeader(headers = {}) {
  return Object.keys(headers).some((key) => key.toLowerCase() === 'authorization')
}

export function buildBasicAuthHeader(username, password) {
  if (typeof username !== 'string' || typeof password !== 'string' || !username || !password) {
    return null
  }
  return `Basic ${Buffer.from(`${username}:${password}`).toString('base64')}`
}
