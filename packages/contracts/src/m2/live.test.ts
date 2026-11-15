import { describe, expect, test, vi } from 'vitest';
import { availabilityReducedFixture, spacesDeltaFixture, spacesFixture, spacesFullEventFixture } from '../fixtures';
import type { Availability } from './availability';
import {
  availabilityFigures,
  countSpaces,
  createConnectionStore,
  decodeFrame,
  initialLiveState,
  lotQueryKeys,
  nextDelay,
  parseRetryAfter,
  planAnnouncement,
  reduceLive,
  type AnnounceInput,
  type LiveState,
} from './live';

const v = (version: number, available = 211): Availability => ({ ...availabilityReducedFixture, version, available });
const stateOf = (spaces: LiveState['spaces'], code: string) => spaces?.find((s) => s.code === code)?.state;
const ready = (): LiveState => reduceLive(initialLiveState(), { kind: 'spaces', data: spacesFullEventFixture });

describe('reduceLive', () => {
  test('availability with a version at or below the last is ignored', () => {
    const s = reduceLive(initialLiveState(), { kind: 'availability', data: v(120) });
    expect(s.lastVersion).toBe(120);
    expect(reduceLive(s, { kind: 'availability', data: v(120, 5) })).toBe(s);
    expect(reduceLive(s, { kind: 'availability', data: v(119, 5) })).toBe(s);
    const next = reduceLive(s, { kind: 'availability', data: v(121, 5) });
    expect(next.availability?.available).toBe(5);
    expect(next.lastVersion).toBe(121);
  });

  test('deltas before the first full list are ignored', () => {
    const s = initialLiveState({ spaces: spacesFixture.spaces });
    expect(reduceLive(s, { kind: 'spaces', data: spacesDeltaFixture })).toBe(s);
  });

  test('a full spaces list replaces the map and sets spacesReady', () => {
    const s = ready();
    expect(s.spacesReady).toBe(true);
    expect(s.spaces).toEqual(spacesFixture.spaces);
  });

  test('a delta after full sets E100 to occupied', () => {
    const s = ready();
    expect(stateOf(s.spaces, 'E100')).toBe('free');
    const next = reduceLive(s, { kind: 'spaces', data: spacesDeltaFixture });
    expect(stateOf(next.spaces, 'E100')).toBe('occupied');
    expect(countSpaces(next.spaces ?? []).occupied).toBe(67);
    expect(reduceLive(next, { kind: 'spaces', data: spacesDeltaFixture })).toBe(next);
  });

  test('reduceLive ignores unknown codes in a delta', () => {
    const s = ready();
    expect(reduceLive(s, { kind: 'spaces', data: { full: false, spaces: [{ code: 'E999', state: 'occupied' }] } })).toBe(s);
  });

  test('open resets spacesReady and keeps the last spaces', () => {
    const s = ready();
    const next = reduceLive(s, { kind: 'open' });
    expect(next.spacesReady).toBe(false);
    expect(next.spaces).toBe(s.spaces);
    expect(reduceLive(next, { kind: 'open' })).toBe(next);
  });

  test('heartbeat returns the same state', () => {
    const s = initialLiveState({ availability: v(120) });
    expect(reduceLive(s, { kind: 'heartbeat' })).toBe(s);
  });

  test('snapshot availability passes the version guard; snapshot spaces apply only while not ready', () => {
    const s = initialLiveState({ availability: v(120) });
    expect(s).toMatchObject({ lastVersion: 120, spaces: null, spacesReady: false });
    expect(initialLiveState().lastVersion).toBe(-1);
    expect(reduceLive(s, { kind: 'snapshot', availability: v(119) })).toBe(s);
    const withSpaces = reduceLive(s, { kind: 'snapshot', availability: v(125), spaces: spacesFixture.spaces });
    expect(withSpaces).toMatchObject({ lastVersion: 125, spaces: spacesFixture.spaces, spacesReady: false });
    const r = ready();
    expect(reduceLive(r, { kind: 'snapshot', spaces: [] })).toBe(r);
  });
});

describe('decodeFrame', () => {
  test('decodes the three stream events', () => {
    expect(decodeFrame({ event: 'availability', data: JSON.stringify(v(121)) })).toEqual({ kind: 'availability', data: v(121) });
    expect(decodeFrame({ event: 'spaces', data: JSON.stringify(spacesDeltaFixture) })).toEqual({ kind: 'spaces', data: spacesDeltaFixture });
    expect(decodeFrame({ event: 'heartbeat', data: '{"as_of":"2026-11-15T14:47:15Z"}' })).toEqual({ kind: 'heartbeat' });
  });

  test('decodeFrame drops malformed data', () => {
    expect(decodeFrame({ event: 'availability', data: 'not json' })).toBeNull();
    expect(decodeFrame({ event: 'availability', data: JSON.stringify({ ...v(121), version: '121' }) })).toBeNull();
    expect(decodeFrame({ event: 'foo', data: JSON.stringify(v(121)) })).toBeNull();
    expect(decodeFrame({ event: 'message', data: '{}' })).toBeNull();
  });
});

