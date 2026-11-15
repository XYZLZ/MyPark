import { cookies } from 'next/headers';
import { getTranslations } from 'next-intl/server';
import type { ReactNode } from 'react';
import { AppShell } from '@/components/shell/AppShell';
import { SIDEBAR_ITEM_CLASS } from '@/components/shell/sidebar-item';
import { LanguageSwitch } from '@/components/ui/LanguageSwitch';
import { LogoutButtons } from '@/features/profile/LogoutButtons';
import { Link } from '@/i18n/navigation';

// Public pages: home, login, register, verify, reset. The auth panel is full-bleed (mockup), so
// `main` carries no width constraint. Only the presence of a session cookie picks the sidebar
// (CC-24): a hint, not a check. The guards on protected pages verify the session.
export default async function PublicLayout({ children }: { children: ReactNode }) {
  const [store, t] = await Promise.all([cookies(), getTranslations('home')]);
  const signedIn = store.has('mp_access') || store.has('mp_refresh');

  return (
    <AppShell
      variant={signedIn ? 'driver' : 'guest'}
      languageSwitch={<LanguageSwitch appearance="on-light" />}
      sidebarFooter={
        signedIn ? (
          <LogoutButtons scope="this" appearance="sidebar" />
        ) : (
          <>
            <Link href="/login" className={SIDEBAR_ITEM_CLASS}>
              {t('logIn')}
            </Link>
            <Link href="/register" className={SIDEBAR_ITEM_CLASS}>
              {t('signUp')}
            </Link>
          </>
        )
      }
    >
      {children}
    </AppShell>
  );
}
