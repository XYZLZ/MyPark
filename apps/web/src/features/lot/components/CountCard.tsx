import type { AvailabilityFigures, LotStatus } from '@mypark/contracts';
import { useTranslations } from 'next-intl';
import { Icon } from './icons';
import { StatusPill } from './StatusPill';

/** The compact count (M2B-24): the available number, "of {capacity}", and the out of service line. A closed lot says so instead of a number. */
export function CountCard({ figures, status }: { figures: AvailabilityFigures; status: LotStatus }) {
  const t = useTranslations('availability');
  const tLot = useTranslations('lot');
  const closed = status === 'closed';
  const capacity = t('of', { total: figures.effectiveCapacity });
  const detail = figures.outOfService > 0 ? `${capacity} · ${t('outOfService', { count: figures.outOfService })}` : capacity;

  return (
    <div className="flex items-center gap-3">
      <span className="grid size-13 shrink-0 place-items-center rounded-full bg-brand-sky text-brand-navy">
        <Icon name="car" className="size-[30px]" />
      </span>
      <div>
        <p className="text-sm font-semibold text-brand-navy">{t('availableLong')}</p>
        {closed ? (
          <p className="py-1.5 text-[2.125rem] leading-none font-extrabold text-status-closed">{t('status.closed')}</p>
        ) : (
          <p className="text-5xl leading-none font-bold text-brand-navy tabular-nums">{figures.available}</p>
        )}
        <p className="text-sm text-ink-muted">{closed ? tLot('unavailable.closedBody') : detail}</p>
      </div>
      <div className="ml-auto self-start">
        <StatusPill status={status} />
      </div>
    </div>
  );
}