describe('backoff', () => {
  test('nextDelay bounds', () => {
    expect(nextDelay(0, () => 0)).toBe(500);
    expect(nextDelay(0, () => 1)).toBe(1000);
    expect(nextDelay(5, () => 1)).toBe(30000);
    expect(nextDelay(10, () => 0)).toBe(15000);
    expect(nextDelay(0, () => 0, 10000)).toBe(10000);
  });

  test('parseRetryAfter rejects anything but non-negative integer seconds', () => {
    expect(parseRetryAfter('10')).toBe(10000);
    expect(parseRetryAfter('0')).toBe(0);
    for (const bad of ['soon', '-5', '1.5', 'Wed, 21 Oct 2026 07:28:00 GMT', '', ' ', null]) {
      expect(parseRetryAfter(bad)).toBeNull();
    }
  });
});

describe('figures', () => {
  test('availabilityFigures uses capacity minus effective capacity', () => {
    expect(availabilityFigures(availabilityReducedFixture)).toEqual({
      total: 280,
      available: 211,
      occupied: 66,
      outOfService: 3,
      effectiveCapacity: 277,
    });
  });

  test('countSpaces counts the fixture', () => {
    expect(countSpaces(spacesFixture.spaces)).toEqual({ free: 211, occupied: 66, outOfService: 3 });
  });
});

test('planAnnouncement: silent on mount, status at once, count at most every 30 s', () => {
  const t = 1_000_000;
  const input: AnnounceInput = { available: 211, status: 'open', connection: 'live' };
  let step = planAnnouncement(null, input, t);
  expect(step.say).toBeNull();
  step = planAnnouncement(step.memo, { ...input, status: 'full' }, t + 1_000);
  expect(step.say).toBe('status');
  step = planAnnouncement(step.memo, { ...input, status: 'full', available: 210 }, t + 2_000);
  expect(step.say).toBeNull();
  step = planAnnouncement(step.memo, { ...input, status: 'full', available: 209 }, t + 31_000);
  expect(step.say).toBe('count');
  step = planAnnouncement(step.memo, { ...input, status: 'full', available: 209 }, t + 70_000);
  expect(step.say).toBeNull();
  step = planAnnouncement(step.memo, { ...input, status: 'full', available: 209, connection: 'stale' }, t + 71_000);
  expect(step.say).toBe('connection');
  step = planAnnouncement(step.memo, { ...input, status: 'full', available: 209, connection: 'stale' }, t + 72_000);
  expect(step.say).toBeNull();
});

test('planAnnouncement returns when a held-back count becomes due', () => {
  const t = 1_000_000;
  const input: AnnounceInput = { available: 211, status: 'open', connection: 'live' };
  let step = planAnnouncement(null, input, t);
  expect(step.dueAt).toBeNull();
  // The count changes at t + 2 s and nothing else happens: the caller re-calls at dueAt.
  step = planAnnouncement(step.memo, { ...input, available: 210 }, t + 2_000);
  expect(step).toMatchObject({ say: null, dueAt: t + 30_000 });
  step = planAnnouncement(step.memo, { ...input, available: 210 }, step.dueAt!);
  expect(step).toMatchObject({ say: 'count', dueAt: null });
});

test('a status announcement leaves the count pending (the status copy has no number)', () => {
  const t = 1_000_000;
  const input: AnnounceInput = { available: 211, status: 'open', connection: 'live' };
  let step = planAnnouncement(null, input, t);
  step = planAnnouncement(step.memo, { ...input, status: 'full', available: 0 }, t + 10_000);
  expect(step).toMatchObject({ say: 'status', dueAt: t + 30_000 });
  step = planAnnouncement(step.memo, { ...input, status: 'full', available: 0 }, t + 30_000);
  expect(step.say).toBe('count');
});

test('lotQueryKeys builds the design keys', () => {
  expect(lotQueryKeys.all).toEqual(['lots']);
  expect(lotQueryKeys.lot('L1')).toEqual(['lot', 'L1']);
  expect(lotQueryKeys.availability('L1')).toEqual(['lot', 'L1', 'availability']);
  expect(lotQueryKeys.spaces('L1')).toEqual(['lot', 'L1', 'spaces']);
  expect(lotQueryKeys.tariff('L1')).toEqual(['lot', 'L1', 'tariff']);
});

describe('createConnectionStore', () => {
  test('set merges a patch and notifies subscribers', () => {
    const store = createConnectionStore({ status: 'connecting', lastFrameAt: null, spacesReady: false });
    const listener = vi.fn();
    const unsubscribe = store.subscribe(listener);
    store.set({ status: 'live', lastFrameAt: 5 });
    expect(store.get()).toEqual({ status: 'live', lastFrameAt: 5, spacesReady: false });
    expect(listener).toHaveBeenCalledTimes(1);
    unsubscribe();
    store.set({ status: 'stale' });
    expect(listener).toHaveBeenCalledTimes(1);
  });

  test('an unchanged patch keeps the same snapshot and does not notify', () => {
    const store = createConnectionStore({ status: 'live', lastFrameAt: 5, spacesReady: true });
    const listener = vi.fn();
    store.subscribe(listener);
    const before = store.get();
    store.set({ status: 'live', lastFrameAt: 5 });
    expect(store.get()).toBe(before);
    expect(listener).not.toHaveBeenCalled();
  });
});
