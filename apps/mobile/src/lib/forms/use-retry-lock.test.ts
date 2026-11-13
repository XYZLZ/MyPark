import { act, renderHook } from '@testing-library/react-native';

import { useRetryLock } from './use-retry-lock';

beforeEach(() => jest.useFakeTimers());
afterEach(() => jest.useRealTimers());

test('locks for the wait, counts down and unlocks', async () => {
  const { result } = await renderHook(() => useRetryLock());
  expect(result.current.locked).toBe(false);

  await act(async () => result.current.lockFor(2));
  expect(result.current).toMatchObject({ locked: true, seconds: 2 });

  await act(async () => jest.advanceTimersByTime(1000));
  expect(result.current.seconds).toBe(1);
  await act(async () => jest.advanceTimersByTime(1000));
  expect(result.current).toMatchObject({ locked: false, seconds: 0 });
});

test('a wait that is not a number never locks', async () => {
  const { result } = await renderHook(() => useRetryLock());
  await act(async () => result.current.lockFor(Number.NaN));
  expect(result.current.locked).toBe(false);
});
