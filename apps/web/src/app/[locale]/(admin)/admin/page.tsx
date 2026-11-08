import type { Metadata } from 'next';
import { getLocale, getTranslations } from 'next-intl/server';
import { AdminLanding } from '@/features/admin/AdminLanding';
import { resolveLocale } from '@/i18n/routing';
import { requireAdmin } from '@/lib/auth/guards';

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('admin');
  return { title: t('title') };
}

export default async function AdminPage() {
  const gate = await requireAdmin(resolveLocale(await getLocale()));
  if (gate.state !== 'ready') return null;

  return <AdminLanding user={gate.user} />;
}
