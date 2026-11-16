import { lotQueryKeys, type Availability, type SpaceState } from '@mypark/contracts';
import { availabilityReducedFixture, errorBody, spacesFixture, spacesFullEventFixture, sseText } from '@mypark/contracts/fixtures';
import { QueryClient } from '@tanstack/react-query';
import type { AppStateStatus } from 'react-native';

import type { StreamFetch, StreamResponse } from '@/services/sse';
import { controllableStream, type ControllableStream } from '@/testing/stream';

import { acquireLiveLot, resetLiveLotsForTests, type MobileLiveDeps } from './manager';

/** Mobile adapters only: how `expo/fetch` ends are classified and how `AppState` drives the stream. The shared rules are tested in contracts (m2/registry.test.ts). */

const ORIGIN = 'http://api.test';
const LOT = 'L1';
const v = (version: number, available = 211): Availability => ({ ...availabilityReducedFixture, version, available });
const freeSpaces: SpaceState[] = spacesFixture.spaces.map((s) => ({ ...s, state: 'free' }));

let queryClient: QueryClient;
let stream: ControllableStream;
let appStateListeners: Set<(state: AppStateStatus) => void>;
let currentState: AppStateStatus;
let fetchSnapshot: jest.MockedFunction<MobileLiveDeps['fetchSnapshot']>;
let onLotGone: jest.Mock<void, []>;
let log: jest.MockedFunction<MobileLiveDeps['log']>;

beforeEach(() => {
  jest.useFakeTimers();
  queryClient = new QueryClient();
  stream = controllableStream();
  appStateListeners = new Set();
  currentState = 'active';
  fetchSnapshot = jest.fn(async (_lotId: string) => ({ availability: v(125, 200), spaces: freeSpaces }));
  onLotGone = jest.fn();
  log = jest.fn();
});

afterEach(() => {
  resetLiveLotsForTests();
  queryClient.clear();
  jest.useRealTimers();
});

const appState: MobileLiveDeps['appState'] = {
  get currentState() {
    return currentState;
  },
  addEventListener(_type, listener) {
    appStateListeners.add(listener);
    return { remove: () => appStateListeners.delete(listener) };
  },
};
const setAppState = (state: AppStateStatus) => {
  currentState = state;
  appStateListeners.forEach((listener) => listener(state));
};

const acquire = (fetchImpl: StreamFetch = stream.fetchImpl) =>
  acquireLiveLot(ORIGIN, LOT, queryClient, {
    fetchImpl,
    random: () => 1,
    now: () => Date.now(),
    appState,
    fetchSnapshot,
    onLotGone,
    log,
  });

/** Runs due timers and lets the transport's promises settle. */
const advance = (ms: number) => jest.advanceTimersByTimeAsync(ms);
const calls = () => stream.calls.length;
const cachedAvailability = () => queryClient.getQueryData<Availability>(lotQueryKeys.availability(LOT));
const cachedSpaces = () => queryClient.getQueryData<SpaceState[]>(lotQueryKeys.spaces(LOT));

/** Waits the 10 s HTTP floor exactly: no call at 9 999 ms, one at 10 000 ms. */
async function expectFloorWait() {
  await advance(0);
  await advance(9_999);
  expect(calls()).toBe(1);
  await advance(1);
  expect(calls()).toBe(2);
}

test('the first frame makes it live and writes the cache', async () => {
  const { store } = acquire();
  expect(stream.calls[0]?.url).toBe('http://api.test/v1/lots/L1/availability/stream?include=spaces');
  expect(store.get()).toEqual({ status: 'connecting', lastFrameAt: null, spacesReady: false });
  stream.push(sseText('availability', v(121), 121));
  await advance(0);
  expect(store.get()).toMatchObject({ status: 'live', lastFrameAt: Date.now() });
  expect(cachedAvailability()?.version).toBe(121);
  stream.push(sseText('spaces', spacesFullEventFixture));
  await advance(0);
  expect(cachedSpaces()).toEqual(spacesFixture.spaces);
  expect(store.get().spacesReady).toBe(true);
});

test('a malformed frame keeps the stream live', async () => {
  const { store } = acquire();
  stream.push(sseText('availability', v(121)));
  await advance(0);
  stream.push('event: availability\ndata: oops\n\n');
  await advance(0);
  expect(store.get().status).toBe('live');
  expect(cachedAvailability()?.available).toBe(211);
  expect(log).toHaveBeenCalledWith('live: dropped a malformed stream frame', { event: 'availability' });
});

test('a 429 waits Retry-After when it is larger than the backoff', async () => {
  const { store } = acquire();
  stream.respond(429, JSON.stringify(errorBody('rate_limit.exceeded')), { 'Retry-After': '20' });
  await advance(0);
  expect(store.get().status).toBe('stale');
  await advance(19_999);
  expect(calls()).toBe(1);
  await advance(1);
  expect(calls()).toBe(2);
});

test('a 429 with Retry-After soon waits the 10 s floor', async () => {
  acquire();
  stream.respond(429, '<html>Too many</html>', { 'retry-after': 'soon' });
  await expectFloorWait();
});

