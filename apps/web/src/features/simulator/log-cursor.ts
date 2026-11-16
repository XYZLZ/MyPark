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
  /** Newest first. */
  rows: LogRow[];
};

export const initialLogState: LogState = { cursor: 0, resetPending: false, startedAt: null, rows: [] };

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
 * Folds one events response into the log. After a failed poll the request was `after=0` (M2B-31), and
 * the answer is read two ways (PF2): the same process still holds the rows already shown, so those are
 * dropped and no gap is reported; a restarted process starts over, so a separator is added instead.
 */
export function applyEvents(state: LogState, after: number, events: SimulatorEvent[]): LogState {
  const recovering = state.resetPending && state.cursor > 0;
  if (events.length === 0) {
    if (!recovering) return state.resetPending ? { ...state, resetPending: false } : state;
    return { ...state, cursor: 0, resetPending: false, rows: prepend(state.rows, [{ kind: 'restart', before: state.cursor }]) };
  }

  const sorted = [...events].sort((a, b) => a.seq - b.seq);
  const max = sorted[sorted.length - 1]!.seq;
  const restarted = recovering && max < state.cursor;
  const base = restarted ? 0 : recovering ? state.cursor : after;
  const fresh = recovering && !restarted ? sorted.filter((event) => event.seq > state.cursor) : sorted;
  if (fresh.length === 0) return { ...state, resetPending: false };

  const ascending: LogRow[] = [];
  if (restarted) ascending.push({ kind: 'restart', before: state.cursor });
  const first = fresh[0]!.seq;
  // Nothing was seen before the first load, so nothing was skipped.
  if (first > base + 1 && (base > 0 || restarted)) ascending.push({ kind: 'gap', from: base + 1, to: first - 1 });
  for (const event of fresh) ascending.push({ kind: 'event', event });

  return {
    ...state,
    cursor: restarted ? max : Math.max(state.cursor, max),
    resetPending: false,
    rows: prepend(state.rows, ascending),
  };
}

/** A run separator when `started_at` changes. It never touches the cursor (M2B-31). */
export function noteRun(state: LogState, status: SimulatorStatus): LogState {
  if (!status.started_at || status.started_at === state.startedAt) return state;
  const row: LogRow = { kind: 'run', scenario: status.scenario, startedAt: status.started_at };
  return { ...state, startedAt: status.started_at, rows: prepend(state.rows, [row]) };
}
