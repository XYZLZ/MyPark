import type { ReactNode } from 'react';
import { AppShell } from '@/components/shell/AppShell';
import { LanguageSwitch } from '@/components/ui/LanguageSwitch';
import { AccountBlocked } from '@/features/account/AccountBlocked';
import { LegalGate } from '@/features/legal/LegalGate';
import { LogoutButtons } from '@/features/profile/LogoutButtons';
import { PersistedLanguageSwitch } from '@/features/session/PersistedLanguageSwitch';
import { SessionUnavailable } from '@/features/session/SessionUnavailable';
import { resolveLocale } from '@/i18n/routing';
import { requireDriver } from '@/lib/auth/guards';

// Driver pages. Layouts do not re-run on client navigation, so every page calls the
// guard too (spec 5.6); this one decides what the whole area shows. Anything but
// `ready` returns before `children`, so a gated or blocked driver sees no app content.
export default async function DriverLayout({ children, params }: { children: ReactNode; params: Promise<{ locale: string }> }) {
  const { locale } = await params;
  const gate = await requireDriver(resolveLocale(locale));

  // PATCH /v1/me is not legal-exempt, so behind the gate the language switch stays local.
  const switcher =
    gate.state === 'ready' ? <PersistedLanguageSwitch appearance="on-light" /> : <LanguageSwitch appearance="on-light" />;

  let body: ReactNode;
  switch (gate.state) {
    case 'ready':
      body = <div className="mx-auto w-full max-w-5xl flex-1 px-4 py-8">{children}</div>;
      break;
    case 'legal':
      body = <LegalGate legal={gate.legal} user={gate.user} />;
      break;
    case 'suspended':
      body = <AccountBlocked />;
      break;
    case 'unavailable':
      body = <SessionUnavailable />;
      break;
  }

  // A gated or blocked user sees the guest items only; the panels carry their own log out.
  return (
    <AppShell
      variant={gate.state === 'ready' ? 'driver' : 'guest'}
      greetingName={gate.state === 'ready' ? gate.user.first_name : undefined}
      languageSwitch={switcher}
      sidebarFooter={gate.state === 'ready' ? <LogoutButtons scope="this" appearance="sidebar" /> : null}
    >
      {body}
    </AppShell>
  );
}