test('a 5xx waits the 10 s floor', async () => {
  acquire();
  stream.respond(503, JSON.stringify(errorBody('internal.unavailable')));
  await expectFloorWait();
});

test('a 200 that is not an event stream waits the 10 s floor', async () => {
  acquire();
  stream.respond(200, '<html>captive portal</html>', { 'content-type': 'text/html' });
  await expectFloorWait();
});

test('a 404 that is not lot.not_found waits the 10 s floor', async () => {
  acquire();
  stream.respond(404, '<html>Not found</html>');
  await expectFloorWait();
  expect(onLotGone).not.toHaveBeenCalled();
});

test('404 lot.not_found re-discovers the lot once', async () => {
  acquire();
  stream.respond(404, JSON.stringify(errorBody('lot.not_found')));
  await advance(0);
  expect(onLotGone).toHaveBeenCalledTimes(1);
  await advance(60_000);
  expect(calls()).toBe(1);
  expect(onLotGone).toHaveBeenCalledTimes(1);
});

test('end of stream reconnects with backoff from 1 s', async () => {
  const { store } = acquire();
  stream.push(sseText('heartbeat', { as_of: '2026-11-15T14:47:15Z' }));
  await advance(0);
  stream.end();
  await advance(0);
  expect(store.get().status).toBe('stale');
  await advance(999);
  expect(calls()).toBe(1);
  await advance(1);
  expect(calls()).toBe(2);
});

test('45 s without a frame goes stale, aborts and reconnects', async () => {
  const { store } = acquire();
  stream.push(sseText('availability', v(121)));
  await advance(0);
  await advance(44_999);
  expect(store.get().status).toBe('live');
  await advance(1);
  expect(store.get().status).toBe('stale');
  expect(stream.calls[0]?.signal.aborted).toBe(true);
  await advance(1_000);
  expect(calls()).toBe(2);
});

test('a late openStream result after a silence abort is ignored', async () => {
  // A transport that ignores the signal: its response lands after the manager gave up on it.
  let late: (res: StreamResponse) => void = () => {};
  const signals: AbortSignal[] = [];
  const stubborn: StreamFetch = (_url, init) => {
    signals.push(init.signal);
    return new Promise((resolve) => (late = resolve));
  };
  const { store } = acquire(stubborn);
  await advance(45_000);
  expect(signals[0]?.aborted).toBe(true);
  late({ status: 404, headers: { get: () => null }, body: null, text: async () => JSON.stringify(errorBody('lot.not_found')) });
  await advance(1_000);
  expect(onLotGone).not.toHaveBeenCalled();
  expect(signals).toHaveLength(2);
  expect(store.get().status).toBe('stale');
});

test('background aborts the stream; foreground reconnects as connecting until its first frame', async () => {
  const { store } = acquire();
  stream.push(sseText('availability', v(121)) + sseText('spaces', spacesFullEventFixture));
  await advance(0);
  setAppState('background');
  expect(stream.calls[0]?.signal.aborted).toBe(true);
  expect(store.get().status).toBe('stale');
  await advance(60_000);
  expect(calls()).toBe(1);

  setAppState('active');
  expect(calls()).toBe(2);
  expect(store.get()).toMatchObject({ status: 'connecting', spacesReady: false });
  stream.push(sseText('availability', v(130, 190)) + sseText('spaces', spacesFullEventFixture));
  await advance(0);
  expect(store.get()).toMatchObject({ status: 'live', spacesReady: true });
  expect(cachedAvailability()?.available).toBe(190);
});

test('a foreground during a 429 wait keeps waiting for Retry-After (M2B-5)', async () => {
  const { store } = acquire();
  stream.respond(429, JSON.stringify(errorBody('rate_limit.exceeded')), { 'Retry-After': '20' });
  await advance(1_000);
  setAppState('background');
  setAppState('active');
  expect(store.get().status).toBe('stale');
  await advance(18_999);
  expect(calls()).toBe(1);
  await advance(1);
  expect(calls()).toBe(2);
});

test('repeated app-state events open exactly one new stream', async () => {
  acquire();
  setAppState('inactive');
  setAppState('background');
  setAppState('active');
  setAppState('active');
  await advance(0);
  expect(calls()).toBe(2);
});

test('an acquire while the app is not active opens nothing until it is', async () => {
  currentState = 'background';
  const { store } = acquire();
  await advance(60_000);
  expect(calls()).toBe(0);
  expect(store.get().status).toBe('connecting');
  setAppState('active');
  expect(calls()).toBe(1);
});

test('two subscribers share one stream; the last release aborts after 2 s', async () => {
  const first = acquire();
  const second = acquire();
  expect(calls()).toBe(1);
  first.release();
  await advance(5_000);
  expect(stream.calls[0]?.signal.aborted).toBe(false);
  second.release();
  await advance(1_999);
  expect(stream.calls[0]?.signal.aborted).toBe(false);
  await advance(1);
  expect(stream.calls[0]?.signal.aborted).toBe(true);
  expect(appStateListeners.size).toBe(0);
});
