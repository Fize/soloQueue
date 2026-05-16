import { create } from 'zustand'
import { listPlans, updatePlanStatus, createPlan as apiCreatePlan, updatePlan as apiUpdatePlan, deletePlan as apiDeletePlan } from '@/lib/api'
import type { Plan, PlanStatus, CreatePlanRequest, UpdatePlanRequest } from '@/types'

interface PlanState {
  plans: Plan[]
  loading: boolean
  error: string | null
  fetchPlans: () => Promise<void>
  movePlan: (planId: string, newStatus: PlanStatus) => Promise<void>
  createPlan: (data: CreatePlanRequest) => Promise<Plan>
  updatePlan: (id: string, data: UpdatePlanRequest) => Promise<Plan>
  deletePlan: (id: string) => Promise<void>
}

export const usePlanStore = create<PlanState>((set, get) => ({
  plans: [],
  loading: true,
  error: null,

  fetchPlans: async () => {
    set({ loading: true, error: null })
    try {
      const data = await listPlans()
      set({ plans: data, loading: false })
    } catch (err) {
      set({
        error: err instanceof Error ? err.message : 'Failed to fetch plans',
        loading: false,
      })
    }
  },

  movePlan: async (planId: string, newStatus: PlanStatus) => {
    const prevPlans = get().plans
    // Optimistic update
    set((state) => ({
      plans: state.plans.map((p) => (p.id === planId ? { ...p, status: newStatus } : p)),
    }))
    try {
      await updatePlanStatus(planId, newStatus)
    } catch {
      // Rollback on failure
      set({ plans: prevPlans })
    }
  },

  createPlan: async (data: CreatePlanRequest) => {
    const plan = await apiCreatePlan(data)
    set((state) => ({ plans: [plan, ...state.plans] }))
    return plan
  },

  updatePlan: async (id: string, data: UpdatePlanRequest) => {
    const prevPlans = get().plans
    // Optimistic update — apply partial changes immediately
    set((state) => ({
      plans: state.plans.map((p) =>
        p.id === id ? { ...p, ...data, updated_at: new Date().toISOString() } as Plan : p
      ),
    }))
    try {
      const plan = await apiUpdatePlan(id, data)
      // Replace with server response to get accurate updated_at etc
      set((state) => ({
        plans: state.plans.map((p) => (p.id === id ? plan : p)),
      }))
      return plan
    } catch {
      set({ plans: prevPlans })
      throw new Error('Failed to update plan')
    }
  },

  deletePlan: async (id: string) => {
    const prevPlans = get().plans
    set((state) => ({
      plans: state.plans.filter((p) => p.id !== id),
    }))
    try {
      await apiDeletePlan(id)
    } catch {
      set({ plans: prevPlans })
      throw new Error('Failed to delete plan')
    }
  },
}))
