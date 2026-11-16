import { useTranslations } from 'next-intl';
import type { Ref } from 'react';

/**
 * Shown when the console route answers 404 or the simulator rejects the token (M2B-29). The live region in the
 * console announces it, so the panel is not one. The heading takes focus when a failed action ends here, so
 * focus is not dropped on the page when the button that was pressed goes away.
 */
export function NotConfiguredPanel({ headingRef }: { headingRef?: Ref<HTMLHeadingElement> }) {
  const t = useTranslations('simulator');
  return (
    <section aria-labelledby="sim-status-title" className="rounded-panel bg-surface p-5 text-ink">
      <h2 ref={headingRef} tabIndex={-1} id="sim-status-title" className="mb-3 text-lg font-bold outline-none">
        {t('status.title')}
      </h2>
      <p>{t('notConfigured')}</p>
    </section>
  );
}
