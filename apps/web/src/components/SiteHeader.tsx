import { useTranslations } from 'next-intl';
import type { ReactNode } from 'react';
import { ParkingSign } from '@/components/ui/ParkingSign';
import { Link } from '@/i18n/navigation';

type SiteHeaderProps = {
  appName: string;
  nav?: ReactNode;
  languageSwitch: ReactNode;
};

/** Presentational: skip link, then the navy bar with the mark, wordmark, nav and language switch. */
export function SiteHeader({ appName, nav, languageSwitch }: SiteHeaderProps) {
  const t = useTranslations('common');

  return (
    <>
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-50 focus:rounded-full focus:bg-surface focus:px-4 focus:py-2 focus:font-semibold focus:text-brand-primary focus:shadow-[0_0_0_3px_var(--color-brand-primary)] focus:outline-none"
      >
        {t('skipToContent')}
      </a>
      <header className="bg-brand-primary text-brand-primary-contrast">
        <div className="mx-auto flex max-w-5xl flex-wrap items-center gap-x-3 gap-y-2 px-4 py-3">
          <Link
            href="/"
            className="flex items-center gap-2.5 rounded-full font-semibold no-underline outline-none focus-visible:shadow-[0_0_0_3px_var(--color-surface)]"
          >
            <ParkingSign size="sm" />
            <span>{appName}</span>
          </Link>
          <div className="ml-auto flex items-center gap-2">
            {nav ? <nav className="flex items-center gap-1 text-sm">{nav}</nav> : null}
            {languageSwitch}
          </div>
        </div>
      </header>
    </>
  );
}
