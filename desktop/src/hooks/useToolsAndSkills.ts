import { useState, useEffect } from 'react'
import type { ToolListResponse, SkillListResponse } from '@/types'
import { getTools, getSkills } from '@/lib/api'

export function useTools() {
  const [tools, setTools] = useState<ToolListResponse | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    setLoading(true)
    getTools()
      .then(setTools)
      .catch(() => setTools(null))
      .finally(() => setLoading(false))
  }, [])

  return { tools, loading }
}

export function useSkills() {
  const [skills, setSkills] = useState<SkillListResponse | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    setLoading(true)
    getSkills()
      .then(setSkills)
      .catch(() => setSkills(null))
      .finally(() => setLoading(false))
  }, [])

  return { skills, loading }
}
