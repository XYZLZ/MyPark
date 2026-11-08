'use client';

import { useTranslations } from 'next-intl';
import { AuthPanel } from '@/components/ui/AuthPanel';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { useRouter } from '@/i18n/navigation';

/** Sign-up needs the current legal versions; without them it offers a retry instead of a form. */
export function LegalUnavailable() {
  const t = useTranslations('auth.register.unavailable');
  const router = useRouter();

  return (
    <AuthPanel title={t('title')}>
      <Banner tone="error">{t('body')}</Banner>
      <Button className="w-full" onClick={() => router.refresh()}>
        {t('retry')}
      </Button>
    </AuthPanel>
  );
}
