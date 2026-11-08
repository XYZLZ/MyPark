'use client';

import { useTranslations } from 'next-intl';
import { Link, usePathname } from '@/i18n/navigation';

const LINKS = [
  { href: '/vehicles', key: 'vehicles' },
  { href: '/profile', key: 'profile' },
] as const;

/** Header nav for a signed-in driver. The current section carries `aria-current` and a filled pill. */
export function DriverNav() {
  const t = useTranslations('common.nav');
  const pathname = usePathname();

  return (
    <>
      {LINKS.map(({ href, key }) => {
        const current = pathname === href || pathname.startsWith(`${href}/`);
        return (
          <Link
            key={href}
            href={href}
            aria-current={current ? 'page' : undefined}
            className={`inline-flex min-h-11 items-center rounded-full px-3 no-underline outline-none focus-visible:shadow-[0_0_0_3px_var(--color-surface)] ${
              current ? 'bg-white/20 font-semibold' : 'hover:bg-white/10'
            }`}
          >
            {t(key)}
          </Link>
        );
      })}
    </>
  );
}
