import { useState, useEffect } from 'react';
import type { RuntimeStatus } from '@/types';
import { wsManager } from '@/lib/websocket';

export function useRuntime() {
  const [status, setStatus] = useState<RuntimeStatus | null>(null);

  useEffect(() => {
    const unsubscribe = wsManager.subscribe('runtime', (data) => {
      setStatus(data);
    });
    return unsubscribe;
  }, []);

  return status;
}
