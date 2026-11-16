import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { ApiError } from '../envelope';
import { availabilityReducedFixture, spacesFixture, spacesFullEventFixture } from '../fixtures';
import type { SseFrame } from '../sse/parser';
import type { Availability } from './availability';
import { lotQueryKeys } from './live';
import { createLiveLotRegistry, type LiveAdapters, type LiveCache } from './registry';
import type { SpaceState } from './spaces';

/** Transport-agnostic rules of the live manager (design §5.4–§5.5), shared by web and mobile. */

const ORIGIN = 'http://api.test';
const LOT = 'L1';
const v = (version: number, available = 211): Availability => ({ ...availabilityReducedFixture, version, available });
const freeSpaces: SpaceState[] = spacesFixture.spaces.map((s) => ({ ...s, state: 'free' }));

/** `'drop'` a network drop, `'http'` an HTTP failure (10 s floor), `'gone'` a lot.not_found; a number is a Retry-After in ms. */
type End = 'drop' | 'http' | 'gone' | number;
type Transport = { url: string; closed: boolean; frame(event: string, data: unknown): void; end(end: End): void };

const registry = createLiveLotRegistry();
let transports: Transport[];
let hide: () => void;
let show: () => void;
let lifecycleSubscribed: number;
let cacheData: Map<string, unknown>;
let cache: LiveCache;
let fetchSnapshot: ReturnType<typeof vi.fn<LiveAdapters<End>['fetchSnapshot']>>;
let onLotGone: ReturnType<typeof vi.fn<() => void>>;
let log: ReturnType<typeof vi.fn<LiveAdapters<End>['log']>>;

beforeEach(() => {
  vi.useFakeTimers();
  transports = [];
  lifecycleSubscribed = 0;
  cacheData = new Map();
  cache = {
    getQueryData: (key) => cacheData.get(JSON.stringify(key)),
    setQueryData: (key, data) => cacheData.set(JSON.stringify(key), data),
  };
  fetchSnapshot = vi.fn<LiveAdapters<End>['fetchSnapshot']>(async () => ({ availability: v(125, 200), spaces: freeSpaces }));
  onLotGone = vi.fn<() => void>();
  log = vi.fn<LiveAdapters<End>['log']>();
});

afterEach(() => {
  registry.reset();
  vi.useRealTimers();
});

const adapters = (): LiveAdapters<End> => ({
  openTransport(url, { onFrame, onEnd }) {
    const t: Transport = {
      url,
      closed: false,
      // The fake keeps delivering after close, so the core's own guard is what is tested.
      frame: (event, data) => onFrame({ event, data: typeof data === 'string' ? data : JSON.stringify(data) } satisfies SseFrame),
      end: (end) => onEnd(end),
    };
    transports.push(t);
    return () => {
      t.closed = true;
    };
  },
  classifyEnd: (end) => ({
    floorMs: end === 'drop' ? 0 : 10_000,
    retryAfterMs: typeof end === 'number' ? end : null,
    gone: end === 'gone',
  }),
  subscribeLifecycle(onHide, onShow) {
    hide = onHide;
    show = onShow;
    lifecycleSubscribed++;
    return () => lifecycleSubscribed--;
  },
  fetchSnapshot,
  timers: { set: (callback, ms) => setTimeout(callback, ms), clear: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>) },
  random: () => 1,
  now: () => Date.now(),
  log,
});

const acquire = () => registry.acquire(ORIGIN, LOT, cache, adapters, onLotGone);
const t = (n: number) => transports[n]!;
const cachedAvailability = () => cacheData.get(JSON.stringify(lotQueryKeys.availability(LOT))) as Availability | undefined;
const cachedSpaces = () => cacheData.get(JSON.stringify(lotQueryKeys.spaces(LOT))) as SpaceState[] | undefined;

