import { getTranslations } from 'next-intl/server';
import type { ReactNode } from 'react';
import { SiteHeader } from '@/components/SiteHeader';
import { LanguageSwitch } from '@/components/ui/LanguageSwitch';

// Driver pages (dashboard, reserve, reservations, vehicles, payments, profile).
// The session requirement is added in M1.
export default async function DriverLayout({ children }: { children: ReactNode }) {
  const t = await getTranslations('common');

  return (
    <>
      <SiteHeader appName={t('appName')} languageSwitch={<LanguageSwitch />} />
      <main id="main" className="mx-auto w-full max-w-5xl flex-1 px-4 py-8">
        {children}
      </main>
    </>
  );
}
