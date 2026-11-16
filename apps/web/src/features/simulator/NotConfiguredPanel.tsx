import { useTranslations } from 'next-intl';

/** Shown when the console route answers 404 or the simulator rejects the token (M2B-29). The live region in the console announces it. */
export function NotConfiguredPanel() {
  const t = useTranslations('simulator');
  return <section className="rounded-panel bg-surface p-5 text-ink">{t('notConfigured')}</section>;
}
