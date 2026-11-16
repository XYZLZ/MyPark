import { defaultScheduler, notifyManager } from '@tanstack/react-query';

/**
 * TanStack Query batches observer notifications on a `setTimeout(0)`, which fake timers freeze.
 * This runs them synchronously so a cache write shows up inside the same `act` with no timer
 * advanced (the frame-to-screen tests, M2B-42). Production adds only that 0 ms batch, not a
 * deliberate delay. Returns the restore function.
 */
export function notifyQuerySynchronously(): () => void {
  notifyManager.setScheduler((callback) => callback());
  return () => notifyManager.setScheduler(defaultScheduler);
}
