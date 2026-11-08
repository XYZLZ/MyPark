import type { Metadata } from 'next';
import { getLocale, getTranslations } from 'next-intl/server';
import { ProfileForm } from '@/features/profile/ProfileForm';
import { RefreshOnMount } from '@/features/session/RefreshOnMount';
import { resolveLocale } from '@/i18n/routing';
import { requireDriver } from '@/lib/auth/guards';

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('profile');
  return { title: t('title') };
}

export default async function ProfilePage() {
  const gate = await requireDriver(resolveLocale(await getLocale()));
  // Re-runs the layout, which shows the gate, the blocked panel or the retry panel instead.
  if (gate.state !== 'ready') return <RefreshOnMount />;

  return <ProfileForm user={gate.user} />;
}
