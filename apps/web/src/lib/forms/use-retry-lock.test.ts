import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { useRetryLock } from './use-retry-lock';

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

test('locks, counts down and unlocks', () => {
  const { result } = renderHook(() => useRetryLock());
  expect(result.current).toMatchObject({ locked: false, seconds: 0 });

  act(() => result.current.lockFor(3));
  expect(result.current).toMatchObject({ locked: true, seconds: 3 });

  act(() => vi.advanceTimersByTime(1000));
  expect(result.current.seconds).toBe(2);

  // Each tick is scheduled after the previous render, so advance one second at a time.
  act(() => vi.advanceTimersByTime(1000));
  act(() => vi.advanceTimersByTime(1000));
  expect(result.current).toMatchObject({ locked: false, seconds: 0 });
});

test('a new lock restarts the countdown', () => {
  const { result } = renderHook(() => useRetryLock());
  act(() => result.current.lockFor(5));
  act(() => vi.advanceTimersByTime(2000));

  act(() => result.current.lockFor(10));

  expect(result.current.seconds).toBe(10);
});

test('ignores a non-positive duration', () => {
  const { result } = renderHook(() => useRetryLock());

  act(() => result.current.lockFor(0));

  expect(result.current.locked).toBe(false);
});
