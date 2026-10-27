import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import { StorageTab } from './StorageTab'
import { StoragePreviewDialog } from './StoragePreviewDialog'
import { tightensStoragePolicy, validateStoragePolicy } from './StoragePolicyFields'
import type { StorageSnapshot, StoragePreview } from '@/lib/api/storage-api'
const api = vi.hoisted(() => ({ getStorage: vi.fn(), updateStorage: vi.fn(), previewStorage: vi.fn(), cleanupStorage: vi.fn() }))
vi.mock('@/lib/api/storage-api', async () => ({ ...await vi.importActual('@/lib/api/storage-api'), ...api }))
vi.mock('@/lib/i18n', () => ({ useTranslation: () => ({ t: (key: string) => key }) }))
const snapshot: StorageSnapshot = {
  policy: {
    runtime_logs: { max_file_mb: 50, retention_days: 15, max_files: 30, max_total_mb: 1024, http_override: false, http_max_file_mb: 50, http_retention_days: 15, http_max_files: 30 },
    session_history: { max_file_mb: 50, retention_days: 0, max_files: 0, max_total_mb: 0, warning_mb: 1024 },
    execution_details: { max_file_mb: 50, retention_days: 30, max_files: 0, max_total_mb: 1024, max_turn_mb: 50 },
    cron_history: { max_file_mb: 50, retention_days: 30, max_files: 0, max_total_mb: 512, max_per_task: 0 },
  }, revision: 'rev1', restart_required: false,
  usage: { sampled_at: '2026-10-26T08:00:00Z', total_bytes: 2048, file_bytes: 1024, database_bytes: 1024, categories: [{ id: 'runtime_logs', bytes: 1024, files: 2 }, { id: 'legacy', bytes: 0, files: 0 }], errors: [] },
  last_cleanup: { deleted_files: 1, freed_bytes: 20, errors: [] }, next_cleanup_at: '2026-10-26T09:00:00Z',
}
const preview: StoragePreview = { preview_id: 'token', revision: 'rev1', expires_at: '2099-01-01T00:00:00Z', files: 2, bytes: 128, categories: [{ id: 'runtime_logs', files: 2, bytes: 128 }], protected_files: 3, errors: [], policy: snapshot.policy }
beforeEach(() => { vi.resetAllMocks(); api.getStorage.mockResolvedValue(structuredClone(snapshot)); api.previewStorage.mockResolvedValue(structuredClone(preview)); api.updateStorage.mockResolvedValue(snapshot); api.cleanupStorage.mockResolvedValue({ deleted_files: 2, freed_bytes: 128, errors: [] }) })
it('loads usage and policies with advanced limits collapsed', async () => {
  render(<StorageTab />)
  expect(await screen.findByRole('heading', { name: 'storage.title' })).toBeInTheDocument()
  expect(screen.getAllByText('storage.runtime_logs').length).toBeGreaterThan(0)
  expect(screen.getByText(/2.0 KiB/)).toBeInTheDocument()
  const history = screen.getByRole('region', { name: 'storage.session_history' })
  expect(within(history).getByLabelText('storage.longTerm')).toBeChecked()
  expect(within(history).getByLabelText('storage.retention')).toBeDisabled()
  expect(document.querySelector('details')?.open).toBe(false)
  expect(screen.getByRole('button', { name: 'storage.save' })).toBeDisabled()
})
it('preserves edits on refresh and reports validation failures without saving', async () => {
  render(<StorageTab />)
  const input = await screen.findByLabelText('storage.max_turn_mb')
  fireEvent.change(input, { target: { value: '-1' } })
  fireEvent.click(screen.getByRole('button', { name: 'storage.save' }))
  expect(screen.getByRole('alert')).toHaveTextContent('storage.validation')
  expect(api.updateStorage).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: 'storage.refresh' }))
  await waitFor(() => expect(api.getStorage).toHaveBeenCalledTimes(2))
  expect(input).toHaveValue(-1)
})
it('previews a tighter policy and requires confirmation before applying once', async () => {
  const next = structuredClone(snapshot.policy); next.execution_details.retention_days = 10
  api.previewStorage.mockResolvedValue({ ...preview, policy: next })
  render(<StorageTab />)
  const details = await screen.findByRole('region', { name: 'storage.execution_details' })
  fireEvent.change(within(details).getByLabelText('storage.retention'), { target: { value: '10' } })
  fireEvent.click(screen.getByRole('button', { name: 'storage.save' }))
  const dialog = await screen.findByRole('dialog')
  expect(api.updateStorage).not.toHaveBeenCalled()
  expect(within(dialog).getByText(/storage.protected.*3/)).toBeInTheDocument()
  let finish!: (value: StorageSnapshot) => void
  api.updateStorage.mockImplementation(() => new Promise((resolve) => { finish = resolve }))
  const confirm = within(dialog).getByRole('button', { name: 'storage.confirmSave' })
  fireEvent.click(confirm); fireEvent.click(confirm)
  expect(api.updateStorage).toHaveBeenCalledTimes(1)
  expect(api.updateStorage).toHaveBeenCalledWith(next, 'rev1', 'token')
  finish({ ...snapshot, policy: next })
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
})
it('cleans only after preview confirmation, with explicit legacy opt-in', async () => {
  render(<StorageTab />)
  fireEvent.click(await screen.findByLabelText('storage.includeLegacy'))
  fireEvent.click(screen.getByRole('button', { name: 'storage.previewCleanup' }))
  const dialog = await screen.findByRole('dialog')
  expect(api.previewStorage).toHaveBeenCalledWith('rev1', undefined, true)
  expect(api.cleanupStorage).not.toHaveBeenCalled()
  fireEvent.click(within(dialog).getByRole('button', { name: 'storage.confirmCleanup' }))
  await waitFor(() => expect(api.cleanupStorage).toHaveBeenCalledWith('token', 'rev1'))
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
})
it('reports a stale preview while preserving the proposed policy', async () => {
  api.updateStorage.mockRejectedValue(new Error('preview expired'))
  render(<StorageTab />)
  const details = await screen.findByRole('region', { name: 'storage.execution_details' })
  fireEvent.change(within(details).getByLabelText('storage.retention'), { target: { value: '10' } })
  fireEvent.click(screen.getByRole('button', { name: 'storage.save' }))
  fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'storage.confirmSave' }))
  expect(await screen.findByText('preview expired')).toBeInTheDocument()
  expect(within(details).getByLabelText('storage.retention')).toHaveValue(10)
})
it('shows a retry state after initial load failure and explicit empty usage', async () => {
  api.getStorage.mockRejectedValueOnce(new Error('offline'))
  render(<StorageTab />)
  expect(await screen.findByRole('alert')).toHaveTextContent('offline')
  api.getStorage.mockResolvedValue({ ...snapshot, usage: { ...snapshot.usage, total_bytes: 0, categories: [] } })
  fireEvent.click(screen.getByRole('button', { name: 'storage.retry' }))
  expect(await screen.findByText('storage.empty')).toBeInTheDocument()
})
it('detects effective HTTP tightening and rejects malformed limit values', () => {
  const next = structuredClone(snapshot.policy)
  next.runtime_logs.http_override = true; next.runtime_logs.http_retention_days = 5
  expect(tightensStoragePolicy(snapshot.policy, next)).toBe(true)
  next.execution_details.max_turn_mb = NaN
  expect(validateStoragePolicy(next)).toBe(false)
  expect(validateStoragePolicy(snapshot.policy)).toBe(true)
})

