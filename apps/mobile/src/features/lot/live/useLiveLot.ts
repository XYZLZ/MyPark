import { lotQueryKeys } from '@mypark/contracts';
import { useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useSyncExternalStore } from 'react';

import { getApiBaseUrl } from '@/lib/env';

import { CONNECTING, type Connection } from './connection-store';
import { acquireLiveLot, liveLotStore } from './manager';

const subscribeNothing = () => () => {};
const getConnecting = () => CONNECTING;

/**
 * The lot's live connection state (M2B-19). The stream writes availability and spaces into the
 * Query cache; this returns only the connection. A `null` lot never connects. A `lot.not_found`
 * invalidates the lot list once per mount, so `useLotId` re-discovers the lot (M2B-7).
 */
export function useLiveLot(lotId: string | null): Connection {
  const queryClient = useQueryClient();
  const origin = getApiBaseUrl();
  const rediscovered = useRef(false);
  const store = lotId === null ? null : liveLotStore(origin, lotId);

  useEffect(() => {
    if (lotId === null) return;
    const { release } = acquireLiveLot(origin, lotId, queryClient, {
      onLotGone: () => {
        if (rediscovered.current) return;
        rediscovered.current = true;
        void queryClient.invalidateQueries({ queryKey: lotQueryKeys.all });
      },
    });
    return release;
  }, [origin, lotId, queryClient]);

  return useSyncExternalStore(store ? store.subscribe : subscribeNothing, store ? store.get : getConnecting);
}
