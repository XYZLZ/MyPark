import { useTranslations } from 'next-intl';
import { CarShape } from './SpaceCell';

const SWATCH = 'relative inline-grid aspect-[2/3] w-4 shrink-0 place-items-center rounded-[3px]';

/** One line that repeats each cell shape with its text (design §9.3). Visible to screen readers: it is plain text. */
export function MapLegend() {
  const t = useTranslations('spaces.legend');
  return (
    <ul className="m-0 mt-3.5 flex list-none flex-wrap justify-center gap-x-5.5 gap-y-2 p-0 text-sm">
      <li className="inline-flex items-center gap-2">
        <i aria-hidden="true" className={`${SWATCH} bg-surface shadow-[inset_0_0_0_2px_var(--color-status-open)]`} />
        {t('free')}
      </li>
      <li className="inline-flex items-center gap-2">
        <i aria-hidden="true" className={`${SWATCH} bg-lot-concrete`}>
          <CarShape />
        </i>
        {t('occupied')}
      </li>
      <li className="inline-flex items-center gap-2">
        <i
          aria-hidden="true"
          style={{ backgroundImage: 'repeating-linear-gradient(45deg, var(--color-surface) 0 3px, var(--color-hairline) 3px 5px)' }}
          className={`${SWATCH} text-[0.6rem] leading-none font-extrabold text-ink shadow-[inset_0_0_0_1px_var(--color-ink-muted)]`}
        >
          ×
        </i>
        {t('outOfService')}
      </li>
    </ul>
  );
}
