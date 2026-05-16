import '@testing-library/jest-dom'

const mockFetch = vi.fn()
mockFetch.mockResolvedValue(new Response(JSON.stringify({}), {
  status: 200,
  headers: { 'Content-Type': 'application/json' },
}))
vi.stubGlobal('fetch', mockFetch)

class MockWebSocket {
  static OPEN = 1
  static CONNECTING = 0
  static CLOSED = 3
  readyState = MockWebSocket.CONNECTING
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  onerror: (() => void) | null = null
  constructor(_url: string) {
    ;(globalThis as any).__lastMockWS = this
    setTimeout(() => { this.readyState = MockWebSocket.OPEN; this.onopen?.() }, 0)
  }
  close() { this.readyState = MockWebSocket.CLOSED; this.onclose?.() }
  send(_data: string) {}
}
vi.stubGlobal('WebSocket', MockWebSocket)
vi.stubGlobal('PointerEvent', class PointerEvent extends Event {
  constructor(type: string, props?: any) { super(type, props) }
})
