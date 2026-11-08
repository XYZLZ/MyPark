'use client';

import { useEffect, useRef } from 'react';
import { useRouter } from '@/i18n/navigation';

/**
 * What a guarded page renders when its guard is not `ready`. A shared layout does not
 * re-run on client navigation, so after a transient failure, a new legal version or a
 * suspension it would still show an empty <main>. One refresh re-runs the layout, which
 * then renders the matching gate or panel. It fires once per mount (and once under
 * StrictMode's double effect), so a state that stays non-ready cannot loop.
 */
export function RefreshOnMount() {
  const router = useRouter();
  const done = useRef(false);

  useEffect(() => {
    if (done.current) return;
    done.current = true;
    router.refresh();
  }, [router]);

  return null;
}
