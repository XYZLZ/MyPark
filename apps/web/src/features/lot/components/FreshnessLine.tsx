import type { ConnectionStatus } from '@mypark/contracts';
import { useLocale, useTranslations } from 'next-intl';
import { formatClock } from '@/lib/format/time';
import type { Locale } from '@/i18n/routing';
import { Icon } from './icons';

type FreshnessLineProps = {
  connection: ConnectionStatus;
  /** `availability.updated_at`: the last count change (M2B-14). */
  updatedAt: string;
  /** Client time of the last frame; `null` before the first one. */
  lastFrameAt: number | null;
  /** Client time of the server snapshot. */
  snapshotAt: number;
};

/**
 * Two times, on purpose (M2B-14). Live says when the count last changed; stale says how old the
 * values on screen are, which only the last frame time answers. Text and an icon, never color alone.
 * `formatClock` gives `''` for a bad time, so the time part is left out instead of dangling.
 */
export function FreshnessLine({ connection, updatedAt, lastFrameAt, snapshotAt }: FreshnessLineProps) {
  const t = useTranslations('availability');
  const locale = useLocale() as Locale;

  if (connection === 'stale') {
    const time = formatClock(lastFrameAt ?? snapshotAt, locale);
    return (
      <div className="flex flex-wrap items-center gap-2 rounded-control bg-status-stale-tint px-3.5 py-2.5 text-sm font-semibold text-status-stale">
        <Icon name="clock" className="size-4" />
        <span>{t('stale', { time }).trim()}</span>
        <span className="basis-full" />
        <span
          aria-hidden="true"
          className="size-3.5 animate-spin rounded-full border-2 border-status-stale/25 border-t-status-stale motion-reduce:animate-none"
        />
        <span>{t('reconnecting')}</span>
      </div>
    );
  }

  if (connection === 'unavailable') {
    const time = formatClock(snapshotAt, locale);
    return (
      <div className="flex items-center gap-2 rounded-control bg-status-closed-tint px-3.5 py-2.5 text-sm font-semibold text-status-closed">
        <Icon name="info" className="size-4" />
        {time === '' ? t('unavailable') : t('unavailableAt', { time })}
      </div>
    );
  }

  if (connection === 'connecting') {
    return <p className="text-sm text-ink-muted">{t('connecting')}</p>;
  }

  const lastChange = formatClock(updatedAt, locale);
  return (
    <p className="flex flex-wrap items-center gap-2 text-sm text-ink-muted">
      <span aria-hidden="true" className="size-2.5 rounded-full bg-status-open shadow-[0_0_0_3px_var(--color-status-open-tint)]" />
      <span>
        <b className="font-bold text-status-open">{t('live')}</b>
        {lastChange === '' ? null : ` · ${t('lastChange', { time: lastChange })}`}
      </span>
    </p>
  );
}
