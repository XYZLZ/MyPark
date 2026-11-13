'use client';

import { ApiError } from '@mypark/contracts';
import { useLocale, useTranslations } from 'next-intl';
import { useRef, useState } from 'react';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { apiFetch } from '@/lib/api/client';
import { LOGOUT_ALL_FAILED_NOTICE, LOGOUT_ALL_UNCONFIRMED } from '@/lib/bff/logout-all';
import { hardNavigate } from '@/lib/browser/navigate';
import { translateError, type Translator } from '@/lib/i18n/translate-error';

const ROUTES = { this: '/api/v1/auth/logout', both: '/api/v1/auth/logout-all' } as const;

type LogoutResult = 'confirmed' | 'unconfirmed';

/**
 * Any answer from Go means done (PF3): the BFF clears the cookies whatever the status was,
 * a 403 `auth.account_suspended` included. The one exception is the BFF's own CSRF denial,
 * a 403 `auth.forbidden` returned before anything was cleared: the user is still signed in.
 *
 * Done is not the same as confirmed. Only a 2xx proves the server revoked the sessions; the BFF
 * answers logout-all it could not confirm with `LOGOUT_ALL_UNCONFIRMED` (see `lib/bff/logout-all.ts`).
 */
function parseLogout(body: unknown, status?: number): LogoutResult {
  const code = (body as { error?: { code?: unknown } } | undefined)?.error?.code;
  if (status === 403 && code === 'auth.forbidden') throw new ApiError({ code, message: 'csrf denied', status });
  if (code === LOGOUT_ALL_UNCONFIRMED) return 'unconfirmed';
  return status !== undefined && status >= 200 && status < 300 ? 'confirmed' : 'unconfirmed';
}

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
      const result = await apiFetch(ROUTES[scope], parseLogout, { method: 'POST' });
      // This device is signed out either way; only logout-all owes the user a word when unconfirmed.
      hardNavigate(scope === 'both' && result === 'unconfirmed' ? `/${locale}?notice=${LOGOUT_ALL_FAILED_NOTICE}` : `/${locale}`);
    } catch (error) {
      // A request that never got an answer, or a CSRF denial: nothing was cleared, so stay.
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
