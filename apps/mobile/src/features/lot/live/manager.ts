import {
  ApiError,
  decodeFrame,
  FRESH_WINDOW_MS,
  HTTP_FAILURE_FLOOR_MS,
  IDLE_CLOSE_MS,
  initialLiveState,
  lotQueryKeys,
  nextDelay,
  reduceLive,
  SNAPSHOT_MIN_INTERVAL_MS,
  streamUrl,
  type Availability,
  type LiveFrame,
  type LiveState,
  type SpaceState,
  type SseFrame,
} from '@mypark/contracts';
import type { QueryClient } from '@tanstack/react-query';
import { fetch } from 'expo/fetch';
import { AppState, type AppStateStatus } from 'react-native';

import { openStream, type StreamEnd, type StreamFetch } from '@/services/sse';

import { fetchLotSnapshot } from '../api';
import { CONNECTING, createConnectionStore, type ConnectionStore } from './connection-store';

export type MobileLiveDeps = {
  fetchImpl: StreamFetch;
  random: () => number;
  now: () => number;
  appState: { addEventListener(type: 'change', listener: (state: AppStateStatus) => void): { remove(): void } };
  fetchSnapshot: (lotId: string) => Promise<{ availability: Availability; spaces: SpaceState[] }>;
  onLotGone: () => void;
  /** Dev diagnostics, e.g. a dropped frame (design §5.2). A no-op outside development. */
  log: (message: string, detail?: unknown) => void;
};

type LiveLot = { acquire(onLotGone: () => void): { store: ConnectionStore; release(): void }; teardown(): void };

const noop = () => {};
const devLog: MobileLiveDeps['log'] = (message, detail) => console.debug(message, detail);

/** One live stream per `(origin, lotId)` per app instance (M2B-20). */
const lots = new Map<string, LiveLot>();
/**
 * Stores outlive streams, so a hook can read one during render before its effect acquires.
 * A store is forgotten once its lot is torn down and nothing listens to it.
 */
const stores = new Map<string, { store: ConnectionStore; listeners: number }>();

const keyOf = (origin: string, lotId: string) => JSON.stringify([origin, lotId]);

function pruneStore(key: string): void {
  if (!lots.has(key) && stores.get(key)?.listeners === 0) stores.delete(key);
}

/** The connection store for a lot. */
export function liveLotStore(origin: string, lotId: string): ConnectionStore {
  const key = keyOf(origin, lotId);
  const existing = stores.get(key);
  if (existing) return existing.store;
  const base = createConnectionStore(CONNECTING);
  const entry = {
    listeners: 0,
    store: {
      get: base.get,
      set: base.set,
      subscribe(listener: () => void) {
        const unsubscribe = base.subscribe(listener);
        entry.listeners++;
        let active = true;
        return () => {
          if (!active) return;
          active = false;
          unsubscribe();
          entry.listeners--;
          if (stores.get(key) === entry) pruneStore(key);
        };
      },
    } satisfies ConnectionStore,
  };
  stores.set(key, entry);
  return entry.store;
}

/**
 * The mobile stream manager (design §5.4). Unlike `EventSource`, `expo/fetch` shows the status of a
 * failed open, so a 429 honors `Retry-After` and a `404 lot.not_found` stops. Frames go through
 * `reduceLive` into the Query cache; the connection state goes into the lot's store. The app going
 * to the background aborts the stream; coming back reopens it.
 */
