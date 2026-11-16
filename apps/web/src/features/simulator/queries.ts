'use client';

import { useQuery } from '@tanstack/react-query';
import { useConsoleStoreApi } from '@/providers/simulator-store-provider';
import { fetchEvents, fetchStatus, simulatorKeys } from './api';
import { isNotConfigured } from './controls';
import { applyEvents, markPollFailed, nextAfter, noteRun } from './log-cursor';

const POLL_MS = 2_000;

/**
 * Status polls every 2 s while the page is visible (M2B-30); TanStack does not overlap a running
 * fetch. Polling stops while the console is not configured: nothing will change without a deploy.
 * A failed poll makes the event log start over from 0 on its next request (M2B-31).
 */
export function useSimulatorStatus() {
  const store = useConsoleStoreApi();
  return useQuery({
    queryKey: simulatorKeys.status,
    queryFn: async () => {
      try {
        const result = await fetchStatus();
        const { log, setLog } = store.getState();
        setLog(noteRun(log, result.status));
        return result;
      } catch (error) {
        const { log, setLog } = store.getState();
        setLog(markPollFailed(log));
        throw error;
      }
    },
    refetchInterval: (query) => (isNotConfigured(query.state.error) ? false : POLL_MS),
    refetchIntervalInBackground: false,
    retry: false,
    staleTime: 0,
  });
}

/**
 * The event log polls on its own while the page is visible, whatever the scenario state (M2B-45).
 * Each answer is folded into the store's log with the cursor of M2B-31.
 */
export function useSimulatorEvents() {
  const store = useConsoleStoreApi();
  return useQuery({
    queryKey: simulatorKeys.events,
    queryFn: async () => {
      const after = nextAfter(store.getState().log);
      try {
        const events = await fetchEvents(after);
        const { log, setLog } = store.getState();
        setLog(applyEvents(log, after, events));
        return events.length;
      } catch (error) {
        const { log, setLog } = store.getState();
        setLog(markPollFailed(log));
        throw error;
      }
    },
    refetchInterval: (query) => (isNotConfigured(query.state.error) ? false : POLL_MS),
    refetchIntervalInBackground: false,
    retry: false,
    staleTime: 0,
  });
}
