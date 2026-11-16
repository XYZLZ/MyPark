import { ApiError } from '../envelope';
import type { SseFrame } from '../sse/parser';
import { streamUrl } from '../sse/url';
import type { Availability } from './availability';
import {
  createConnectionStore,
  decodeFrame,
  FRESH_WINDOW_MS,
  IDLE_CLOSE_MS,
  initialLiveState,
  lotQueryKeys,
  nextDelay,
  reduceLive,
  SNAPSHOT_MIN_INTERVAL_MS,
  type Connection,
  type ConnectionStore,
  type LiveFrame,
  type LiveState,
} from './live';
import type { SpaceState } from './spaces';

/** Before the first frame of a connection. A constant, so `useSyncExternalStore` sees stable snapshots. */
export const CONNECTING: Connection = Object.freeze({ status: 'connecting', lastFrameAt: null, spacesReady: false });

/** What a transport's end means for the manager (design §5.4). */
export type LiveEndPlan = {
  /** The least wait before the next attempt: 0 for a drop, `HTTP_FAILURE_FLOOR_MS` for an HTTP failure. */
  floorMs: number;
  /** A readable `Retry-After`, honored when larger than the backoff. */
  retryAfterMs: number | null;
  /** `lot.not_found`: stop and let the callers re-discover the lot (M2B-7). */
  gone: boolean;
};

export type LiveTimers = { set(callback: () => void, ms: number): unknown; clear(handle: unknown): void };

/** The slice of a TanStack `QueryClient` the manager uses. */
export type LiveCache = {
  getQueryData(key: readonly unknown[]): unknown;
  setQueryData(key: readonly unknown[], data: unknown): unknown;
};

/** The platform parts of a live lot: web `EventSource` and page events, mobile `expo/fetch` and `AppState`. */
export type LiveAdapters<End> = {
  /** Opens the stream; returns its close. Callbacks after close are ignored. */
  openTransport(url: string, handlers: { onFrame(frame: SseFrame): void; onEnd(end: End): void }): () => void;
  classifyEnd(end: End): LiveEndPlan;
  /** May call `onHide` synchronously when the app starts hidden. Returns the unsubscribe. */
  subscribeLifecycle(onHide: () => void, onShow: () => void): () => void;
  fetchSnapshot(lotId: string): Promise<{ availability: Availability; spaces: SpaceState[] }>;
  timers: LiveTimers;
  random: () => number;
  now: () => number;
  /** Dev diagnostics, e.g. a dropped frame (design §5.2). */
  log: (message: string, detail?: unknown) => void;
};

export type LiveLotRegistry = {
  /**
   * Joins the lot's shared connection, creating it with `adapters()` when there is none (M2B-20).
   * `onLotGone` is per caller.
   */
  acquire<End>(
    origin: string,
    lotId: string,
    cache: LiveCache,
    adapters: () => LiveAdapters<End>,
    onLotGone: () => void,
  ): { store: ConnectionStore; release(): void };
  /** The connection store for a lot; it outlives connections so a hook can read it during render. */
  store(origin: string, lotId: string): ConnectionStore;
  /** Closes and forgets every connection and store. */
  reset(): void;
};

type LiveLot = { acquire(onLotGone: () => void): { store: ConnectionStore; release(): void }; teardown(): void };

const SILENCE: LiveEndPlan = { floorMs: 0, retryAfterMs: null, gone: false };

/**
 * The live manager shared by web and mobile (design §5.4, PF16). One connection per
 * `(origin, lotId)`, reference-counted and closed `IDLE_CLOSE_MS` after the last release. It owns
 * reconnection: each end is classified by the platform, then waits `nextDelay` (with the floor and
 * `Retry-After`), and a show never jumps a pending wait (M2B-5). Frames go through `reduceLive`
 * into the cache; the connection state goes into the lot's store.
 */
