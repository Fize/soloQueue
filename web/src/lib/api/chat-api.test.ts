import { beforeEach, expect, it, vi } from 'vitest'
import { cancelSession } from './chat-api'
import { request } from './core'

vi.mock('./core', () => ({
  request: vi.fn(),
  requestBlob: vi.fn(),
  requestForm: vi.fn(),
  requestRootJson: vi.fn(),
  requestText: vi.fn(),
}))

beforeEach(() => vi.clearAllMocks())

it('posts Stop to the session control endpoint without a source or request selector', async () => {
  vi.mocked(request).mockResolvedValue({ status: 'cancelled' })
  await cancelSession('l1')
  expect(request).toHaveBeenCalledExactlyOnceWith('/session/cancel', {
    method: 'POST',
    body: JSON.stringify({ session_id: 'l1' }),
  })
})

it('propagates a failed Stop acknowledgement', async () => {
  vi.mocked(request).mockRejectedValueOnce(new Error('Stop failed'))
  await expect(cancelSession('l2:session-A')).rejects.toThrow('Stop failed')
})
