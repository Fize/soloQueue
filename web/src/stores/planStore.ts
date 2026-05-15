import { create } from 'zustand'
import { listPlans, updatePlanStatus } from '@/lib/api'
import type { Plan, PlanStatus } from '@/types'

interface PlanState {
  plans: Plan[]
  loading: boolean
  error: string | null
  fetchPlans: () => Promise<void>
  movePlan: (planId: string, newStatus: PlanStatus) => Promise<void>
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
}))
