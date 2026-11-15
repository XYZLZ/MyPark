import type { SpaceState } from '@mypark/contracts';
import { useTranslations } from 'next-intl';

/** The free bays as text, for keyboard and screen-reader users. Native `<details>`: no script, opens with Enter or Space. */
export function FreeSpacesList({ spaces }: { spaces: SpaceState[] }) {
  const t = useTranslations('spaces');
  const free = spaces.filter((space) => space.state === 'free').map((space) => space.code);
  return (
    <details className="mt-1.5 text-sm">
      <summary className="min-h-11 cursor-pointer py-2.5 font-semibold text-brand-navy">{t('listFree')}</summary>
      <p className="m-0 font-mono break-words">{free.join(', ')}</p>
    </details>
  );
}
