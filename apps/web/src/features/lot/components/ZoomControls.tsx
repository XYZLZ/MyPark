import { useTranslations } from 'next-intl';
import { ZOOM_LEVELS, type ZoomLevel } from './zoom';

const BUTTON =
  'grid size-11 place-items-center bg-surface text-[1.375rem] leading-none font-bold text-brand-navy ' +
  'focus-visible:outline-3 focus-visible:-outline-offset-3 focus-visible:outline-brand-navy aria-disabled:bg-disabled-bg aria-disabled:text-disabled-ink';

/** Zoom in and out by one step; labelled, 44 px. At a bound the button is `aria-disabled`, not `disabled`, so keyboard focus stays on it. */
export function ZoomControls({ zoom, onZoom }: { zoom: ZoomLevel; onZoom: (next: ZoomLevel) => void }) {
  const t = useTranslations('spaces');
  const index = ZOOM_LEVELS.indexOf(zoom);
  const atMin = index <= 0;
  const atMax = index >= ZOOM_LEVELS.length - 1;
  return (
    <div className="grid overflow-hidden rounded-control bg-surface shadow-md">
      <button type="button" aria-label={t('zoomIn')} aria-disabled={atMax} onClick={() => atMax || onZoom(ZOOM_LEVELS[index + 1]!)} className={BUTTON}>
        <span aria-hidden="true">+</span>
      </button>
      <button type="button" aria-label={t('zoomOut')} aria-disabled={atMin} onClick={() => atMin || onZoom(ZOOM_LEVELS[index - 1]!)} className={`${BUTTON} border-t border-hairline`}>
        <span aria-hidden="true">−</span>
      </button>
    </div>
  );
}
