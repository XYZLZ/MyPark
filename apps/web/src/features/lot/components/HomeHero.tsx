import { useTranslations } from 'next-intl';

/**
 * The brand banner: title and subtitle over a navy gradient. The photo is a placeholder.
 * Server component; it needs nothing from the API, so it renders at once (design §6.1).
 */
export function HomeHero() {
  const t = useTranslations('lot.hero');

  return (
    <div className="relative flex min-h-62 overflow-hidden rounded-panel bg-linear-to-r from-brand-navy-deep via-brand-navy to-nav-active text-brand-primary-contrast shadow-sm">
      {/* TODO(brand): replace this placeholder with the official photo (an <img> with alt={t('photoAlt')}); hidden from assistive technology until then. */}
      <div aria-hidden="true" data-photo-placeholder className="absolute inset-0 bg-linear-to-l from-brand-sky-strong/30 to-transparent" />
      <div className="relative max-w-md px-8 py-9">
        <h1 className="text-balance text-[2rem] leading-[1.12] font-extrabold">{t('title')}</h1>
        <p className="mt-2.5 text-lg font-medium text-on-navy-muted">{t('subtitle')}</p>
        <div aria-hidden="true" className="mt-4.5 h-[5px] w-22 rounded-xs bg-brand-orange" />
      </div>
    </div>
  );
}
