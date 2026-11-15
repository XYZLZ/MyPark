import type { SseFrame } from '../sse/parser';
import { availabilitySchema, heartbeatSchema, type Availability } from './availability';
import type { LotStatus } from './lot';
import { spacesEventSchema, type SpaceState, type SpacesEvent } from './spaces';

/** Shared live-stream rules for web and mobile (design §5.4–§5.5, M2B-10). Pure: time and randomness are injected. */

export const FRESH_WINDOW_MS = 45_000;
export const BACKOFF_BASE_MS = 1_000;
export const BACKOFF_MAX_MS = 30_000;
export const HTTP_FAILURE_FLOOR_MS = 10_000;
export const SNAPSHOT_MIN_INTERVAL_MS = 10_000;
export const IDLE_CLOSE_MS = 2_000;
export const ANNOUNCE_COUNT_INTERVAL_MS = 30_000;

export type ConnectionStatus = 'connecting' | 'live' | 'stale' | 'unavailable';

export type LiveState = {
  availability: Availability | null;
  spaces: SpaceState[] | null;
  spacesReady: boolean;
  lastVersion: number;
};

export type LiveFrame =
  | { kind: 'open' }
  /** The connection dropped or closed: the map stops being authoritative, so REST spaces may replace it. */
  | { kind: 'lost' }
  | { kind: 'availability'; data: Availability }
  | { kind: 'spaces'; data: SpacesEvent }
  | { kind: 'heartbeat' }
  | { kind: 'snapshot'; availability?: Availability; spaces?: SpaceState[] };

export function initialLiveState(snapshot?: { availability?: Availability; spaces?: SpaceState[] }): LiveState {
  return {
    availability: snapshot?.availability ?? null,
    spaces: snapshot?.spaces ?? null,
    spacesReady: false,
    lastVersion: snapshot?.availability?.version ?? -1,
  };
}

function applyAvailability(state: LiveState, data: Availability): LiveState {
  if (data.version <= state.lastVersion) return state;
  return { ...state, availability: data, lastVersion: data.version };
}

function applyDelta(state: LiveState, delta: SpaceState[]): LiveState {
  if (!state.spacesReady || state.spaces === null) return state;
  const updates = new Map(delta.map((s) => [s.code, s.state]));
  let changed = false;
  const spaces = state.spaces.map((space) => {
    const next = updates.get(space.code);
    if (next === undefined || next === space.state) return space;
    changed = true;
    return { code: space.code, state: next };
  });
  return changed ? { ...state, spaces } : state;
}

/** Applies one frame (design §5.5). Returns the same reference when nothing changed. */
export function reduceLive(state: LiveState, frame: LiveFrame): LiveState {
  switch (frame.kind) {
    case 'open':
    case 'lost':
      return state.spacesReady ? { ...state, spacesReady: false } : state;
    case 'availability':
      return applyAvailability(state, frame.data);
    case 'spaces':
      return frame.data.full
        ? { ...state, spaces: frame.data.spaces, spacesReady: true }
        : applyDelta(state, frame.data.spaces);
    case 'heartbeat':
      return state;
    case 'snapshot': {
      const next = frame.availability ? applyAvailability(state, frame.availability) : state;
      if (!frame.spaces || next.spacesReady) return next;
      return { ...next, spaces: frame.spaces };
    }
  }
}

function parseJson(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return undefined;
  }
}

/** Validates an SSE frame. Bad JSON, a failed schema or an unknown event gives `null`; callers still count it as fresh. */
export function decodeFrame(frame: SseFrame): LiveFrame | null {
  const json = parseJson(frame.data);
  switch (frame.event) {
    case 'availability': {
      const parsed = availabilitySchema.safeParse(json);
      return parsed.success ? { kind: 'availability', data: parsed.data } : null;
    }
    case 'spaces': {
      const parsed = spacesEventSchema.safeParse(json);
      return parsed.success ? { kind: 'spaces', data: parsed.data } : null;
    }
    case 'heartbeat':
      return heartbeatSchema.safeParse(json).success ? { kind: 'heartbeat' } : null;
    default:
      return null;
  }
}

/** `max(floorMs, min(30 000, 1 000 × 2^attempt) × (0.5 + 0.5 × random()))` (design §5.4). */
export function nextDelay(attempt: number, random: () => number, floorMs = 0): number {
  const ceiling = Math.min(BACKOFF_MAX_MS, BACKOFF_BASE_MS * 2 ** attempt);
  return Math.max(floorMs, ceiling * (0.5 + 0.5 * random()));
}

