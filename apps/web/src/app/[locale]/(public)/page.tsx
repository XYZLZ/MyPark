import { cookies } from 'next/headers';
import { PublicHome } from '@/features/home/PublicHome';

// Container: only the presence of a session cookie picks the actions (CC-24).
// It is a hint, not a check: the guards on protected pages verify the session.
export default async function HomePage() {
  const store = await cookies();
  const signedIn = store.has('mp_access') || store.has('mp_refresh');

  return <PublicHome signedIn={signedIn} />;
}
