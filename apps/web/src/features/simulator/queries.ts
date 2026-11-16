'use client';

import { useQuery } from '@tanstack/react-query';
import { fetchStatus, simulatorKeys } from './api';

/** Status polls every 2 s while the page is visible (M2B-30); TanStack does not overlap a running fetch. */
export function useSimulatorStatus() {
  return useQuery({
    queryKey: simulatorKeys.status,
    queryFn: fetchStatus,
    refetchInterval: 2_000,
    refetchIntervalInBackground: false,
    retry: false,
    staleTime: 0,
  });
}
