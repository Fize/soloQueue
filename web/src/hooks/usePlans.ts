import { useState, useEffect, useCallback } from 'react'
import type { Plan, PlanStatus } from '@/types'
import { listPlans, updatePlanStatus } from '@/lib/api'

export function usePlans() {
  const [plans, setPlans] = useState<Plan[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchPlans = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await listPlans()
      setPlans(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch plans')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchPlans()
  }, [fetchPlans])

  const movePlan = useCallback(
    async (planId: string, newStatus: PlanStatus) => {
      // Optimistic update
      setPlans((prev) => prev.map((p) => (p.id === planId ? { ...p, status: newStatus } : p)))
      try {
        await updatePlanStatus(planId, newStatus)
      } catch {
        // Rollback on failure
        setPlans((prev) =>
          prev.map((p) =>
            p.id === planId
              ? { ...p, status: plans.find((pl) => pl.id === planId)?.status ?? p.status }
              : p
          )
        )
      }
    },
    [plans]
  )

  const plansByStatus = {
    plan: plans.filter((p) => p.status === 'plan'),
    running: plans.filter((p) => p.status === 'running'),
    done: plans.filter((p) => p.status === 'done'),
  }

  return { plans, plansByStatus, loading, error, fetchPlans, movePlan }
}
