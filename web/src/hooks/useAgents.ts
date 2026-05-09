import { useState, useEffect, useRef } from 'react';
import type { AgentListResponse } from '@/types';
import { getAgents } from '@/lib/api';

export function useAgents() {
  const [data, setData] = useState<AgentListResponse | null>(null);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function poll() {
      try {
        const result = await getAgents();
        if (!cancelled) setData(result);
      } catch {
        if (!cancelled) setData(null);
      }
    }

    poll();
    intervalRef.current = setInterval(poll, 2000);

    return () => {
      cancelled = true;
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, []);

  return data;
}
