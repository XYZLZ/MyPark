import {
  createLiveLotRegistry,
  HTTP_FAILURE_FLOOR_MS,
  type Availability,
  type LiveAdapters,
  type SpaceState,
} from '@mypark/contracts';
import type { QueryClient } from '@tanstack/react-query';
import { fetchLotSnapshot } from '../api';
import type { ConnectionStore } from './connection-store';

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

/** `'http'`: `error` with `CLOSED`, which `EventSource` never retries (429, 404). `'network'`: `error` while `CONNECTING`. */
type WebEnd = 'http' | 'network';

const STREAM_EVENTS = ['availability', 'spaces', 'heartbeat'] as const;
/** `EventSource.CLOSED`, which does not exist outside the browser. */
const CLOSED = 2;
const noop = () => {};

/** The shared rules live in contracts (`createLiveLotRegistry`); this module is the browser's adapters. Module state, browser only. */
const registry = createLiveLotRegistry();

/**
 * `EventSource` hides HTTP failures and retries drops with no jitter, so every `error` closes the
 * source and the shared core schedules its own attempt. `pagehide` closes, `pageshow` reopens.
 */
function webAdapters(deps: Partial<LiveDeps>): LiveAdapters<WebEnd> {
  const EventSourceImpl = deps.EventSourceImpl ?? globalThis.EventSource;
  const target = deps.target ?? window;
  return {
    openTransport(url, { onFrame, onEnd }) {
      const es = new EventSourceImpl(url);
      const onEvent = (event: Event) => {
        const { type, data, lastEventId } = event as MessageEvent;
        const text = typeof data === 'string' ? data : '';
        onFrame(lastEventId ? { event: type, id: lastEventId, data: text } : { event: type, data: text });
      };
      // Read readyState before close(), which would make every failure look like CLOSED.
      const onError = () => onEnd(es.readyState === CLOSED ? 'http' : 'network');
      for (const name of STREAM_EVENTS) es.addEventListener(name, onEvent);
      es.addEventListener('error', onError);
      return () => {
        for (const name of STREAM_EVENTS) es.removeEventListener(name, onEvent);
        es.removeEventListener('error', onError);
        es.close();
      };
    },
    classifyEnd: (end) => ({ floorMs: end === 'http' ? HTTP_FAILURE_FLOOR_MS : 0, retryAfterMs: null, gone: false }),
    subscribeLifecycle(onHide, onShow) {
      // `pageshow` also fires on the first load; the core only reopens what a hide closed.
      target.addEventListener('pagehide', onHide);
      target.addEventListener('pageshow', onShow);
      return () => {
        target.removeEventListener('pagehide', onHide);
        target.removeEventListener('pageshow', onShow);
      };
    },
    fetchSnapshot: deps.fetchSnapshot ?? fetchLotSnapshot,
    timers: { set: (callback, ms) => setTimeout(callback, ms), clear: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>) },
    random: deps.random ?? Math.random,
    now: deps.now ?? (() => Date.now()),
    log: deps.log ?? (process.env.NODE_ENV === 'development' ? (message, detail) => console.debug(message, detail) : noop),
  };
}

/** The connection store for a lot. Browser only: the registry is module state. */
export function liveLotStore(origin: string, lotId: string): ConnectionStore {
  return registry.store(origin, lotId);
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
  return registry.acquire(origin, lotId, queryClient, () => webAdapters(deps), deps.onLotGone ?? noop);
}

/** Closes and forgets every connection and store. */
export function resetLiveLotsForTests(): void {
  registry.reset();
}
