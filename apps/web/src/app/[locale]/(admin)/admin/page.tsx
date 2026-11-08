import type { Metadata } from 'next';
import { getLocale, getTranslations } from 'next-intl/server';
import { AdminLanding } from '@/features/admin/AdminLanding';
import { RefreshOnMount } from '@/features/session/RefreshOnMount';
import { resolveLocale } from '@/i18n/routing';
import { requireAdmin } from '@/lib/auth/guards';

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('admin');
  return { title: t('title') };
}

export default async function AdminPage() {
  const gate = await requireAdmin(resolveLocale(await getLocale()));
  // A layout that does not re-run on client navigation would leave an empty page: refresh once.
  if (gate.state !== 'ready') return <RefreshOnMount />;

  return <AdminLanding user={gate.user} />;
}
