import { getTranslations } from 'next-intl/server';
import type { ReactNode } from 'react';
import { SiteHeader } from '@/components/SiteHeader';
import { LanguageSwitch } from '@/components/ui/LanguageSwitch';
import { AccountBlocked } from '@/features/account/AccountBlocked';
import { PersistedLanguageSwitch } from '@/features/session/PersistedLanguageSwitch';
import { SessionUnavailable } from '@/features/session/SessionUnavailable';
import { resolveLocale } from '@/i18n/routing';
import { requireAdmin } from '@/lib/auth/guards';

// Admin pages live under (admin)/admin/. There is deliberately no loading.tsx here:
// streaming would start before forbidden() runs and the 403 would become a 200 (CC-7).
export default async function AdminLayout({ children, params }: { children: ReactNode; params: Promise<{ locale: string }> }) {
  const { locale } = await params;
  const [gate, t] = await Promise.all([requireAdmin(resolveLocale(locale)), getTranslations('common')]);

  return (
    <>
      <SiteHeader
        appName={t('appName')}
        languageSwitch={gate.state === 'ready' ? <PersistedLanguageSwitch /> : <LanguageSwitch />}
      />
      <main id="main" className="mx-auto flex w-full max-w-5xl flex-1 flex-col px-4 py-8">
        {gate.state === 'ready' ? children : gate.state === 'suspended' ? <AccountBlocked /> : <SessionUnavailable />}
      </main>
    </>
  );
}
