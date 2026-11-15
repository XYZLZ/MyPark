import { ApiError, lotQueryKeys, type Availability, type SpaceState } from '@mypark/contracts';
import { availabilityReducedFixture, spacesFixture, spacesFullEventFixture } from '@mypark/contracts/fixtures';
import { QueryClient } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { FakeEventSource } from '@/testing/event-source';
import { acquireLiveLot, resetLiveLotsForTests, type LiveDeps } from './manager';

const ORIGIN = 'http://api.test';
const LOT = 'L1';
const v = (version: number, available = 211): Availability => ({ ...availabilityReducedFixture, version, available });

let queryClient: QueryClient;
let target: EventTarget;
let fetchSnapshot: ReturnType<typeof vi.fn<LiveDeps['fetchSnapshot']>>;
let onLotGone: ReturnType<typeof vi.fn<() => void>>;

beforeEach(() => {
  vi.useFakeTimers();
  queryClient = new QueryClient();
  target = new EventTarget();
  fetchSnapshot = vi.fn<LiveDeps['fetchSnapshot']>(async () => ({ availability: v(125, 200), spaces: spacesFixture.spaces }));
  onLotGone = vi.fn<() => void>();
});

afterEach(() => {
  resetLiveLotsForTests();
  FakeEventSource.instances = [];
  vi.useRealTimers();
});

const acquire = () =>
  acquireLiveLot(ORIGIN, LOT, queryClient, {
    EventSourceImpl: FakeEventSource,
    fetchSnapshot,
    random: () => 1,
    now: () => Date.now(),
    onLotGone,
    target,
  });

const instances = () => FakeEventSource.instances;
const es = (n: number) => instances()[n]!;
const cachedAvailability = () => queryClient.getQueryData<Availability>(lotQueryKeys.availability(LOT));
const cachedSpaces = () => queryClient.getQueryData<SpaceState[]>(lotQueryKeys.spaces(LOT));