describe('frames', () => {
  test('opens the stream url and the first frame makes it live', () => {
    const { store } = acquire();
    expect(t(0).url).toBe('http://api.test/v1/lots/L1/availability/stream?include=spaces');
    expect(store.get()).toEqual({ status: 'connecting', lastFrameAt: null, spacesReady: false });
    t(0).frame('availability', v(121));
    expect(store.get()).toMatchObject({ status: 'live', lastFrameAt: Date.now() });
    expect(cachedAvailability()?.version).toBe(121);
    t(0).frame('spaces', spacesFullEventFixture);
    expect(cachedSpaces()).toEqual(spacesFixture.spaces);
    expect(store.get().spacesReady).toBe(true);
  });

  test('it seeds from the cache and an unchanged frame writes nothing', () => {
    cache.setQueryData(lotQueryKeys.availability(LOT), v(121));
    const seeded = cachedAvailability();
    acquire();
    t(0).frame('availability', v(121, 5));
    expect(cachedAvailability()).toBe(seeded);
  });

  test('a lower version does not move the count', () => {
    acquire();
    t(0).frame('availability', v(121));
    t(0).frame('availability', v(119, 5));
    expect(cachedAvailability()).toMatchObject({ version: 121, available: 211 });
  });

  test('a malformed frame counts as fresh and is logged; a valid one is not', () => {
    const { store } = acquire();
    t(0).frame('availability', v(121));
    vi.advanceTimersByTime(30_000);
    t(0).frame('availability', 'not json');
    expect(log).toHaveBeenCalledTimes(1);
    expect(log.mock.calls[0]![0]).toMatch(/dropped/);
    vi.advanceTimersByTime(44_999);
    expect(store.get().status).toBe('live');
    expect(cachedAvailability()?.available).toBe(211);
    t(0).frame('heartbeat', { as_of: '2026-11-15T14:47:15Z' });
    expect(log).toHaveBeenCalledTimes(1);
    expect(transports).toHaveLength(1);
  });
});

describe('reconnect', () => {
  test('45 s silence goes stale, closes and reconnects', () => {
    const { store } = acquire();
    vi.advanceTimersByTime(45_000);
    expect(store.get().status).toBe('stale');
    expect(t(0).closed).toBe(true);
    vi.advanceTimersByTime(1_000);
    expect(transports).toHaveLength(2);
  });

  test('a late end or frame from a closed transport is ignored', () => {
    const { store } = acquire();
    vi.advanceTimersByTime(45_000);
    t(0).end('http');
    t(0).frame('availability', v(130));
    vi.advanceTimersByTime(1_000);
    expect(transports).toHaveLength(2);
    expect(store.get().status).toBe('stale');
    expect(cachedAvailability()).toBeUndefined();
  });

  test('a reconnect stays stale until its first frame, which resets the backoff', () => {
    const { store } = acquire();
    t(0).end('drop');
    vi.advanceTimersByTime(1_000);
    expect(store.get().status).toBe('stale');
    t(1).end('drop');
    vi.advanceTimersByTime(1_999);
    expect(transports).toHaveLength(2);
    vi.advanceTimersByTime(1);
    t(2).frame('heartbeat', { as_of: '2026-11-15T14:47:15Z' });
    expect(store.get().status).toBe('live');
    t(2).end('drop');
    vi.advanceTimersByTime(1_000);
    expect(transports).toHaveLength(4);
  });

  test('an HTTP failure waits the 10 s floor; a Retry-After waits when larger', () => {
    acquire();
    t(0).end('http');
    vi.advanceTimersByTime(9_999);
    expect(transports).toHaveLength(1);
    vi.advanceTimersByTime(1);
    t(1).end(20_000);
    vi.advanceTimersByTime(19_999);
    expect(transports).toHaveLength(2);
    vi.advanceTimersByTime(1);
    expect(transports).toHaveLength(3);
  });

  test('a retry due after the last release does not reconnect; a new subscriber does', () => {
    const first = acquire();
    t(0).end('drop');
    first.release();
    vi.advanceTimersByTime(1_500);
    expect(transports).toHaveLength(1);
    acquire();
    expect(transports).toHaveLength(2);
  });

  test('a retry is never opened once nobody subscribes', () => {
    const first = acquire();
    t(0).end('drop');
    first.release();
    vi.advanceTimersByTime(60_000);
    expect(transports).toHaveLength(1);
  });
});

