import { request } from './core'
import type { WorkflowMeta, WorkflowRunDetail, WorkflowRunSummary } from '@/types'

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

export function createWorkflow(name: string, yaml: string) {
  return request<WorkflowMeta>('/workflows/', { method: 'POST', body: JSON.stringify({ name, yaml }) })
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

export function startWorkflowRun(name: string, input = '') {
  return request<{ run_id: string }>(`${workflowPath(name)}/runs`, { method: 'POST', body: JSON.stringify({ input }) })
}

export function cancelWorkflowRun(name: string, runID: string) {
  return request<{ run_id: string }>(`${workflowPath(name)}/runs/${encodeURIComponent(runID)}/cancel`, { method: 'POST' })
}
