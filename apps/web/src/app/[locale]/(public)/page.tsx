import { cookies } from 'next/headers';
import { PublicHome } from '@/features/home/PublicHome';
import { LOGOUT_ALL_FAILED_NOTICE } from '@/lib/bff/logout-all';

type SearchParams = Promise<Record<string, string | string[] | undefined>>;

// Container: only the presence of a session cookie picks the actions (CC-24).
// It is a hint, not a check: the guards on protected pages verify the session.
export default async function HomePage({ searchParams }: { searchParams: SearchParams }) {
  const [store, { notice }] = await Promise.all([cookies(), searchParams]);
  const signedIn = store.has('mp_access') || store.has('mp_refresh');

  // Only the one known notice is shown; anything else in the URL is ignored.
  return <PublicHome signedIn={signedIn} notice={notice === LOGOUT_ALL_FAILED_NOTICE ? 'logout_all_failed' : undefined} />;
}
