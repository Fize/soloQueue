import { request } from './core'

export type StorageCategoryId = 'runtime_logs' | 'session_history' | 'execution_details' | 'cron_history'
export interface StorageLimits {
  max_file_mb: number
  retention_days: number
  max_files: number
  max_total_mb: number
}
export interface StoragePolicy {
  runtime_logs: StorageLimits & { http_override: boolean; http_max_file_mb: number; http_retention_days: number; http_max_files: number }
  session_history: StorageLimits & { warning_mb: number }
  execution_details: StorageLimits & { max_turn_mb: number }
  cron_history: StorageLimits & { max_per_task: number }
}
export interface StorageCategoryUsage {
  id: string
  bytes: number
  files: number
  oldest_at?: string
  warning?: boolean
}
export interface CleanupResult {
  finished_at?: string
  deleted_files: number
  freed_bytes: number
  errors: string[]
}
export interface StorageSnapshot {
  policy: StoragePolicy
  revision: string
  usage: {
    sampled_at: string
    total_bytes: number
    file_bytes: number
    database_bytes: number
    legacy_files?: { name: string; bytes: number; modified_at: string }[]
    categories: StorageCategoryUsage[]
    errors: string[]
  }
  last_cleanup?: CleanupResult | null
  next_cleanup_at?: string
  restart_required: boolean
}
export interface StoragePreview {
  preview_id: string
  revision: string
  expires_at: string
  files: number
  bytes: number
  categories: { id: string; files: number; bytes: number }[]
  protected_files: number
  errors: string[]
  policy: StoragePolicy
}
export const getStorage = () => request<StorageSnapshot>('/storage')
export const previewStorage = (revision: string, policy?: StoragePolicy, includeLegacy = false) =>
  request<StoragePreview>('/storage/preview', {
    method: 'POST', body: JSON.stringify({ revision, policy, include_legacy: includeLegacy }),
  })
export const updateStorage = (policy: StoragePolicy, revision: string, previewId?: string) =>
  request<StorageSnapshot>('/storage', {
    method: 'PUT', body: JSON.stringify({ policy, revision, preview_id: previewId }),
  })
export const cleanupStorage = (previewId: string, revision: string) =>
  request<CleanupResult>('/storage/cleanup', {
    method: 'POST', body: JSON.stringify({ preview_id: previewId, revision, confirm: true }),
  })

export function formatStorageBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 ** 2) return `${(bytes / 1024).toFixed(1)} KiB`
  if (bytes < 1024 ** 3) return `${(bytes / 1024 ** 2).toFixed(1)} MiB`
  return `${(bytes / 1024 ** 3).toFixed(2)} GiB`
}
