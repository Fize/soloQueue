import { useState, useEffect } from 'react';
import type { AgentListResponse } from '@/types';
import { wsManager } from '@/lib/websocket';

export function useAgents() {
  const [data, setData] = useState<AgentListResponse | null>(null);

  useEffect(() => {
    const unsubscribe = wsManager.subscribe('agents', (agentsData) => {
      setData(agentsData);
    });
    return unsubscribe;
  }, []);

  return data;
}