describe('REST snapshot', () => {
  test('a snapshot runs at most once per attempt and never within 10 s of the last', async () => {
    acquire();
    t(0).end('drop');
    expect(fetchSnapshot).toHaveBeenCalledTimes(1);
    expect(fetchSnapshot).toHaveBeenCalledWith(LOT);
    await vi.advanceTimersByTimeAsync(1_000);
    t(1).end('drop');
    await vi.advanceTimersByTimeAsync(2_000);
    t(2).end('drop');
    expect(fetchSnapshot).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(4_000);
    t(3).end('http');
    expect(fetchSnapshot).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(10_000);
    t(4).end('drop');
    expect(fetchSnapshot).toHaveBeenCalledTimes(2);
  });

  test('a lost stream marks the map stale before the snapshot replaces it; values move, the state stays stale', async () => {
    const { store } = acquire();
    t(0).frame('availability', v(121));
    t(0).frame('spaces', spacesFullEventFixture);
    t(0).end('drop');
    expect(store.get().spacesReady).toBe(false);
    await vi.advanceTimersByTimeAsync(0);
    expect(cachedAvailability()).toMatchObject({ version: 125, available: 200 });
    expect(cachedSpaces()).toEqual(freeSpaces);
    expect(store.get()).toMatchObject({ status: 'stale', lastFrameAt: Date.now(), spacesReady: false });
    await vi.advanceTimersByTimeAsync(1_000);
    t(1).frame('spaces', spacesFullEventFixture);
    expect(store.get().spacesReady).toBe(true);
    expect(cachedSpaces()).toEqual(spacesFixture.spaces);
  });

  test('an older snapshot after a newer stream frame keeps the newer count', async () => {
    fetchSnapshot.mockResolvedValue({ availability: v(119, 5), spaces: spacesFixture.spaces });
    acquire();
    t(0).frame('availability', v(121));
    t(0).end('drop');
    await vi.advanceTimersByTimeAsync(0);
    expect(cachedAvailability()).toMatchObject({ version: 121, available: 211 });
  });

  test('a failed snapshot for another reason keeps reconnecting', async () => {
    fetchSnapshot.mockRejectedValue(new ApiError({ code: 'client.network', message: 'down' }));
    acquire();
    t(0).end('http');
    await vi.advanceTimersByTimeAsync(10_000);
    expect(onLotGone).not.toHaveBeenCalled();
    expect(transports).toHaveLength(2);
  });
});

describe('lot gone', () => {
  test('a gone end stops, goes stale and calls every onLotGone once', async () => {
    const other = vi.fn<() => void>();
    const { store } = acquire();
    registry.acquire(ORIGIN, LOT, cache, adapters, other);
    t(0).end('gone');
    await vi.advanceTimersByTimeAsync(60_000);
    expect(onLotGone).toHaveBeenCalledTimes(1);
    expect(other).toHaveBeenCalledTimes(1);
    expect(transports).toHaveLength(1);
    expect(store.get().status).toBe('stale');
  });

  test('a snapshot lot.not_found stops and calls onLotGone once', async () => {
    fetchSnapshot.mockRejectedValue(new ApiError({ code: 'lot.not_found', message: 'gone', status: 404 }));
    acquire();
    t(0).end('http');
    await vi.advanceTimersByTimeAsync(60_000);
    expect(onLotGone).toHaveBeenCalledTimes(1);
    expect(transports).toHaveLength(1);
  });

  test('the next acquire after a gone lot builds a fresh one on the store a reader still holds', () => {
    const first = acquire();
    first.store.subscribe(() => {});
    t(0).end('gone');
    expect(lifecycleSubscribed).toBe(0);
    const second = acquire();
    expect(transports).toHaveLength(2);
    expect(second.store).toBe(first.store);
    // The gone lot's release is a no-op and cannot close the new one.
    first.release();
    vi.advanceTimersByTime(5_000);
    expect(t(1).closed).toBe(false);
  });
});

