import { request } from './core'
import type { WorkflowMeta, WorkflowRunDetail, WorkflowRunSummary, WorkflowTask, WorkflowRunEvent, BuiltinWorkflowView } from '@/types'

const workflowPath = (name: string) => `/workflows/${encodeURIComponent(name)}`

export interface WorkflowDocumentResponse {
  name: string
  yaml: string
  meta: WorkflowMeta
}

export function listWorkflows() {
  return request<WorkflowMeta[]>('/workflows/')
}

export function getWorkflow(name: string) {
  return request<WorkflowDocumentResponse>(`${workflowPath(name)}/`)
}

export function createWorkflow(name: string, yaml?: string) {
  const body: { name: string; yaml?: string } = { name }
  if (yaml?.trim()) body.yaml = yaml
  return request<WorkflowMeta>('/workflows/', { method: 'POST', body: JSON.stringify(body) })
}

export function updateWorkflow(name: string, yaml: string) {
  return request<WorkflowMeta>(`${workflowPath(name)}/`, { method: 'PUT', body: JSON.stringify({ yaml }) })
}

export function deleteWorkflow(name: string) {
  return request<void>(`${workflowPath(name)}/`, { method: 'DELETE' })
}

export function validateWorkflowYAML(yaml: string) {
  return request<{ valid: boolean; error?: string }>('/workflows/validate', { method: 'POST', body: JSON.stringify({ yaml }) })
}

export function listWorkflowRuns(name: string) {
  return request<WorkflowRunSummary[]>(`${workflowPath(name)}/runs`)
}

export function getWorkflowRun(name: string, runID: string) {
  return request<WorkflowRunDetail>(`${workflowPath(name)}/runs/${encodeURIComponent(runID)}/`)
}

export function startWorkflowRun(name: string, task: WorkflowTask, repository?: string) {
	return request<{ run_id: string; status?: string }>(`${workflowPath(name)}/runs`, {
		method: 'POST',
		body: JSON.stringify({ task, ...(repository ? { repository } : {}) }),
	})
}

export function cancelWorkflowRun(name: string, runID: string) {
	return request<{ run_id: string }>(`${workflowPath(name)}/runs/${encodeURIComponent(runID)}/cancel`, { method: 'POST' })
}

export function pauseWorkflowRun(name: string, runID: string, mode: 'graceful' | 'force' = 'graceful') {
	return request<{ run_id: string }>(`${workflowPath(name)}/runs/${encodeURIComponent(runID)}/pause`, { method: 'POST', body: JSON.stringify({ mode }) })
}

export function resumeWorkflowRun(name: string, runID: string, allowDirty = false) {
	return request<{ run_id: string }>(`${workflowPath(name)}/runs/${encodeURIComponent(runID)}/resume`, { method: 'POST', body: JSON.stringify({ allow_dirty: allowDirty }) })
}

export function restartWorkflowRun(name: string, runID: string) {
	return request<{ run_id: string; restarted_from_run_id?: string }>(`${workflowPath(name)}/runs/${encodeURIComponent(runID)}/restart`, { method: 'POST' })
}

export function abandonWorkflowRun(name: string, runID: string) {
	return request<{ run_id: string }>(`${workflowPath(name)}/runs/${encodeURIComponent(runID)}/abandon`, { method: 'POST' })
}

export function cleanupWorkflowRun(name: string, runID: string, force = false) {
	return request<{ run_id: string }>(`${workflowPath(name)}/runs/${encodeURIComponent(runID)}/cleanup?force=${force}`, { method: 'POST' })
}

export function listWorkflowRunEvents(name: string, runID: string, after = 0, limit = 100) {
	return request<{ data: WorkflowRunEvent[]; error: string | null }>(`${workflowPath(name)}/runs/${encodeURIComponent(runID)}/events?after=${after}&limit=${limit}`)
}

export function resolveWorkflowConfirmation(name: string, runID: string, callID: string, choice: string) {
	return request<{ run_id: string; call_id: string }>(`${workflowPath(name)}/runs/${encodeURIComponent(runID)}/confirmations/${encodeURIComponent(callID)}/resolve`, {
		method: 'POST',
		body: JSON.stringify({ choice }),
	})
}

export function listBuiltinWorkflows() {
	return request<BuiltinWorkflowView[]>('/workflows/builtin')
}

export function installBuiltinWorkflows(names: string[]) {
	return request<Array<{ name: string; status: string; created: boolean }>>('/workflows/builtin/install', { method: 'POST', body: JSON.stringify({ names }) })
}
