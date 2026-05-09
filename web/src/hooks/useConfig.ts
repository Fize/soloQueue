import { useState, useEffect, useCallback } from 'react';
import type { AppConfig } from '@/types';
import { getConfig, updateConfig } from '@/lib/api';

export function useConfig() {
  const [config, setConfig] = useState<AppConfig | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const data = await getConfig();
      setConfig(data);
      setError(null);
    } catch {
      setConfig(null);
    }
  }, []);

  useEffect(() => {
    let cancelled = false;
    getConfig()
      .then((data) => {
        if (!cancelled) setConfig(data);
      })
      .catch(() => {
        if (!cancelled) setConfig(null);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const patch = useCallback(async (partial: Partial<AppConfig>) => {
    setSaving(true);
    setError(null);
    try {
      const updated = await updateConfig(partial);
      setConfig(updated);
      return updated;
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to save config';
      setError(msg);
      throw err;
    } finally {
      setSaving(false);
    }
  }, []);

  return { config, saving, error, patch, refresh };
}
