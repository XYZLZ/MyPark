import {
  createLiveLotRegistry,
  HTTP_FAILURE_FLOOR_MS,
  type Availability,
  type LiveAdapters,
  type SpaceState,
} from '@mypark/contracts';
import type { QueryClient } from '@tanstack/react-query';
import { AppState, type AppStateStatus } from 'react-native';

import { openStream, type StreamEnd, type StreamFetch } from '@/services/sse';

import { fetchLotSnapshot } from '../api';
import type { ConnectionStore } from './connection-store';

export type MobileLiveDeps = {
  /** Defaults to `expo/fetch` inside `openStream`. */
  fetchImpl: StreamFetch;
  random: () => number;
  now: () => number;
  appState: {
    /** When it is `background` or `inactive` at the first acquire, nothing opens until `active`. */
    readonly currentState?: string | null;
    addEventListener(type: 'change', listener: (state: AppStateStatus) => void): { remove(): void };
  };
  fetchSnapshot: (lotId: string) => Promise<{ availability: Availability; spaces: SpaceState[] }>;
  onLotGone: () => void;
  /** Dev diagnostics, e.g. a dropped frame (design §5.2). A no-op outside development. */
  log: (message: string, detail?: unknown) => void;
};

const noop = () => {};
const isHidden = (state: string | null | undefined) => state === 'background' || state === 'inactive';

/** The shared rules live in contracts (`createLiveLotRegistry`); this module is the app's adapters. */
const registry = createLiveLotRegistry();

/**
 * Unlike `EventSource`, `expo/fetch` shows the status of a failed open (M2B-21): a 429 honors
 * `Retry-After`, a `404 lot.not_found` stops, any other non-200 takes the 10 s floor. The app going
 * to the background or inactive aborts the stream; `active` reopens it.
 */
function mobileAdapters(deps: Partial<MobileLiveDeps>): LiveAdapters<StreamEnd> {
  const appState: MobileLiveDeps['appState'] = deps.appState ?? AppState;
  return {
    openTransport(url, { onFrame, onEnd }) {
      const controller = new AbortController();
      void openStream(url, { signal: controller.signal, onFrame, fetchImpl: deps.fetchImpl }).then(onEnd);
      return () => controller.abort();
    },
    classifyEnd(end) {
      if (end.kind !== 'http') return { floorMs: 0, retryAfterMs: null, gone: false };
      return {
        floorMs: HTTP_FAILURE_FLOOR_MS,
        retryAfterMs: end.retryAfterMs,
        gone: end.status === 404 && end.code === 'lot.not_found',
      };
    },
    subscribeLifecycle(onHide, onShow) {
      const subscription = appState.addEventListener('change', (state) => {
        if (isHidden(state)) onHide();
        else if (state === 'active') onShow();
      });
      if (isHidden(appState.currentState)) onHide();
      return () => subscription.remove();
    },
    fetchSnapshot: deps.fetchSnapshot ?? fetchLotSnapshot,
    timers: { set: (callback, ms) => setTimeout(callback, ms), clear: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>) },
    random: deps.random ?? Math.random,
    now: deps.now ?? (() => Date.now()),
    log: deps.log ?? (__DEV__ ? (message, detail) => console.debug(message, detail) : noop),
  };
}

/** The connection store for a lot. */
export function liveLotStore(origin: string, lotId: string): ConnectionStore {
  return registry.store(origin, lotId);
}

/**
 * Shares one stream per `(origin, lotId)`, reference-counted, aborted `IDLE_CLOSE_MS` after the
 * last release (M2B-20). The first acquirer's deps drive the stream; `onLotGone` is per caller.
 */
export function acquireLiveLot(
  origin: string,
  lotId: string,
  queryClient: QueryClient,
  deps: Partial<MobileLiveDeps> = {},
): { store: ConnectionStore; release(): void } {
  return registry.acquire(origin, lotId, queryClient, () => mobileAdapters(deps), deps.onLotGone ?? noop);
}

/** Closes and forgets every stream and store. */
export function resetLiveLotsForTests(): void {
  registry.reset();
}