export function createLiveLotRegistry(): LiveLotRegistry {
  const lots = new Map<string, LiveLot>();
  const stores = new Map<string, { store: ConnectionStore; listeners: number }>();
  const keyOf = (origin: string, lotId: string) => JSON.stringify([origin, lotId]);

  function pruneStore(key: string): void {
    if (!lots.has(key) && stores.get(key)?.listeners === 0) stores.delete(key);
  }

  function storeFor(origin: string, lotId: string): ConnectionStore {
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

  function createLiveLot<End>(key: string, origin: string, lotId: string, cache: LiveCache, deps: LiveAdapters<End>): LiveLot {
    const store = storeFor(origin, lotId);
    const { timers } = deps;
    const goneHandlers: (() => void)[] = [];
    let refs = 0;
    let state: LiveState = initialLiveState({
      availability: cache.getQueryData(lotQueryKeys.availability(lotId)) as Availability | undefined,
      spaces: cache.getQueryData(lotQueryKeys.spaces(lotId)) as SpaceState[] | undefined,
    });
    let close: (() => void) | null = null;
    /** Which transport may still deliver; 0 when none. */
    let current = 0;
    let generation = 0;
    let attempt = 0;
    let receivedFrame = false;
    let lastSnapshotAt = Number.NEGATIVE_INFINITY;
    /** The next attempt may not start before this time, even after a show. */
    let notBefore = Number.NEGATIVE_INFINITY;
    let hidden = false;
    let stopped = false;
    let tornDown = false;
    let freshTimer: unknown;
    let retryTimer: unknown;
    let retryPending = false;
    let idleTimer: unknown;

    /** Writes only what changed; the same reference means no cache write. */
    function apply(frame: LiveFrame): void {
      const next = reduceLive(state, frame);
      if (next === state) return;
      if (next.availability && next.availability !== state.availability) {
        cache.setQueryData(lotQueryKeys.availability(lotId), next.availability);
      }
      if (next.spaces && next.spaces !== state.spaces) cache.setQueryData(lotQueryKeys.spaces(lotId), next.spaces);
      state = next;
      store.set({ spacesReady: next.spacesReady });
    }

    function armFreshTimer(): void {
      timers.clear(freshTimer);
      // A half-open connection never ends; the silence timer is what recovers it (M2B-13).
      freshTimer = timers.set(() => fail(SILENCE), FRESH_WINDOW_MS);
    }

    function closeTransport(): void {
      timers.clear(freshTimer);
      current = 0;
      const closing = close;
      close = null;
      closing?.();
    }

    function cancelRetry(): void {
      timers.clear(retryTimer);
      retryPending = false;
    }

    function scheduleRetry(ms: number): void {
      retryPending = true;
      // With no subscribers left (the idle close is pending) the retry is skipped; a new acquire opens.
      retryTimer = timers.set(() => {
        retryPending = false;
        if (refs > 0) open();
      }, ms);
    }

    function open(): void {
      cancelRetry();
      if (stopped || hidden || close) return;
      const id = ++generation;
      current = id;
      receivedFrame = false;
      apply({ kind: 'open' });
      armFreshTimer();
      close = deps.openTransport(streamUrl(origin, lotId), {
        onFrame: (frame) => {
          if (current === id) receive(frame);
        },
        onEnd: (end) => {
          if (current === id) ended(deps.classifyEnd(end));
        },
      });
    }

    /** Opens now, or waits out what is left of the last delay (M2B-5). */
    function resume(): void {
      if (stopped || hidden || close || retryPending) return;
      const wait = notBefore - deps.now();
      if (wait > 0) scheduleRetry(wait);
      else open();
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

    function ended(plan: LiveEndPlan): void {
      if (plan.gone) lotGone();
      else fail(plan);
    }

    /** A drop, an HTTP failure or 45 s of silence: stale now, one snapshot, our own backoff. */
    function fail(plan: LiveEndPlan): void {
      closeTransport();
      // Before the snapshot, so its spaces replace a map the stream no longer keeps current.
      apply({ kind: 'lost' });
      store.set({ status: 'stale' });
      fetchSnapshot();
      const delay = Math.max(plan.retryAfterMs ?? 0, nextDelay(attempt, deps.random, plan.floorMs));
      attempt++;
      notBefore = deps.now() + delay;
      scheduleRetry(delay);
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

    function stop(): void {
      stopped = true;
      closeTransport();
      cancelRetry();
    }

    /** Stops for good and leaves the registry, so a later acquire builds a fresh lot on the same store. */
    function lotGone(): void {
      if (stopped) return;
      stop();
      apply({ kind: 'lost' });
      store.set({ status: 'stale' });
      forget();
      for (const handler of new Set(goneHandlers)) handler();
    }

    function forget(): void {
      tornDown = true;
      timers.clear(idleTimer);
      unsubscribeLifecycle();
      if (lots.get(key) === lot) lots.delete(key);
      pruneStore(key);
    }

    function teardown(): void {
      if (tornDown) return;
      stop();
      store.set(CONNECTING);
      forget();
    }

    const onHide = () => {
      if (stopped || hidden) return;
      hidden = true;
      closeTransport();
      cancelRetry();
      apply({ kind: 'lost' });
      if (store.get().status === 'live') store.set({ status: 'stale' });
    };
    const onShow = () => {
      if (!hidden) return;
      hidden = false;
      // With no subscribers left the idle close is pending; a new acquire opens.
      if (stopped || refs === 0) return;
      if (deps.now() >= notBefore) store.set({ status: 'connecting' });
      resume();
    };

    const lot: LiveLot = {
      teardown,
      acquire(onLotGone) {
        refs++;
        timers.clear(idleTimer);
        resume();
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
            if (refs === 0) idleTimer = timers.set(teardown, IDLE_CLOSE_MS);
          },
        };
      },
    };
    const unsubscribeLifecycle = deps.subscribeLifecycle(onHide, onShow);
    return lot;
  }

  return {
    acquire(origin, lotId, cache, adapters, onLotGone) {
      const key = keyOf(origin, lotId);
      let lot = lots.get(key);
      if (!lot) {
        lot = createLiveLot(key, origin, lotId, cache, adapters());
        lots.set(key, lot);
      }
      return lot.acquire(onLotGone);
    },
    store: storeFor,
    reset() {
      for (const lot of [...lots.values()]) lot.teardown();
      lots.clear();
      stores.clear();
    },
  };
}
