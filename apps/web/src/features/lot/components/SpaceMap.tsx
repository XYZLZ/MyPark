import type { SpaceState } from '@mypark/contracts';
import { useTranslations } from 'next-intl';
import { SpaceCell } from './SpaceCell';
import { ZOOM_LEVELS, type ZoomLevel } from './zoom';

export { ZOOM_LEVELS, type ZoomLevel };

const BAY_COUNT = 280;
const LAWN =
  'radial-gradient(circle, var(--color-lot-tree) 0 5px, transparent 6px) 0 0 / 20px 20px';

/** Trees and lawn are decorative and carry no state. */
function Building() {
  return (
    <div aria-hidden="true" className="hidden items-center justify-center rounded-lg bg-brand-sky p-2 text-brand-navy shadow-sm md:flex">
      <svg viewBox="0 0 24 24" className="size-8" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round">
        <path d="M4 20V9l8-5 8 5v11M9 20v-6h6v6" />
      </svg>
    </div>
  );
}

/**
 * The illustrated top-down lot (design §9.3): concrete pad, lawn and trees from the map tokens,
 * and the bays in code order with no invented zones (M2B-23). The grid is hidden from assistive
 * tech; `SpacesSummary` and `FreeSpacesList` carry the same information as text. Zoom widens the
 * map inside its own scroll frame, so the page never scrolls sideways.
 */
export function SpaceMap({ spaces, ready, zoom }: { spaces: SpaceState[] | null; ready: boolean; zoom: ZoomLevel }) {
  const t = useTranslations('spaces');
  const tHome = useTranslations('home'); // web-only copy: `spaces` is shared with mobile
  const dimmed = spaces !== null && !ready;

  return (
    <div className="relative">
      <div
        role="region"
        tabIndex={0}
        aria-label={tHome('mapLabel')}
        className="overflow-auto rounded-panel focus-visible:outline-3 focus-visible:outline-offset-2 focus-visible:outline-brand-navy"
      >
        <div
          style={{ width: `${zoom * 100}%`, backgroundImage: LAWN }}
          className={`grid grid-cols-1 gap-3 rounded-panel bg-lot-lawn p-3 md:grid-cols-[15%_1fr] md:p-4 ${dimmed ? 'opacity-60' : ''}`}
        >
          <Building />
          <div aria-hidden="true" className="rounded-xl bg-lot-concrete p-2">
            <div className="grid grid-cols-10 gap-x-0.5 gap-y-1 rounded bg-surface p-1 md:grid-cols-28">
              {spaces
                ? spaces.map((space) => <SpaceCell key={space.code} code={space.code} state={space.state} />)
                : Array.from({ length: BAY_COUNT }, (_, i) => (
                    <i key={i} data-skeleton="" className="block aspect-[2/3] animate-pulse rounded-[3px] bg-hairline motion-reduce:animate-none" />
                  ))}
            </div>
          </div>
        </div>
      </div>
      {dimmed ? (
        <p className="absolute top-3 left-1/2 m-0 -translate-x-1/2 rounded-control bg-surface px-3 py-1 text-sm font-semibold text-ink shadow-md">
          {t('updating')}
        </p>
      ) : null}
    </div>
  );
}
