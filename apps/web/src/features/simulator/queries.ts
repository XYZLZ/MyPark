'use client';

import { useQuery } from '@tanstack/react-query';
import { fetchStatus, simulatorKeys } from './api';
import { isNotConfigured } from './controls';

/**
 * Status polls every 2 s while the page is visible (M2B-30); TanStack does not overlap a running
 * fetch. Polling stops while the console is not configured: nothing will change without a deploy.
 */
export function useSimulatorStatus() {
  return useQuery({
    queryKey: simulatorKeys.status,
    queryFn: fetchStatus,
    refetchInterval: (query) => (isNotConfigured(query.state.error) ? false : 2_000),
    refetchIntervalInBackground: false,
    retry: false,
    staleTime: 0,
  });
}
