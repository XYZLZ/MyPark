'use client';

import { useTranslations } from 'next-intl';
import { Button } from '@/components/ui/Button';
import { useRouter } from '@/i18n/navigation';
import { Icon } from './icons';

/** The lot section when the API is down (M2B-17). Retry re-runs the server render; the page around it keeps working. */
export function LotErrorPanel({ onRetry }: { onRetry?: () => void }) {
  const t = useTranslations('lot.error');
  const router = useRouter();

  return (
    <section className="flex flex-col items-start gap-4 rounded-panel bg-surface p-5.5 shadow-sm">
      <h2 className="flex items-center gap-2.5 text-xl font-bold text-brand-navy">
        <Icon name="info" />
        {t('title')}
      </h2>
      <Button onClick={onRetry ?? (() => router.refresh())}>{t('retry')}</Button>
    </section>
  );
}
