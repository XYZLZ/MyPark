'use client';

import { useTranslations } from 'next-intl';
import { useId, useRef, useState, type ReactNode } from 'react';
import { ParkingSign } from '@/components/ui/ParkingSign';
import { Link, usePathname } from '@/i18n/navigation';
import { NavIcon } from './NavIcon';
import { currentHref, navItems, type ShellVariant } from './nav-items';
import { SIDEBAR_ITEM_CLASS, SIDEBAR_ITEM_CURRENT_CLASS } from './sidebar-item';

/**
 * Navy sidebar. From `lg` up it is a fixed-width column; below `lg` it is a bar with the mark and a
 * menu button, and the items open under it. The only client part of the shell: it reads the path
 * and owns the open state.
 */
export function Sidebar({ variant, footer }: { variant: ShellVariant; footer: ReactNode }) {
  const t = useTranslations('common');
  const pathname = usePathname();
  const [open, setOpen] = useState(false);
  const toggleRef = useRef<HTMLButtonElement>(null);
  const navId = useId();
  const items = navItems(variant);
  const current = currentHref(pathname, items);

  function closeFromKeyboard(key: string) {
    if (key !== 'Escape' || !open) return;
    setOpen(false);
    toggleRef.current?.focus();
  }

  return (
    <aside
      onKeyDown={(event) => closeFromKeyboard(event.key)}
      className="bg-linear-to-b from-brand-navy to-brand-navy-deep px-3.5 py-3 text-brand-primary-contrast lg:sticky lg:top-0 lg:flex lg:h-screen lg:w-57 lg:flex-col lg:overflow-y-auto lg:pt-6 lg:pb-4"
    >
      <div className="flex items-center justify-between gap-3 lg:mb-6 lg:px-2">
        {/* TODO(brand): replace the placeholder mark and wordmark with the official Banreservas logo. */}
        <Link
          href="/"
          className="flex min-h-11 items-center gap-2.5 rounded-xl font-extrabold tracking-wide no-underline outline-none focus-visible:outline-3 focus-visible:outline-offset-2 focus-visible:outline-white"
        >
          <ParkingSign size="sm" />
          <span>{t('appName')}</span>
        </Link>
        <button
          ref={toggleRef}
          type="button"
          aria-expanded={open}
          aria-controls={navId}
          onClick={() => setOpen((value) => !value)}
          className="min-h-11 rounded-xl border border-white/40 px-4 font-semibold outline-none hover:bg-white/10 focus-visible:outline-3 focus-visible:outline-offset-2 focus-visible:outline-white lg:hidden"
        >
          {t('nav.menu')}
        </button>
      </div>
      <nav id={navId} aria-label={t('nav.label')} className={`${open ? 'grid' : 'hidden'} gap-1.5 pt-3 lg:grid lg:pt-0`}>
        {items.map(({ key, href }) => {
          const isCurrent = href === current;
          return (
            <Link
              key={key}
              href={href}
              aria-current={isCurrent ? 'page' : undefined}
              onClick={() => setOpen(false)}
              className={`${SIDEBAR_ITEM_CLASS} ${isCurrent ? SIDEBAR_ITEM_CURRENT_CLASS : ''}`}
            >
              <NavIcon name={key} />
              {t(`nav.${key}`)}
            </Link>
          );
        })}
        {footer ? (
          <>
            <div aria-hidden="true" className="mx-1.5 my-2 border-t border-white/15" />
            {footer}
          </>
        ) : null}
      </nav>
    </aside>
  );
}
