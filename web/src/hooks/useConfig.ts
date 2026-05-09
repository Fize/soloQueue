import { useState, useEffect } from 'react';
import type { AppConfig } from '@/types';
import { getConfig } from '@/lib/api';

export function useConfig() {
  const [config, setConfig] = useState<AppConfig | null>(null);

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

  return config;
}
