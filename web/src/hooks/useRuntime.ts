import { useState, useEffect, useRef } from 'react';
import type { RuntimeStatus } from '@/types';
import { getRuntime } from '@/lib/api';

export function useRuntime() {
  const [status, setStatus] = useState<RuntimeStatus | null>(null);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function poll() {
      try {
        const data = await getRuntime();
        if (!cancelled) setStatus(data);
      } catch {
        if (!cancelled) setStatus(null);
      }
    }

    poll();
    intervalRef.current = setInterval(poll, 2000);

    return () => {
      cancelled = true;
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, []);

  return status;
}
