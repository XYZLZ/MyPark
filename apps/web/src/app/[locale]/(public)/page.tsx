import { cookies } from 'next/headers';
import { setRequestLocale } from 'next-intl/server';
import { PublicHome } from '@/features/home/PublicHome';

type HomePageProps = { params: Promise<{ locale: string }> };

// Container: only the presence of a session cookie picks the actions (CC-24).
// It is a hint, not a check: the guards on protected pages verify the session.
export default async function HomePage({ params }: HomePageProps) {
  const { locale } = await params;
  setRequestLocale(locale);

  const store = await cookies();
  const signedIn = store.has('mp_access') || store.has('mp_refresh');

  return <PublicHome signedIn={signedIn} />;
}
