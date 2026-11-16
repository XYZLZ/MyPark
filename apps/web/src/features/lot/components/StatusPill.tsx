import type { LotStatus } from '@mypark/contracts';
import { useTranslations } from 'next-intl';

// Full class names, never built from the status: Tailwind only keeps what it can read.
const PILLS = {
  open: { tint: 'bg-status-open-tint text-status-open', mark: 'size-2.5 rounded-full' },
  full: { tint: 'bg-status-full-tint text-status-full', mark: 'size-2.5 rounded-xs' },
  closed: { tint: 'bg-status-closed-tint text-status-closed', mark: 'h-1 w-3 rounded-xs' },
} as const;

/** Icon and text on the status tint: a circle for open, a square for full, a bar for closed (never color alone). */
export function StatusPill({ status }: { status: LotStatus }) {
  const t = useTranslations('availability.status');
  const { tint, mark } = PILLS[status];
  return (
    <span className={`inline-flex h-[30px] items-center gap-[7px] rounded-full px-3 text-sm font-bold whitespace-nowrap ${tint}`}>
      <i aria-hidden="true" className={`block shrink-0 bg-current ${mark}`} />
      {t(status)}
    </span>
  );
}