it('does not carry an expired state into a newly generated preview token', async () => {
  const props = { mode: 'cleanup' as const, busy: false, onClose: vi.fn(), onConfirm: vi.fn() }
  const view = render(<StoragePreviewDialog {...props} preview={{ ...preview, preview_id: 'old-token', expires_at: '2000-01-01T00:00:00Z' }} />)
  expect(await screen.findByText('storage.stale')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'storage.confirmCleanup' })).toBeDisabled()
  view.rerender(<StoragePreviewDialog {...props} preview={{ ...preview, preview_id: 'new-token' }} />)
  await waitFor(() => expect(screen.queryByText('storage.stale')).not.toBeInTheDocument())
  expect(screen.getByRole('button', { name: 'storage.confirmCleanup' })).not.toBeDisabled()
})

it('previews enabling HTTP overrides even when inherited limits are unchanged', async () => {
  const next = structuredClone(snapshot.policy); next.runtime_logs.http_override = true
  api.previewStorage.mockResolvedValue({ ...preview, policy: next })
  render(<StorageTab />)
  fireEvent.click(await screen.findByLabelText('storage.httpOverride'))
  fireEvent.click(screen.getByRole('button', { name: 'storage.save' }))
  expect(await screen.findByRole('dialog')).toBeInTheDocument()
  expect(api.previewStorage).toHaveBeenCalledWith('rev1', next, false)
  expect(api.updateStorage).not.toHaveBeenCalled()
  expect(tightensStoragePolicy(snapshot.policy, next)).toBe(true)
})
