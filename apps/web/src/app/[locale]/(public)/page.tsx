import { cookies } from 'next/headers';
import { Suspense } from 'react';
import { PublicHome } from '@/features/home/PublicHome';
import { LotSkeleton } from '@/features/lot/components/LotSkeleton';
import { LotSection } from '@/features/lot/LotSection';
import { LOGOUT_ALL_FAILED_NOTICE } from '@/lib/bff/logout-all';
import { hasSessionHint } from '@/lib/session/hint';

type SearchParams = Promise<Record<string, string | string[] | undefined>>;

// Container: only the presence of a session cookie picks the actions (CC-24).
// It is a hint, not a check: the guards on protected pages verify the session.
export default async function HomePage({ searchParams }: { searchParams: SearchParams }) {
  const [store, { notice }] = await Promise.all([cookies(), searchParams]);

  // Only the one known notice is shown; anything else in the URL is ignored.
  return (
    <PublicHome
      signedIn={hasSessionHint(store)}
      notice={notice === LOGOUT_ALL_FAILED_NOTICE ? 'logout_all_failed' : undefined}
      lot={(actions) => (
        <Suspense
          fallback={
            <>
              <div className="lg:col-start-2 lg:row-start-1">
                <LotSkeleton />
              </div>
              {actions}
            </>
          }
        >
          <LotSection variant="home" actions={actions} />
        </Suspense>
      )}
    />
  );
}
