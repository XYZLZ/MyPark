import { useTranslations } from 'next-intl';
import type { ReactNode } from 'react';
import { Sidebar } from './Sidebar';
import { TopBar } from './TopBar';
import type { ShellVariant } from './nav-items';

export type { ShellVariant } from './nav-items';

type AppShellProps = {
  variant: ShellVariant;
  greetingName?: string;
  languageSwitch: ReactNode;
  sidebarFooter: ReactNode;
  children: ReactNode;
};

/** Skip link, navy sidebar, top bar, then `main`. Server component: only `Sidebar` is a client part. */
export function AppShell({ variant, greetingName, languageSwitch, sidebarFooter, children }: AppShellProps) {
  const t = useTranslations('common');

  return (
    <>
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-50 focus:rounded-full focus:bg-surface focus:px-4 focus:py-2 focus:font-semibold focus:text-brand-navy focus:shadow-[0_0_0_3px_var(--color-brand-navy)] focus:outline-none"
      >
        {t('skipToContent')}
      </a>
      <div className="flex flex-1 flex-col lg:grid lg:grid-cols-[14.25rem_minmax(0,1fr)]">
        <Sidebar variant={variant} footer={sidebarFooter} />
        <div className="flex min-w-0 flex-1 flex-col">
          <TopBar greetingName={greetingName} languageSwitch={languageSwitch} />
          <main id="main" className="flex flex-1 flex-col">
            {children}
          </main>
        </div>
      </div>
    </>
  );
}
