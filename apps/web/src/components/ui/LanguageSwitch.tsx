'use client';

import { useLocale, useTranslations } from 'next-intl';
import { Link, usePathname } from '@/i18n/navigation';
import { locales, type Locale } from '@/i18n/routing';

/**
 * One link per language. Each carries `lang`/`hrefLang` so a screen reader
 * speaks its name in that language. The current one is marked with
 * `aria-current` and a filled pill, so color is not the only signal.
 */
export function LanguageSwitch({ onSwitch }: { onSwitch?: (locale: Locale) => void }) {
  const t = useTranslations('common.language');
  const current = useLocale();
  const pathname = usePathname();

  return (
    <div role="group" aria-label={t('label')} className="flex gap-1 text-sm">
      {locales.map((locale) => {
        const active = locale === current;
        return (
          <Link
            key={locale}
            href={pathname}
            locale={locale}
            lang={locale}
            hrefLang={locale}
            aria-label={t(locale)}
            aria-current={active ? 'true' : undefined}
            onClick={() => onSwitch?.(locale)}
            className={`inline-flex min-h-11 min-w-11 items-center justify-center rounded-full px-3 no-underline outline-none focus-visible:shadow-[0_0_0_3px_var(--color-surface)] ${
              active ? 'bg-white/20 font-semibold' : 'hover:bg-white/10'
            }`}
          >
            <span aria-hidden="true">{locale.slice(0, 2).toUpperCase()}</span>
          </Link>
        );
      })}
    </div>
  );
}
