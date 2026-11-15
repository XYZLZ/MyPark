import type { AvailabilityFigures, LotStatus } from '@mypark/contracts';
import { useTranslations } from 'next-intl';
import { Icon, type IconName } from './icons';
import { StatusPill } from './StatusPill';

type Tile = { icon: IconName; tone: string; label: string; value: string; size: string; note?: string };

/**
 * The gate counter on the home (design §4.3): total with the status pill, then Disponibles (the
 * largest), Ocupados and Fuera de servicio. Each figure is a description item, so a screen reader
 * reads "Disponibles 211" as plain text; the live region only announces changes (M2B-22).
 */
export function StatTiles({ figures, status }: { figures: AvailabilityFigures; status: LotStatus }) {
  const t = useTranslations('availability');
  const tLegend = useTranslations('spaces.legend');
  const closed = status === 'closed';

  const tiles: Tile[] = [
    {
      icon: 'check',
      tone: 'text-status-open',
      label: t('available'),
      value: closed ? t('status.closed') : String(figures.available),
      size: closed ? 'text-3xl' : 'text-[2.375rem]',
      note: closed ? undefined : t('of', { total: figures.effectiveCapacity }),
    },
    { icon: 'car', tone: 'text-space-occupied', label: t('occupied'), value: String(figures.occupied), size: 'text-3xl' },
    { icon: 'ban', tone: 'text-status-closed', label: tLegend('outOfService'), value: String(figures.outOfService), size: 'text-3xl' },
  ];

  return (
    <div className="flex flex-col">
      <div className="flex items-center gap-4 px-5 pt-4 pb-3.5">
        <Icon name="car" className="size-14 text-brand-navy" />
        <dl className="m-0">
          <div className="flex flex-col-reverse">
            <dt className="font-semibold text-brand-navy">{t('total')}</dt>
            <dd className="m-0 text-[3.375rem] leading-none font-extrabold text-brand-navy tabular-nums">{figures.total}</dd>
          </div>
        </dl>
        <div className="ml-auto self-start">
          <StatusPill status={status} />
        </div>
      </div>
      <dl className="m-0 grid grid-cols-3 gap-0.5 bg-brand-sky-strong">
        {tiles.map((tile) => (
          <div key={tile.icon} className="flex flex-col-reverse items-center gap-0.5 bg-surface px-2 py-3.5 text-center">
            <dt className="text-sm font-medium">{tile.label}</dt>
            <dd className="m-0 grid justify-items-center gap-0.5">
              <Icon name={tile.icon} className={`size-[30px] ${tile.tone}`} />
              <span className={`leading-[1.05] font-extrabold tabular-nums ${tile.size} ${tile.icon === 'check' ? 'text-status-open' : 'text-ink'}`}>
                {tile.value}
              </span>
              {tile.note ? <span className="text-xs text-ink-muted">{tile.note}</span> : null}
            </dd>
          </div>
        ))}
      </dl>
    </div>
  );
}
