'use client';

import { useQueryClient } from '@tanstack/react-query';
import { useEffect, useSyncExternalStore } from 'react';
import { useRouter } from '@/i18n/navigation';
import { CONNECTING, UNAVAILABLE, type Connection, type ConnectionStore } from './connection-store';
import { acquireLiveLot, liveLotStore } from './manager';

const subscribeNothing = () => () => {};
const getConnecting = () => CONNECTING;
const getUnavailable = () => UNAVAILABLE;

/**
 * The lot's live connection state (M2B-19). The stream writes availability and spaces into the
 * Query cache; this returns only the connection. A `null` origin (no usable `API_PUBLIC_URL`)
 * never connects. A `lot.not_found` refreshes the route once per mount (M2B-7).
 */
export function useLiveLot(lotId: string, origin: string | null): Connection {
  const queryClient = useQueryClient();
  const router = useRouter();
  // The registry is browser-only module state; the server render reads a constant.
  const store: ConnectionStore | null = origin !== null && typeof window !== 'undefined' ? liveLotStore(origin, lotId) : null;

  useEffect(() => {
    if (origin === null) return;
    let refreshed = false;
    const { release } = acquireLiveLot(origin, lotId, queryClient, {
      onLotGone: () => {
        if (refreshed) return;
        refreshed = true;
        router.refresh();
      },
    });
    return release;
  }, [origin, lotId, queryClient, router]);

  const getServerSnapshot = origin === null ? getUnavailable : getConnecting;
  return useSyncExternalStore(
    store ? store.subscribe : subscribeNothing,
    store ? store.get : getServerSnapshot,
    getServerSnapshot,
  );
}
