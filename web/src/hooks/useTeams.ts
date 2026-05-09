import { useState, useEffect } from 'react';
import type { TeamListResponse } from '@/types';
import { getTeams } from '@/lib/api';

export function useTeams() {
  const [data, setData] = useState<TeamListResponse | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    setLoading(true);
    getTeams()
      .then(setData)
      .catch(() => setData(null))
      .finally(() => setLoading(false));
  }, []);

  return { data, loading };
}
