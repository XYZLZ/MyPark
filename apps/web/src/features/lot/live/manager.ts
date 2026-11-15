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
} from '@mypark/contracts';
import type { QueryClient } from '@tanstack/react-query';
import { fetchLotSnapshot } from '../api';
import { CONNECTING, createConnectionStore, type ConnectionStore } from './connection-store';

export type EventSourceLike = Pick<EventSource, 'readyState' | 'close' | 'addEventListener' | 'removeEventListener'>;

export type LiveDeps = {
  EventSourceImpl: new (url: string) => EventSourceLike;
  fetchSnapshot: (lotId: string) => Promise<{ availability: Availability; spaces: SpaceState[] }>;
  random: () => number;
  now: () => number;
  onLotGone: () => void;
  target: Pick<Window, 'addEventListener' | 'removeEventListener'>;
  /** Dev diagnostics, e.g. a dropped frame (design §5.2). A no-op outside development. */
  log: (message: string, detail?: unknown) => void;
};

type LiveLot = { acquire(onLotGone: () => void): { store: ConnectionStore; release(): void }; teardown(): void };

const STREAM_EVENTS = ['availability', 'spaces', 'heartbeat'] as const;
/** `EventSource.CLOSED`, which does not exist outside the browser. */
const CLOSED = 2;
const noop = () => {};

/** One live connection per `(origin, lotId)` per app instance (M2B-20). */
const lots = new Map<string, LiveLot>();
/**
 * Stores outlive connections, so a hook can read one during render before its effect acquires.
 * A store is forgotten once its lot is torn down and nothing listens to it.
 */
const stores = new Map<string, { store: ConnectionStore; listeners: number }>();

const keyOf = (origin: string, lotId: string) => JSON.stringify([origin, lotId]);

function pruneStore(key: string): void {
  if (!lots.has(key) && stores.get(key)?.listeners === 0) stores.delete(key);
}

/** The connection store for a lot. Browser only: the registry is module state. */
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
 * The web connection manager (design §5.4). It owns reconnection: `EventSource` never retries
 * after an HTTP failure (429, 404) and retries a network drop with no jitter, so every `error`
 * closes the source and schedules our own attempt with `nextDelay`. Frames go through
 * `reduceLive` into the Query cache; the connection state goes into the lot's store.
 */
