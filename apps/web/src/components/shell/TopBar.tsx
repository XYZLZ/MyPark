import { useTranslations } from 'next-intl';
import type { ReactNode } from 'react';

/** Banner landmark: the lot's name and place, the language switch and, when signed in, the greeting. */
export function TopBar({ greetingName, languageSwitch }: { greetingName?: string; languageSwitch: ReactNode }) {
  const t = useTranslations('common');

  return (
    <header className="bg-surface">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-4 lg:px-7">
        <p className="text-xl font-extrabold text-brand-navy">{t('location')}</p>
        <div className="ml-auto flex flex-wrap items-center gap-2.5 text-brand-navy">
          {languageSwitch}
          {greetingName ? <p className="font-semibold text-ink">{t('greeting', { name: greetingName })}</p> : null}
        </div>
      </div>
      {/* Decorative: brand-orange never carries text or state. */}
      <div aria-hidden="true" className="h-[3px] bg-brand-orange" />
    </header>
  );
}
