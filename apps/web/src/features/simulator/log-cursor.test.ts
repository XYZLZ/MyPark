import type { SimulatorEvent } from '@mypark/contracts';
import { simulatorStatusRunningFixture } from '@mypark/contracts/fixtures';
import { describe, expect, test } from 'vitest';
import { applyEvents, initialLogState, markPollFailed, MAX_LOG_ROWS, nextAfter, noteRun, type LogRow, type LogState } from './log-cursor';

const ev = (seq: number): SimulatorEvent => ({ seq, at: '2026-11-15T14:46:10Z', device: 'dispenser-1', type: 'barrier.passed', result: 'ack' });
const range = (from: number, to: number) => Array.from({ length: to - from + 1 }, (_, i) => ev(from + i));
const seqs = (state: LogState) => state.rows.map((row) => (row.kind === 'event' ? row.event.seq : row.kind));
const withCursor = (cursor: number): LogState => applyEvents(initialLogState, 0, [ev(cursor)]);

describe('log cursor', () => {
  test('the cursor is kept across started_at changes', () => {
    const state = noteRun(withCursor(1846), { ...simulatorStatusRunningFixture, started_at: '2026-11-15T15:00:00Z' });
    expect(state.cursor).toBe(1846);
    expect(nextAfter(state)).toBe(1846);
    expect(state.rows[0]).toEqual({ kind: 'run', scenario: 'weekday', startedAt: '2026-11-15T15:00:00Z' });
  });

  test('the same started_at adds no second run row', () => {
    const status = { ...simulatorStatusRunningFixture, started_at: '2026-11-15T15:00:00Z' };
    const once = noteRun(initialLogState, status);
    expect(noteRun(once, status)).toBe(once);
  });

  test('a failed poll then a success restarts from 0', () => {
    const failed = markPollFailed(withCursor(5));
    expect(nextAfter(failed)).toBe(0);
    const fresh = markPollFailed(initialLogState);
    const next = applyEvents(fresh, 0, [ev(1), ev(2)]);
    expect(next.cursor).toBe(2);
    expect(next.resetPending).toBe(false);
  });

  test('after a failed poll, events already shown are dropped and no gap row appears (same process)', () => {
    const shown = applyEvents(initialLogState, 0, range(1, 10));
    const again = applyEvents(markPollFailed(shown), 0, range(1, 12));
    expect(seqs(again)).toEqual([12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1]);
    expect(again.cursor).toBe(12);
    expect(again.resetPending).toBe(false);
  });

  test('after a failed poll with nothing new, the rows are unchanged', () => {
    const shown = applyEvents(initialLogState, 0, range(1, 10));
    const again = applyEvents(markPollFailed(shown), 0, range(1, 10));
    expect(again.rows).toBe(shown.rows);
    expect(again.resetPending).toBe(false);
  });

  test('a restarted simulator adds a separator row and starts over', () => {
    const shown = applyEvents(initialLogState, 0, range(1, 10));
    const restarted = applyEvents(markPollFailed(shown), 0, range(1, 3));
    expect(seqs(restarted).slice(0, 4)).toEqual([3, 2, 1, 'restart']);
    expect(restarted.cursor).toBe(3);
  });

  test('an empty answer after a failed poll means the simulator restarted', () => {
    const restarted = applyEvents(markPollFailed(withCursor(40)), 0, []);
    expect(restarted.cursor).toBe(0);
    expect(restarted.rows[0]?.kind).toBe('restart');
    expect(restarted.resetPending).toBe(false);
  });

  test('a gap row when the first seq skips', () => {
    const state = applyEvents(withCursor(1796), 1796, [ev(1841), ev(1842)]);
    expect(state.rows.find((row): row is Extract<LogRow, { kind: 'gap' }> => row.kind === 'gap')).toEqual({ kind: 'gap', from: 1797, to: 1840 });
    expect(seqs(state).slice(0, 4)).toEqual([1842, 1841, 'gap', 1796]);
  });

  test('the first load shows no gap row', () => {
    expect(seqs(applyEvents(initialLogState, 0, range(1341, 1345)))).not.toContain('gap');
  });

  test('newest first and capped at 500 rows', () => {
    const first = applyEvents(initialLogState, 0, range(1, 400));
    const state = applyEvents(first, 400, range(401, 700));
    expect(state.rows).toHaveLength(MAX_LOG_ROWS);
    expect(seqs(state)[0]).toBe(700);
    expect(seqs(state)[MAX_LOG_ROWS - 1]).toBe(201);
    expect(state.cursor).toBe(700);
  });
});
