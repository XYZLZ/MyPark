import { useCallback, useEffect, useState } from 'react';

/** Locks a form for a number of seconds (a 429 wait) and counts down to unlock. */
export function useRetryLock(): { locked: boolean; seconds: number; lockFor(seconds: number): void } {
  const [seconds, setSeconds] = useState(0);

  useEffect(() => {
    if (seconds <= 0) return;
    const timer = setTimeout(() => setSeconds((current) => current - 1), 1000);
    return () => clearTimeout(timer);
  }, [seconds]);

  const lockFor = useCallback((wait: number) => setSeconds(Number.isFinite(wait) ? Math.max(0, Math.ceil(wait)) : 0), []);

  return { locked: seconds > 0, seconds, lockFor };
}
