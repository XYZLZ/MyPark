import type { Metadata } from 'next';
import { getLocale, getTranslations } from 'next-intl/server';
import { ProfileForm } from '@/features/profile/ProfileForm';
import { resolveLocale } from '@/i18n/routing';
import { requireDriver } from '@/lib/auth/guards';

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('profile');
  return { title: t('title') };
}

export default async function ProfilePage() {
  const gate = await requireDriver(resolveLocale(await getLocale()));
  // The layout shows the gate, the blocked panel or the retry panel instead.
  if (gate.state !== 'ready') return null;

  return <ProfileForm user={gate.user} />;
}