/** Milliseconds for a `Retry-After` of non-negative integer seconds; `null` for anything else, HTTP dates included. */
export function parseRetryAfter(header: string | null): number | null {
  if (header === null || !/^\d+$/.test(header.trim())) return null;
  return Number(header.trim()) * 1000;
}

export type AvailabilityFigures = {
  total: number;
  available: number;
  occupied: number;
  outOfService: number;
  effectiveCapacity: number;
};

/** Figures from the gate counter only; the map never feeds them (M2B-15). */
export function availabilityFigures(a: Availability): AvailabilityFigures {
  return {
    total: a.capacity,
    available: a.available,
    occupied: a.occupied,
    outOfService: a.capacity - a.effective_capacity,
    effectiveCapacity: a.effective_capacity,
  };
}

export function countSpaces(spaces: SpaceState[]): { free: number; occupied: number; outOfService: number } {
  const counts = { free: 0, occupied: 0, outOfService: 0 };
  for (const { state } of spaces) {
    if (state === 'free') counts.free++;
    else if (state === 'occupied') counts.occupied++;
    else counts.outOfService++;
  }
  return counts;
}

export type AnnounceInput = { available: number; status: LotStatus; connection: ConnectionStatus };

declare const announceMemoBrand: unique symbol;
type AnnounceMemoFields = { status: LotStatus; connection: ConnectionStatus; count: number; countAt: number };
/** Opaque memo of what was last announced. */
export type AnnounceMemo = AnnounceMemoFields & { readonly [announceMemoBrand]: true };

const memo = (fields: AnnounceMemoFields) => fields as AnnounceMemo;

export type AnnouncementStep = {
  say: 'status' | 'connection' | 'count' | null;
  memo: AnnounceMemo;
  /** When a count change is still pending, the time it becomes due; otherwise `null`. */
  dueAt: number | null;
};

/**
 * One polite announcer (M2B-22): silent on mount, a status or connection change at once,
 * a count change at most every 30 s and only when it differs from the last announced count.
 * When several things change, one is said and the rest stay pending in the memo.
 *
 * A held-back count is only said on a later call, so callers re-call with the same input at
 * `dueAt` (a timer) even when nothing else changes. The status sentences (`announce.open`,
 * `full`, `closed`) carry no number, so a status announcement leaves `memo.count` alone and
 * a count that changed with it is still announced when due.
 */
export function planAnnouncement(prev: AnnounceMemo | null, input: AnnounceInput, now: number): AnnouncementStep {
  const step = (say: AnnouncementStep['say'], next: AnnounceMemo): AnnouncementStep => ({
    say,
    memo: next,
    dueAt: input.available === next.count ? null : next.countAt + ANNOUNCE_COUNT_INTERVAL_MS,
  });
  if (prev === null) {
    return step(null, memo({ status: input.status, connection: input.connection, count: input.available, countAt: now }));
  }
  if (input.status !== prev.status) return step('status', memo({ ...prev, status: input.status }));
  if (input.connection !== prev.connection) return step('connection', memo({ ...prev, connection: input.connection }));
  if (input.available !== prev.count && now - prev.countAt >= ANNOUNCE_COUNT_INTERVAL_MS) {
    return step('count', memo({ ...prev, count: input.available, countAt: now }));
  }
  return step(null, prev);
}

/** TanStack Query keys (M2B-19). */
export const lotQueryKeys = {
  all: ['lots'] as const,
  lot: (id: string) => ['lot', id] as const,
  availability: (id: string) => ['lot', id, 'availability'] as const,
  spaces: (id: string) => ['lot', id, 'spaces'] as const,
  tariff: (id: string) => ['lot', id, 'tariff'] as const,
};

/** Per-lot connection state, shared by the web and mobile managers (PF16). */
export type Connection = { status: ConnectionStatus; lastFrameAt: number | null; spacesReady: boolean };

export type ConnectionStore = {
  get(): Connection;
  set(patch: Partial<Connection>): void;
  subscribe(listener: () => void): () => void;
};

/** A tiny external store for `useSyncExternalStore`: `get()` keeps its reference until a field changes. */
export function createConnectionStore(initial: Connection): ConnectionStore {
  let current = initial;
  const listeners = new Set<() => void>();
  return {
    get: () => current,
    set(patch) {
      const keys = Object.keys(patch) as Array<keyof Connection>;
      if (keys.every((key) => patch[key] === current[key])) return;
      current = { ...current, ...patch };
      for (const listener of [...listeners]) listener();
    },
    subscribe(listener) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
  };
}
