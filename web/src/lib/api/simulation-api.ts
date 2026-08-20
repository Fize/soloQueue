import { request, requestJson } from './core'

export interface SimulationSeedRequest {
  seed_text: string
  topic?: string
  persona_count?: number
  model_id?: string
  provider_id?: string
  max_wall_clock_ms?: number
  simulated_hours?: number
  time_scale?: number
  tick_interval_ms?: number
  enable_reflection?: boolean
  language?: string
}

export interface SimulationAnswerResponse {
  answer?: string
}

export async function listSimulations<T = any[]>(): Promise<T> {
  return request<T>('/simulations')
}

export async function getSimulation<T = any>(id: string, options?: RequestInit): Promise<T> {
  return request<T>(`/simulations/${encodeURIComponent(id)}`, options)
}

export async function getSimulationEnvironment<T = { world_state?: Record<string, any> }>(
  id: string,
  options?: RequestInit
): Promise<T> {
  return request<T>(`/simulations/${encodeURIComponent(id)}/environment`, options)
}

export async function createSimulationFromSeed<T = { simulation_id: string }>(
  payload: SimulationSeedRequest
): Promise<T> {
  return request<T>('/simulations/from-seed', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export async function updateSimulation<T = any>(id: string, payload: unknown): Promise<T> {
  return request<T>(`/simulations/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export async function deleteSimulation(id: string): Promise<void> {
  await requestJson(`/simulations/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export async function forkSimulation<T = { new_simulation_id: string }>(
  id: string,
  payload: { new_topic: string; new_max_wall_clock_ms: number }
): Promise<T> {
  return request<T>(`/simulations/${encodeURIComponent(id)}/fork`, {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export async function controlSimulation(id: string, action: 'start' | 'stop' | 'pause' | 'resume' | 'step') {
  await requestJson(`/simulations/${encodeURIComponent(id)}/${action}`, { method: 'POST' })
}

export async function askSimulationAgent(
  simulationId: string,
  agentId: string,
  question: string
): Promise<SimulationAnswerResponse> {
  return request<SimulationAnswerResponse>(
    `/simulations/${encodeURIComponent(simulationId)}/agents/${encodeURIComponent(agentId)}/ask`,
    {
      method: 'POST',
      body: JSON.stringify({ question }),
    }
  )
}

export async function getSimulationAgentPlan<T = { plan?: any }>(
  simulationId: string,
  agentId: string
): Promise<T> {
  return request<T>(
    `/simulations/${encodeURIComponent(simulationId)}/agents/${encodeURIComponent(agentId)}/plan`
  )
}

export async function getSimulationAgentMemories<T = { memories?: any[] }>(
  simulationId: string,
  agentId: string
): Promise<T> {
  return request<T>(
    `/simulations/${encodeURIComponent(simulationId)}/agents/${encodeURIComponent(agentId)}/memory`
  )
}

export async function getSimulationAgentReflections<T = { reflections?: any[] }>(
  simulationId: string,
  agentId: string
): Promise<T> {
  return request<T>(
    `/simulations/${encodeURIComponent(simulationId)}/agents/${encodeURIComponent(agentId)}/reflections`
  )
}
