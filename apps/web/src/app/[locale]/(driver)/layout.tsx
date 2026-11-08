import { getTranslations } from 'next-intl/server';
import type { ReactNode } from 'react';
import { SiteHeader } from '@/components/SiteHeader';
import { LanguageSwitch } from '@/components/ui/LanguageSwitch';
import { AccountBlocked } from '@/features/account/AccountBlocked';
import { LegalGate } from '@/features/legal/LegalGate';
import { DriverNav } from '@/features/session/DriverNav';
import { PersistedLanguageSwitch } from '@/features/session/PersistedLanguageSwitch';
import { SessionUnavailable } from '@/features/session/SessionUnavailable';
import { resolveLocale } from '@/i18n/routing';
import { requireDriver } from '@/lib/auth/guards';

// Driver pages. Layouts do not re-run on client navigation, so every page calls the
// guard too (spec 5.6); this one decides what the whole area shows. Anything but
// `ready` returns before `children`, so a gated or blocked driver sees no app content.
export default async function DriverLayout({ children, params }: { children: ReactNode; params: Promise<{ locale: string }> }) {
  const { locale } = await params;
  const [gate, t] = await Promise.all([requireDriver(resolveLocale(locale)), getTranslations('common')]);

  // PATCH /v1/me is not legal-exempt, so behind the gate the language switch stays local.
  const switcher = gate.state === 'ready' ? <PersistedLanguageSwitch /> : <LanguageSwitch />;

  let body: ReactNode;
  switch (gate.state) {
    case 'ready':
      body = <main id="main" className="mx-auto w-full max-w-5xl flex-1 px-4 py-8">{children}</main>;
      break;
    case 'legal':
      body = (
        <main id="main" className="flex flex-1 flex-col">
          <LegalGate legal={gate.legal} user={gate.user} />
        </main>
      );
      break;
    case 'suspended':
      body = (
        <main id="main" className="flex flex-1 flex-col">
          <AccountBlocked />
        </main>
      );
      break;
    case 'unavailable':
      body = (
        <main id="main" className="flex flex-1 flex-col">
          <SessionUnavailable />
        </main>
      );
      break;
  }

  return (
    <>
      <SiteHeader appName={t('appName')} nav={gate.state === 'ready' ? <DriverNav /> : undefined} languageSwitch={switcher} />
      {body}
    </>
  );
}
