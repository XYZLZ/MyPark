'use client';

import { useTranslations } from 'next-intl';
import { startTransition } from 'react';
import { SiteHeader } from '@/components/SiteHeader';
import { Button } from '@/components/ui/Button';
import { LanguageSwitch } from '@/components/ui/LanguageSwitch';
import { useRouter } from '@/i18n/navigation';

// `error.message` is never shown: it can carry server detail, and a production build
// replaces it anyway. The message is translated and generic.
export default function ErrorPage({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  const t = useTranslations();
  const router = useRouter();

  return (
    <>
      <SiteHeader appName={t('common.appName')} languageSwitch={<LanguageSwitch />} />
      <main id="main" className="mx-auto flex w-full max-w-xl flex-1 flex-col items-start gap-4 px-4 py-16">
        <h1 className="text-2xl font-bold text-balance">{t('errorPage.title')}</h1>
        <p className="text-ink-muted">{t('errors.fallback')}</p>
        <Button
          onClick={() =>
            // A server render failed: re-fetch it, then re-render the boundary.
            startTransition(() => {
              router.refresh();
              reset();
            })
          }
        >
          {t('errorPage.retry')}
        </Button>
      </main>
    </>
  );
}
