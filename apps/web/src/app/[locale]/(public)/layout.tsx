import { getTranslations } from 'next-intl/server';
import type { ReactNode } from 'react';
import { SiteHeader } from '@/components/SiteHeader';
import { LanguageSwitch } from '@/components/ui/LanguageSwitch';

// Public pages: home, login, register, verify, reset. The auth panel is
// full-bleed (mockup), so `main` carries no width constraint.
export default async function PublicLayout({ children }: { children: ReactNode }) {
  const t = await getTranslations('common');

  return (
    <>
      <SiteHeader appName={t('appName')} languageSwitch={<LanguageSwitch />} />
      <main id="main" className="flex flex-1 flex-col">
        {children}
      </main>
    </>
  );
}
