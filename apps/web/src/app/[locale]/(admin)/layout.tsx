import type { ReactNode } from 'react';
import { AppShell } from '@/components/shell/AppShell';
import { LanguageSwitch } from '@/components/ui/LanguageSwitch';
import { AccountBlocked } from '@/features/account/AccountBlocked';
import { LogoutButtons } from '@/features/profile/LogoutButtons';
import { PersistedLanguageSwitch } from '@/features/session/PersistedLanguageSwitch';
import { SessionUnavailable } from '@/features/session/SessionUnavailable';
import { resolveLocale } from '@/i18n/routing';
import { requireAdmin } from '@/lib/auth/guards';

// Admin pages live under (admin)/admin/. There is deliberately no loading.tsx here:
// streaming would start before forbidden() runs and the 403 would become a 200 (CC-7).
export default async function AdminLayout({ children, params }: { children: ReactNode; params: Promise<{ locale: string }> }) {
  const { locale } = await params;
  const gate = await requireAdmin(resolveLocale(locale));
  const ready = gate.state === 'ready';

  return (
    <AppShell
      variant={ready ? 'admin' : 'guest'}
      greetingName={ready ? gate.user.first_name : undefined}
      languageSwitch={ready ? <PersistedLanguageSwitch appearance="on-light" /> : <LanguageSwitch appearance="on-light" />}
      sidebarFooter={ready ? <LogoutButtons scope="this" appearance="sidebar" /> : null}
    >
      <div className="mx-auto flex w-full max-w-5xl flex-1 flex-col px-4 py-8">
        {ready ? children : gate.state === 'suspended' ? <AccountBlocked /> : <SessionUnavailable />}
      </div>
    </AppShell>
  );
}