describe('sharing', () => {
  test('two subscribers share one connection; the last release closes after 2 s', () => {
    const a = acquire();
    const b = acquire();
    expect(transports).toHaveLength(1);
    expect(b.store).toBe(a.store);
    a.release();
    b.release();
    b.release();
    vi.advanceTimersByTime(1_999);
    expect(t(0).closed).toBe(false);
    vi.advanceTimersByTime(1);
    expect(t(0).closed).toBe(true);
    expect(lifecycleSubscribed).toBe(0);
    vi.advanceTimersByTime(60_000);
    expect(transports).toHaveLength(1);
  });

  test('a new subscriber within 2 s keeps the connection', () => {
    acquire().release();
    vi.advanceTimersByTime(1_500);
    acquire();
    vi.advanceTimersByTime(5_000);
    expect(t(0).closed).toBe(false);
    expect(transports).toHaveLength(1);
  });

  test('a late release from a torn-down connection leaves the new one open', () => {
    const stale = acquire();
    registry.reset();
    acquire();
    stale.release();
    vi.advanceTimersByTime(5_000);
    expect(t(1).closed).toBe(false);
    acquire();
    expect(transports).toHaveLength(2);
  });

  test('a torn-down lot forgets its store once nothing listens', () => {
    const first = acquire();
    const unsubscribe = first.store.subscribe(() => {});
    first.release();
    vi.advanceTimersByTime(2_000);
    expect(registry.store(ORIGIN, LOT)).toBe(first.store);
    unsubscribe();
    expect(registry.store(ORIGIN, LOT)).not.toBe(first.store);
    const second = acquire();
    second.release();
    vi.advanceTimersByTime(2_000);
    expect(registry.store(ORIGIN, LOT)).not.toBe(second.store);
  });
});

describe('lifecycle', () => {
  test('hide closes and marks stale; show reopens and stays stale until the first frame', () => {
    const { store } = acquire();
    show();
    expect(transports).toHaveLength(1);
    t(0).frame('spaces', spacesFullEventFixture);
    hide();
    expect(t(0).closed).toBe(true);
    expect(store.get()).toMatchObject({ status: 'stale', spacesReady: false });
    vi.advanceTimersByTime(60_000);
    expect(transports).toHaveLength(1);
    show();
    expect(transports).toHaveLength(2);
    expect(store.get()).toMatchObject({ status: 'stale', spacesReady: false });
    t(1).frame('heartbeat', { as_of: '2026-11-15T14:47:15Z' });
    expect(store.get().status).toBe('live');
  });

  test('repeated hide and show events open exactly one new connection', () => {
    acquire();
    hide();
    hide();
    show();
    show();
    expect(transports).toHaveLength(2);
  });

  test('show before the retry is due waits the rest of the delay and stays stale (M2B-5)', () => {
    const { store } = acquire();
    t(0).end(20_000);
    vi.advanceTimersByTime(1_000);
    hide();
    show();
    expect(store.get().status).toBe('stale');
    vi.advanceTimersByTime(18_999);
    expect(transports).toHaveLength(1);
    vi.advanceTimersByTime(1);
    expect(transports).toHaveLength(2);
  });

  test('an acquire while hidden opens nothing until shown', () => {
    const lifecycle = adapters();
    const hiddenAtStart = (): LiveAdapters<End> => ({
      ...lifecycle,
      subscribeLifecycle(onHide, onShow) {
        const unsubscribe = lifecycle.subscribeLifecycle(onHide, onShow);
        onHide();
        return unsubscribe;
      },
    });
    const { store } = registry.acquire(ORIGIN, LOT, cache, hiddenAtStart, onLotGone);
    expect(transports).toHaveLength(0);
    expect(store.get().status).toBe('connecting');
    show();
    expect(transports).toHaveLength(1);
  });
});
