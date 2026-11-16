import { useTranslations } from 'next-intl';

/** Shown when the console route answers 404 or the simulator rejects the token (M2B-29). */
export function NotConfiguredPanel() {
  const t = useTranslations('simulator');
  return (
    <p role="status" className="rounded-panel bg-surface p-5 text-ink">
      {t('notConfigured')}
    </p>
  );
}