function createLiveLot(
  key: string,
  origin: string,
  lotId: string,
  queryClient: QueryClient,
  deps: Omit<LiveDeps, 'onLotGone'>,
): LiveLot {
  const store = liveLotStore(origin, lotId);
  const goneHandlers: Array<() => void> = [];
  let refs = 0;
  let state: LiveState = initialLiveState({
    availability: queryClient.getQueryData<Availability>(lotQueryKeys.availability(lotId)),
    spaces: queryClient.getQueryData<SpaceState[]>(lotQueryKeys.spaces(lotId)),
  });
  let source: EventSourceLike | null = null;
  let detach = noop;
  let attempt = 0;
  let receivedFrame = false;
  let lastSnapshotAt = Number.NEGATIVE_INFINITY;
  let closedByPageHide = false;
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
    // A half-open connection never raises `error`; the silence timer is what recovers it (M2B-13).
    freshTimer = setTimeout(() => fail(false), FRESH_WINDOW_MS);
  }

  function closeSource(): void {
    clearTimeout(freshTimer);
    detach();
    detach = noop;
    source?.close();
    source = null;
  }

  function open(): void {
    clearTimeout(retryTimer);
    retryPending = false;
    if (stopped || closedByPageHide || source) return;
    const es = new deps.EventSourceImpl(streamUrl(origin, lotId));
    const onFrame = (event: Event) => {
      if (source === es) receive(event as MessageEvent);
    };
    // Read readyState before close(), which would make every failure look like CLOSED.
    const onError = () => {
      if (source === es) fail(es.readyState === CLOSED);
    };
    for (const name of STREAM_EVENTS) es.addEventListener(name, onFrame);
    es.addEventListener('error', onError);
    detach = () => {
      for (const name of STREAM_EVENTS) es.removeEventListener(name, onFrame);
      es.removeEventListener('error', onError);
    };
    source = es;
    receivedFrame = false;
    apply({ kind: 'open' });
    armFreshTimer();
  }

  /** Any frame counts as fresh, even one the schema or the version guard drops (M2B-13). */
  function receive(event: MessageEvent): void {
    armFreshTimer();
    if (!receivedFrame) {
      receivedFrame = true;
      attempt = 0;
    }
    const data = typeof event.data === 'string' ? event.data : '';
    const frame = decodeFrame(event.lastEventId ? { event: event.type, id: event.lastEventId, data } : { event: event.type, data });
    if (frame) apply(frame);
    else deps.log('live: dropped a malformed stream frame', { event: event.type });
    store.set({ status: 'live', lastFrameAt: deps.now() });
  }

  /** A drop, an HTTP failure or 45 s of silence: stale now, one snapshot, our own backoff. */
  function fail(httpFailure: boolean): void {
    closeSource();
    // Before the snapshot, so its spaces replace a map the stream no longer keeps current.
    apply({ kind: 'lost' });
    store.set({ status: 'stale' });
    fetchSnapshot();
    const delay = nextDelay(attempt, deps.random, httpFailure ? HTTP_FAILURE_FLOOR_MS : 0);
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
        if (stopped || !(error instanceof ApiError) || error.code !== 'lot.not_found') return;
        stop();
        for (const handler of new Set(goneHandlers)) handler();
      },
    );
  }

  function stop(): void {
    stopped = true;
    closeSource();
    clearTimeout(retryTimer);
    retryPending = false;
  }

  const onPageHide = () => {
    if (stopped || closedByPageHide) return;
    closedByPageHide = true;
    closeSource();
    clearTimeout(retryTimer);
    retryPending = false;
    apply({ kind: 'lost' });
    if (store.get().status === 'live') store.set({ status: 'stale' });
  };
  // `pageshow` also fires on the first load; only reopen what `pagehide` closed.
  const onPageShow = () => {
    if (!closedByPageHide) return;
    closedByPageHide = false;
    open();
  };

  function teardown(): void {
    if (tornDown) return;
    tornDown = true;
    stop();
    clearTimeout(idleTimer);
    deps.target.removeEventListener('pagehide', onPageHide);
    deps.target.removeEventListener('pageshow', onPageShow);
    store.set(CONNECTING);
    lots.delete(key);
    pruneStore(key);
  }

  deps.target.addEventListener('pagehide', onPageHide);
  deps.target.addEventListener('pageshow', onPageShow);
  open();

  return {
    teardown,
    acquire(onLotGone) {
      refs++;
      clearTimeout(idleTimer);
      if (!source && !retryPending) open();
      goneHandlers.push(onLotGone);
      let released = false;
      return {
        store,
        release() {
          // A release that outlives its connection must not touch a newer one under the same key.
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
 * Shares one connection per `(origin, lotId)`, reference-counted, closed `IDLE_CLOSE_MS` after the
 * last release (M2B-20). The first acquirer's deps drive the connection; `onLotGone` is per caller.
 * Never sets `withCredentials`: the API sends no credentialed CORS headers (M2B-11).
 */
export function acquireLiveLot(
  origin: string,
  lotId: string,
  queryClient: QueryClient,
  deps: Partial<LiveDeps> = {},
): { store: ConnectionStore; release(): void } {
  const key = keyOf(origin, lotId);
  let lot = lots.get(key);
  if (!lot) {
    lot = createLiveLot(key, origin, lotId, queryClient, {
      EventSourceImpl: deps.EventSourceImpl ?? globalThis.EventSource,
      fetchSnapshot: deps.fetchSnapshot ?? fetchLotSnapshot,
      random: deps.random ?? Math.random,
      now: deps.now ?? (() => Date.now()),
      target: deps.target ?? window,
      log: deps.log ?? (process.env.NODE_ENV === 'development' ? (message, detail) => console.debug(message, detail) : noop),
    });
    lots.set(key, lot);
  }
  return lot.acquire(deps.onLotGone ?? noop);
}

/** Closes and forgets every connection and store. */
export function resetLiveLotsForTests(): void {
  for (const lot of [...lots.values()]) lot.teardown();
  lots.clear();
  stores.clear();
}
