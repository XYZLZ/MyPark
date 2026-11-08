'use client';

import { ApiError } from '@mypark/contracts';
import { useLocale, useTranslations } from 'next-intl';
import { useRef, useState } from 'react';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { apiFetch } from '@/lib/api/client';
import { hardNavigate } from '@/lib/browser/navigate';
import { translateError, type Translator } from '@/lib/i18n/translate-error';

const ROUTES = { this: '/api/v1/auth/logout', both: '/api/v1/auth/logout-all' } as const;

/** Any answer at all means done (PF3): the BFF clears the cookies whatever the status was. */
const anyStatus = () => undefined;

export function LogoutButtons({ scope }: { scope: 'this' | 'both' }) {
  const t = useTranslations();
  const locale = useLocale();
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const inFlight = useRef(false);

  async function logout() {
    if (inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    setFailure(null);
    try {
      await apiFetch(ROUTES[scope], anyStatus, { method: 'POST' });
      hardNavigate(`/${locale}`);
    } catch (error) {
      // Only a request that never got an answer lands here: nothing was cleared, so stay.
      const apiError = error instanceof ApiError ? error : new ApiError({ code: 'client.network', message: 'logout failed' });
      setFailure(translateError(t as Translator, apiError));
      inFlight.current = false;
      setBusy(false);
    }
  }

  return (
    <>
      {failure ? <Banner tone="error">{failure}</Banner> : null}
      <Button variant="quiet" loading={busy} loadingLabel={t('session.loggingOut')} onClick={() => void logout()}>
        {scope === 'both' ? t('session.logoutAll') : t('session.logout')}
      </Button>
    </>
  );
}