function createLiveLot(
  key: string,
  origin: string,
  lotId: string,
  queryClient: QueryClient,
  deps: Omit<MobileLiveDeps, 'onLotGone'>,
): LiveLot {
  const store = liveLotStore(origin, lotId);
  const goneHandlers: (() => void)[] = [];
  let refs = 0;
  let state: LiveState = initialLiveState({
    availability: queryClient.getQueryData<Availability>(lotQueryKeys.availability(lotId)),
    spaces: queryClient.getQueryData<SpaceState[]>(lotQueryKeys.spaces(lotId)),
  });
  let controller: AbortController | null = null;
  let attempt = 0;
  let receivedFrame = false;
  let lastSnapshotAt = Number.NEGATIVE_INFINITY;
  let inBackground = false;
  let stopped = false;
  let tornDown = false;
  let freshTimer: ReturnType<typeof setTimeout> | undefined;
  let retryTimer: ReturnType<typeof setTimeout> | undefined;
  let retryPending = false;
  let idleTimer: ReturnType<typeof setTimeout> | undefined;

  /** Writes only what changed; the same reference means no cache write. */
  function apply(frame: LiveFrame): void {
    const next = reduceLive(state, frame);
    if (next === state) return;
    if (next.availability && next.availability !== state.availability) {
      queryClient.setQueryData(lotQueryKeys.availability(lotId), next.availability);
    }
    if (next.spaces && next.spaces !== state.spaces) queryClient.setQueryData(lotQueryKeys.spaces(lotId), next.spaces);
    state = next;
    store.set({ spacesReady: next.spacesReady });
  }

  function armFreshTimer(): void {
    clearTimeout(freshTimer);
    // A half-open connection never ends the read; the silence timer is what recovers it (M2B-13).
    freshTimer = setTimeout(() => fail(0, null), FRESH_WINDOW_MS);
  }

  function closeStream(): void {
    clearTimeout(freshTimer);
    controller?.abort();
    controller = null;
  }

  function cancelRetry(): void {
    clearTimeout(retryTimer);
    retryPending = false;
  }

  function open(): void {
    cancelRetry();
    if (stopped || inBackground || controller) return;
    const current = new AbortController();
    controller = current;
    receivedFrame = false;
    apply({ kind: 'open' });
    armFreshTimer();
    void openStream(streamUrl(origin, lotId), {
      signal: current.signal,
      fetchImpl: deps.fetchImpl,
      onFrame: (frame) => {
        if (controller === current) receive(frame);
      },
    }).then((end) => {
      // Our own aborts (silence, background, teardown) already moved on.
      if (controller === current) ended(end);
    });
  }

  /** Any frame counts as fresh, even one the schema or the version guard drops (M2B-13). */
  function receive(raw: SseFrame): void {
    armFreshTimer();
    if (!receivedFrame) {
      receivedFrame = true;
      attempt = 0;
    }
    const frame = decodeFrame(raw);
    if (frame) apply(frame);
    else deps.log('live: dropped a malformed stream frame', { event: raw.event });
    store.set({ status: 'live', lastFrameAt: deps.now() });
  }

  function ended(end: StreamEnd): void {
    if (end.kind === 'http' && end.status === 404 && end.code === 'lot.not_found') {
      lotGone();
      return;
    }
    if (end.kind === 'http') fail(HTTP_FAILURE_FLOOR_MS, end.retryAfterMs);
    else fail(0, null);
  }

  /** A drop, an HTTP failure or 45 s of silence: stale now, one snapshot, our own backoff. */
  function fail(floorMs: number, retryAfterMs: number | null): void {
    closeStream();
    // Before the snapshot, so its spaces replace a map the stream no longer keeps current.
    apply({ kind: 'lost' });
    store.set({ status: 'stale' });
    fetchSnapshot();
    const delay = Math.max(retryAfterMs ?? 0, nextDelay(attempt, deps.random, floorMs));
    attempt++;
    retryPending = true;
    // With no subscribers left (the idle close is pending) the retry is skipped; a new acquire opens.
    retryTimer = setTimeout(() => (refs > 0 ? open() : (retryPending = false)), delay);
  }

  /** The REST fallback: at most once per attempt and never within 10 s of the last (M2B-18). */
  function fetchSnapshot(): void {
    const at = deps.now();
    if (at - lastSnapshotAt < SNAPSHOT_MIN_INTERVAL_MS) return;
    lastSnapshotAt = at;
    deps.fetchSnapshot(lotId).then(
      ({ availability, spaces }) => {
        if (stopped) return;
        apply({ kind: 'snapshot', availability, spaces });
        // Values move, the state stays stale until the stream is back (design §5.4).
        store.set({ lastFrameAt: deps.now() });
      },
      (error: unknown) => {
        if (error instanceof ApiError && error.code === 'lot.not_found') lotGone();
      },
    );
  }

  /** Stop for good and let the callers re-discover the lot (M2B-7). */
  function lotGone(): void {
    if (stopped) return;
    stop();
    apply({ kind: 'lost' });
    store.set({ status: 'stale' });
    for (const handler of new Set(goneHandlers)) handler();
  }

  function stop(): void {
    stopped = true;
    closeStream();
    cancelRetry();
  }

  const onAppState = (next: AppStateStatus) => {
    if (stopped) return;
    if (next === 'background' || next === 'inactive') {
      if (inBackground) return;
      inBackground = true;
      closeStream();
      cancelRetry();
      apply({ kind: 'lost' });
      if (store.get().status === 'live') store.set({ status: 'stale' });
    } else if (next === 'active' && inBackground) {
      inBackground = false;
      // With no subscribers left the idle close is pending; a new acquire opens.
      if (refs === 0) return;
      store.set({ status: 'connecting' });
      open();
    }
  };
  const appStateSubscription = deps.appState.addEventListener('change', onAppState);

  function teardown(): void {
    if (tornDown) return;
    tornDown = true;
    stop();
    clearTimeout(idleTimer);
    appStateSubscription.remove();
    store.set(CONNECTING);
    lots.delete(key);
    pruneStore(key);
  }

  open();

  return {
    teardown,
    acquire(onLotGone) {
      refs++;
      clearTimeout(idleTimer);
      if (!controller && !retryPending) open();
      goneHandlers.push(onLotGone);
      let released = false;
      return {
        store,
        release() {
          // A release that outlives its stream must not touch a newer one under the same key.
          if (released || tornDown) return;
          released = true;
          goneHandlers.splice(goneHandlers.indexOf(onLotGone), 1);
          refs--;
          if (refs === 0) idleTimer = setTimeout(teardown, IDLE_CLOSE_MS);
        },
      };
    },
  };
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
  const key = keyOf(origin, lotId);
  let lot = lots.get(key);
  if (!lot) {
    lot = createLiveLot(key, origin, lotId, queryClient, {
      fetchImpl: deps.fetchImpl ?? fetch,
      random: deps.random ?? Math.random,
      now: deps.now ?? (() => Date.now()),
      appState: deps.appState ?? AppState,
      fetchSnapshot: deps.fetchSnapshot ?? fetchLotSnapshot,
      log: deps.log ?? (__DEV__ ? devLog : noop),
    });
    lots.set(key, lot);
  }
  return lot.acquire(deps.onLotGone ?? noop);
}

/** Closes and forgets every stream and store. */
export function resetLiveLotsForTests(): void {
  for (const lot of [...lots.values()]) lot.teardown();
  lots.clear();
  stores.clear();
}
