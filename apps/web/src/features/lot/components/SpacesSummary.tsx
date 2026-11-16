import { countSpaces, type SpaceState } from '@mypark/contracts';
import { useTranslations } from 'next-intl';

/** The sentence screen readers get instead of 280 cells, and the note that says where the map comes from (M2B-22). */
export function SpacesSummary({ spaces }: { spaces: SpaceState[] }) {
  const t = useTranslations('spaces');
  const { free, occupied, outOfService } = countSpaces(spaces);
  return (
    <>
      <p className="m-0 mt-3 text-center text-sm text-ink-muted">{t('note')}</p>
      <p className="m-0 mt-2.5">{t('summary', { free, occupied, oos: outOfService })}</p>
    </>
  );
}