describe('acquireLiveLot', () => {
  test('opens one EventSource at the stream url without credentials', () => {
    const { store } = acquire();
    expect(instances()).toHaveLength(1);
    expect(es(0).url).toBe('http://api.test/v1/lots/L1/availability/stream?include=spaces');
    expect(es(0).withCredentials).toBe(false);
    expect(store.get()).toEqual({ status: 'connecting', lastFrameAt: null, spacesReady: false });
  });

  test('the first frame makes it live and writes the cache', () => {
    const { store } = acquire();
    es(0).open();
    es(0).emit('availability', v(121), 121);
    expect(store.get()).toMatchObject({ status: 'live', lastFrameAt: Date.now() });
    expect(cachedAvailability()?.version).toBe(121);
    es(0).emit('spaces', spacesFullEventFixture);
    expect(cachedSpaces()).toEqual(spacesFixture.spaces);
    expect(store.get().spacesReady).toBe(true);
  });

  test('it seeds from the Query cache and an unchanged frame writes nothing', () => {
    queryClient.setQueryData(lotQueryKeys.availability(LOT), v(121));
    const seeded = cachedAvailability();
    acquire();
    es(0).emit('availability', v(121, 5));
    expect(cachedAvailability()).toBe(seeded);
  });

  test('a lower version does not move the count', () => {
    acquire();
    es(0).emit('availability', v(121));
    es(0).emit('availability', v(119, 5));
    expect(cachedAvailability()).toMatchObject({ version: 121, available: 211 });
  });

  test('a malformed frame counts as fresh', () => {
    const { store } = acquire();
    es(0).emit('availability', v(121));
    vi.advanceTimersByTime(30_000);
    es(0).emit('availability', 'not json');
    vi.advanceTimersByTime(44_999);
    expect(store.get().status).toBe('live');
    expect(instances()).toHaveLength(1);
  });

  test('45 s silence goes stale and reconnects', () => {
    const { store } = acquire();
    vi.advanceTimersByTime(45_000);
    expect(store.get().status).toBe('stale');
    expect(es(0).closed).toBe(true);
    vi.advanceTimersByTime(1_000);
    expect(instances()).toHaveLength(2);
  });

  test('error while CONNECTING backs off from 1 s', () => {
    const { store } = acquire();
    es(0).fail(0);
    expect(store.get().status).toBe('stale');
    expect(es(0).closed).toBe(true);
    vi.advanceTimersByTime(999);
    expect(instances()).toHaveLength(1);
    vi.advanceTimersByTime(1);
    expect(instances()).toHaveLength(2);
  });

  test('a reconnect stays stale until its first frame, which resets the backoff', () => {
    const { store } = acquire();
    es(0).fail(0);
    vi.advanceTimersByTime(1_000);
    expect(store.get().status).toBe('stale');
    es(1).fail(0);
    vi.advanceTimersByTime(1_999);
    expect(instances()).toHaveLength(2);
    vi.advanceTimersByTime(1);
    es(2).emit('heartbeat', { as_of: '2026-11-15T14:47:15Z' });
    expect(store.get().status).toBe('live');
    es(2).fail(0);
    vi.advanceTimersByTime(1_000);
    expect(instances()).toHaveLength(4);
  });

  test('error while CLOSED waits 10 s and fetches one BFF snapshot', () => {
    acquire();
    es(0).fail(2);
    expect(fetchSnapshot).toHaveBeenCalledTimes(1);
    expect(fetchSnapshot).toHaveBeenCalledWith(LOT);
    vi.advanceTimersByTime(9_999);
    expect(instances()).toHaveLength(1);
    vi.advanceTimersByTime(1);
    expect(instances()).toHaveLength(2);
  });

  test('a snapshot runs at most once per attempt and never within 10 s of the last', async () => {
    acquire();
    es(0).fail(0);
    expect(fetchSnapshot).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1_000);
    es(1).fail(0);
    await vi.advanceTimersByTimeAsync(2_000);
    es(2).fail(0);
    expect(fetchSnapshot).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(4_000);
    es(3).fail(2);
    expect(fetchSnapshot).toHaveBeenCalledTimes(1); // 7 s after the last one
    await vi.advanceTimersByTimeAsync(10_000);
    es(4).fail(0);
    expect(fetchSnapshot).toHaveBeenCalledTimes(2);
  });

  test('a stale snapshot updates values but the state stays stale', async () => {
    const { store } = acquire();
    es(0).emit('availability', v(121));
    es(0).fail(2);
    await vi.advanceTimersByTimeAsync(0);
    expect(cachedAvailability()).toMatchObject({ version: 125, available: 200 });
    expect(cachedSpaces()).toEqual(spacesFixture.spaces);
    expect(store.get()).toMatchObject({ status: 'stale', lastFrameAt: Date.now() });
  });

  test('snapshot 404 lot.not_found calls onLotGone once and stops', async () => {
    fetchSnapshot.mockRejectedValue(new ApiError({ code: 'lot.not_found', message: 'gone', status: 404 }));
    acquire();
    es(0).fail(2);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(onLotGone).toHaveBeenCalledTimes(1);
    expect(instances()).toHaveLength(1);
  });

  test('a failed snapshot for another reason keeps reconnecting', async () => {
    fetchSnapshot.mockRejectedValue(new ApiError({ code: 'client.network', message: 'down' }));
    acquire();
    es(0).fail(2);
    await vi.advanceTimersByTimeAsync(10_000);
    expect(onLotGone).not.toHaveBeenCalled();
    expect(instances()).toHaveLength(2);
  });

  test('two subscribers share one connection', () => {
    const a = acquire();
    const b = acquire();
    expect(instances()).toHaveLength(1);
    expect(b.store).toBe(a.store);
  });

  test('the last release closes after 2 s', () => {
    const a = acquire();
    const b = acquire();
    a.release();
    b.release();
    b.release();
    vi.advanceTimersByTime(1_999);
    expect(es(0).closed).toBe(false);
    vi.advanceTimersByTime(1);
    expect(es(0).closed).toBe(true);
    vi.advanceTimersByTime(60_000);
    expect(instances()).toHaveLength(1);
  });

  test('a new subscriber within 2 s keeps the connection', () => {
    acquire().release();
    vi.advanceTimersByTime(1_500);
    acquire();
    vi.advanceTimersByTime(5_000);
    expect(es(0).closed).toBe(false);
    expect(instances()).toHaveLength(1);
  });

  test('a late release from a torn-down connection leaves the new one open', () => {
    const stale = acquire();
    resetLiveLotsForTests();
    acquire();
    stale.release();
    vi.advanceTimersByTime(5_000);
    expect(es(1).closed).toBe(false);
    acquire();
    expect(instances()).toHaveLength(2);
  });

  test('pagehide closes and pageshow reopens', () => {
    const { store } = acquire();
    target.dispatchEvent(new Event('pageshow'));
    expect(instances()).toHaveLength(1);
    es(0).emit('spaces', spacesFullEventFixture);
    target.dispatchEvent(new Event('pagehide'));
    expect(es(0).closed).toBe(true);
    expect(store.get().status).toBe('stale');
    vi.advanceTimersByTime(60_000);
    expect(instances()).toHaveLength(1);
    target.dispatchEvent(new Event('pageshow'));
    expect(instances()).toHaveLength(2);
    expect(store.get().spacesReady).toBe(false);
  });
});
