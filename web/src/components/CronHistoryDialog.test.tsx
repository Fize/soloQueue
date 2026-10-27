import { render, screen, within } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import { CronHistoryDialog } from './CronHistoryDialog'
import type { CronTask, CronExecutionRecord } from '@/types'
const api = vi.hoisted(() => ({ listCronHistory: vi.fn(), getCronHistory: vi.fn(), t: (key: string) => key }))
vi.mock('@/lib/api', () => ({ listCronHistory: api.listCronHistory, getCronHistory: api.getCronHistory }))
vi.mock('@/lib/i18n', () => ({ useTranslation: () => ({ t: api.t }) }))
vi.mock('@/components/ui/streamdown-preview', () => ({ StreamdownPreview: ({ content }: { content: string }) => <div>{content}</div> }))
const task: CronTask = { id: 'task1', title: 'Daily task', task_type: 'general', expression: '0 9 * * *', instruction: 'Report', target_agent: 'leader', status: 'active', next_run_at: '', created_at: '', updated_at: '' }
const record: CronExecutionRecord = { id: 'exec1', task_id: 'task1', executed_at: '2026-10-26T00:00:00Z', completed_at: '2026-10-26T00:00:02Z', duration_ms: 2000, status: 'success', result_summary: '', error_message: '', task_type: 'general', target_agent: 'leader', model_id: 'model', provider_id: 'provider', timeline_dir: '' }
beforeEach(() => { vi.resetAllMocks(); api.listCronHistory.mockResolvedValue([record]); api.getCronHistory.mockResolvedValue({ execution: record, events: [] }) })
it('distinguishes expired history from empty history and preserves the result summary and execution status', async () => {
  const expired = { ...record, details_expired: true, result_summary: 'Saved successful result' }
  api.listCronHistory.mockResolvedValue([expired]); api.getCronHistory.mockResolvedValue({ execution: expired, events: [] })
  const close = vi.fn()
  render(<CronHistoryDialog open task={task} onClose={close} />)
  const dialog = await screen.findByRole('dialog')
  expect(await within(dialog).findByText('observability.cronExpired')).toBeInTheDocument()
  expect(within(dialog).queryByText('cron.noExecutionDetail')).not.toBeInTheDocument()
  const summary = within(dialog).getByText('observability.resultSummary').parentElement!
  expect(summary).toHaveTextContent('Saved successful result')
  expect(within(dialog).getByText('cron.jobSucceeded')).toBeInTheDocument()
  expect(within(dialog).getByRole('link', { name: 'observability.settings' })).toHaveAttribute('href', '#/settings/storage?category=cron_history')
})
it('keeps an ordinary empty execution distinct from expiration', async () => {
  render(<CronHistoryDialog open task={task} onClose={vi.fn()} />)
  expect(await screen.findByText('cron.noExecutionDetail')).toBeInTheDocument()
  expect(screen.queryByText('observability.cronExpired')).not.toBeInTheDocument()
  expect(screen.queryByRole('link', { name: 'observability.settings' })).not.toBeInTheDocument()
})
it('preserves available timeline output and failure metadata alongside expiration', async () => {
  const failed = { ...record, status: 'failed' as const, details_expired: true, error_message: 'Saved execution failure' }
  api.listCronHistory.mockResolvedValue([failed]); api.getCronHistory.mockResolvedValue({ execution: failed, events: [{ ts: failed.executed_at, type: 'message', msg: { role: 'assistant', content: 'Available final output' } }] })
  render(<CronHistoryDialog open task={task} onClose={vi.fn()} />)
  expect(await screen.findByText('Available final output')).toBeInTheDocument()
  expect(screen.getByText('observability.cronExpired')).toBeInTheDocument()
  expect(screen.getAllByText('Saved execution failure')).toHaveLength(2)
  expect(screen.getByText('cron.jobFailed')).toBeInTheDocument()
})
