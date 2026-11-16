import { lotQueryKeys, type Availability, type SpaceState } from '@mypark/contracts';
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
let log: ReturnType<typeof vi.fn<LiveDeps['log']>>;

beforeEach(() => {
  vi.useFakeTimers();
  queryClient = new QueryClient();
  target = new EventTarget();
  fetchSnapshot = vi.fn<LiveDeps['fetchSnapshot']>(async () => ({ availability: v(125, 200), spaces: spacesFixture.spaces }));
  onLotGone = vi.fn<() => void>();
  log = vi.fn<LiveDeps['log']>();
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
    log,
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

  test('pagehide closes and pageshow reopens', () => {
    const { store } = acquire();
    target.dispatchEvent(new Event('pageshow'));
    expect(instances()).toHaveLength(1);
    es(0).emit('spaces', spacesFullEventFixture);
    target.dispatchEvent(new Event('pagehide'));
    expect(store.get().spacesReady).toBe(false);
    expect(es(0).closed).toBe(true);
    expect(store.get().status).toBe('stale');
    vi.advanceTimersByTime(60_000);
    expect(instances()).toHaveLength(1);
    target.dispatchEvent(new Event('pageshow'));
    expect(instances()).toHaveLength(2);
    expect(store.get().spacesReady).toBe(false);
  });

  test('pageshow before the 10 s floor is up waits the rest of it (M2B-5)', () => {
    const { store } = acquire();
    es(0).fail(2);
    vi.advanceTimersByTime(1_000);
    target.dispatchEvent(new Event('pagehide'));
    target.dispatchEvent(new Event('pageshow'));
    expect(store.get().status).toBe('stale');
    vi.advanceTimersByTime(8_999);
    expect(instances()).toHaveLength(1);
    vi.advanceTimersByTime(1);
    expect(instances()).toHaveLength(2);
  });

  test('a late error from a closed source is ignored', () => {
    acquire();
    const first = es(0);
    vi.advanceTimersByTime(45_000);
    first.closed = false;
    first.fail(2);
    vi.advanceTimersByTime(1_000);
    expect(instances()).toHaveLength(2);
  });
});
