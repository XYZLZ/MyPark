import type { Lot } from '@mypark/contracts';
import { useTranslations } from 'next-intl';
import type { ReactNode } from 'react';
import { Icon, type IconName } from './icons';

/** `map_url` comes from the API; only an absolute http(s) URL becomes a link. */
function safeHref(value: string): string | null {
  try {
    const url = new URL(value);
    return url.protocol === 'https:' || url.protocol === 'http:' ? url.href : null;
  } catch {
    return null;
  }
}

function Row({ icon, label, children }: { icon: IconName; label: string; children: ReactNode }) {
  return (
    <div className="flex items-start gap-3 py-2.5 not-first:border-t not-first:border-hairline">
      <span className="grid size-10 shrink-0 place-items-center rounded-control bg-brand-sky text-brand-navy">
        <Icon name={icon} className="size-5" />
      </span>
      <div>
        <dt className="text-sm font-bold text-brand-navy">{label}</dt>
        <dd className="m-0">{children}</dd>
      </div>
    </div>
  );
}

/** Address, hours and the Maps link (M2B-25). The link opens a new tab and says so in text. */
export function LotInfo({ lot }: { lot: Lot }) {
  const t = useTranslations('lot');
  const href = safeHref(lot.map_url);

  return (
    <section className="rounded-panel bg-surface px-5.5 py-5 shadow-sm">
      <h2 className="mb-3.5 flex items-center gap-2.5 text-xl font-bold text-brand-navy">
        <Icon name="info" />
        {t('info')}
      </h2>
      <dl className="m-0">
        <Row icon="pin" label={t('address')}>
          {lot.address}
        </Row>
        <Row icon="clock" label={t('hours.label')}>
          {lot.opening_hours.always_open ? t('hours.alwaysOpen') : t('hours.varies')}
        </Row>
        {href ? (
          <Row icon="external" label={t('location')}>
            <a
              href={href}
              target="_blank"
              rel="noopener noreferrer"
              className="font-semibold text-brand-primary underline underline-offset-2 outline-none focus-visible:shadow-[0_0_0_2px_var(--color-surface),0_0_0_5px_var(--color-brand-navy)]"
            >
              {t('openInMaps')}
            </a>{' '}
            <span className="text-sm text-ink-muted">{t('opensInNewTab')}</span>
          </Row>
        ) : null}
      </dl>
    </section>
  );
}
