import type { SimulatorEvent, SimulatorStatus } from '@mypark/contracts';

/** Rows kept in memory, newest first (M2B-30). */
export const MAX_LOG_ROWS = 500;

export type LogRow =
  | { kind: 'event'; event: SimulatorEvent }
  | { kind: 'gap'; from: number; to: number }
  | { kind: 'run'; scenario: string; startedAt: string }
  /** The simulator process restarted (its `seq` started over), so rows below belong to the old process (PF2). */
  | { kind: 'restart'; before: number };

export type LogState = {
  /** The highest `seq` seen. It survives `started_at` changes: `seq` is per simulator process (M2B-31). */
  cursor: number;
  /** A poll failed, so the next request asks from 0 in case the simulator restarted. */
  resetPending: boolean;
  startedAt: string | null;
  /** Every event ever added, so the log can say how many are new even when the cap drops rows. */
  received: number;
  /** An events response has been applied, even an empty one: the log has had its first load. */
  loaded: boolean;
  /** Newest first. */
  rows: LogRow[];
};

export const initialLogState: LogState = { cursor: 0, resetPending: false, startedAt: null, received: 0, loaded: false, rows: [] };

/** The `after` value for the next events request. */
export function nextAfter(state: LogState): number {
  return state.resetPending ? 0 : state.cursor;
}

export function markPollFailed(state: LogState): LogState {
  return state.resetPending ? state : { ...state, resetPending: true };
}

/** Newest first, capped. `ascending` is oldest to newest. */
function prepend(rows: LogRow[], ascending: LogRow[]): LogRow[] {
  return [...ascending.reverse(), ...rows].slice(0, MAX_LOG_ROWS);
}

/**
 * Folds one events response into the log. `after` is the cursor the request was sent with.
 * - Events at or below the cursor are always dropped: overlapping polls can return the same page twice.
 * - Only an answer to `after=0` that follows a failed poll can reveal a restart (PF2): the same process
 *   still holds the rows already shown (nothing new is added, no gap), a restarted one starts over, so a
 *   separator is added. A normal answer to a non-zero `after` never means a restart and keeps the reset pending.
 */
export function applyEvents(state: LogState, after: number, events: SimulatorEvent[]): LogState {
  const next = foldEvents(state, after, events);
  return next.loaded ? next : { ...next, loaded: true };
}

function foldEvents(state: LogState, after: number, events: SimulatorEvent[]): LogState {
  const resetAnswer = after === 0;
  const recovering = resetAnswer && state.resetPending && state.cursor > 0;
  const resetPending = resetAnswer ? false : state.resetPending;
  if (events.length === 0) {
    if (recovering) {
      return { ...state, cursor: 0, resetPending, rows: prepend(state.rows, [{ kind: 'restart', before: state.cursor }]) };
    }
    return resetPending === state.resetPending ? state : { ...state, resetPending };
  }

  const sorted = [...events].sort((a, b) => a.seq - b.seq);
  const max = sorted[sorted.length - 1]!.seq;
  const restarted = recovering && max < state.cursor;
  const fresh = restarted ? sorted : sorted.filter((event) => event.seq > state.cursor);
  if (fresh.length === 0) return resetPending === state.resetPending ? state : { ...state, resetPending };

  const base = restarted ? 0 : state.cursor;
  const ascending: LogRow[] = [];
  if (restarted) ascending.push({ kind: 'restart', before: state.cursor });
  const first = fresh[0]!.seq;
  // Nothing was seen before the first load, so nothing was skipped.
  if (first > base + 1 && (base > 0 || restarted)) ascending.push({ kind: 'gap', from: base + 1, to: first - 1 });
  for (const event of fresh) ascending.push({ kind: 'event', event });

  return {
    ...state,
    cursor: restarted ? max : Math.max(state.cursor, max),
    resetPending,
    received: state.received + fresh.length,
    rows: prepend(state.rows, ascending),
  };
}

/** A run separator when `started_at` changes. It never touches the cursor (M2B-31). */
export function noteRun(state: LogState, status: SimulatorStatus): LogState {
  if (!status.started_at || status.started_at === state.startedAt) return state;
  // The first one seen is not a change: a reload into a running scenario is not a new run.
  if (state.startedAt === null) return { ...state, startedAt: status.started_at };
  const row: LogRow = { kind: 'run', scenario: status.scenario, startedAt: status.started_at };
  return { ...state, startedAt: status.started_at, rows: prepend(state.rows, [row]) };
}
