'use client';

import { useTranslations } from 'next-intl';
import { Button } from '@/components/ui/Button';
import { useRouter } from '@/i18n/navigation';

/** The refresh failed transiently. The session is intact, so the only action is to retry, never to log out. */
export function SessionUnavailable() {
  const t = useTranslations('session.unavailable');
  const router = useRouter();

  return (
    <div className="mx-auto flex w-full max-w-xl flex-1 flex-col items-start gap-4 px-4 py-16">
      <h1 className="text-2xl font-bold text-balance">{t('title')}</h1>
      <p className="text-ink-muted">{t('body')}</p>
      <Button onClick={() => router.refresh()}>{t('retry')}</Button>
    </div>
  );
}
