import { errorEnvelopeSchema } from '@mypark/contracts';
import { NextResponse, type NextRequest } from 'next/server';
import { resolveLocale } from '@/i18n/routing';
import { safeNext } from '@/lib/auth/safe-next';
import { DEFAULT_TIMEOUT_MS } from '@/lib/bff/allowlist';
import { apiUrl } from '@/lib/bff/upstream';
import {
  ACCESS_COOKIE,
  REFRESH_COOKIE,
  SESSION_RETRY_COOKIE,
  clearAccessCookie,
  clearSessionCookies,
  clearSessionRetryCookie,
  setSessionRetryCookie,
} from './cookies';

/** A relative `Location` keeps the redirect on this origin whatever host the proxy chain reports. */
function redirectTo(location: string): NextResponse {
  return new NextResponse(null, { status: 307, headers: { Location: location, 'Cache-Control': 'no-store' } });
}

async function upstreamStatus(request: NextRequest, access: string): Promise<{ status: number; code?: string } | null> {
  const headers: Record<string, string> = { Accept: 'application/json', Authorization: `Bearer ${access}` };
  const forwarded = request.headers.get('x-forwarded-for');
  if (forwarded) headers['X-Forwarded-For'] = forwarded;
  try {
    const response = await fetch(apiUrl('/v1/me'), {
      headers,
      cache: 'no-store',
      redirect: 'manual',
      signal: AbortSignal.timeout(DEFAULT_TIMEOUT_MS),
    });
    if (response.status !== 401) return { status: response.status };
    // A malformed 401 body has no code: it is still a 401 and takes the same path.
    const parsed = errorEnvelopeSchema.safeParse(await response.json().catch(() => undefined));
    return { status: 401, code: parsed.success ? parsed.data.error.code : undefined };
  } catch {
    return null;
  }
}

/**
 * `GET /api/session/end?locale=&next=` (spec 5.6). A Server Component that sees a 401
 * cannot clear cookies, so it sends the browser here. The proxy has already run, so a
 * lapsed access cookie was refreshed. Only `auth.session_revoked`, or having no cookies,
 * ends the session (CC-6). Any other 401 goes back to `next` once, with a 60 s marker
 * cookie; a second 401 with the marker leaves for login without touching the refresh
 * cookie. A valid session survives a link from another site: with a 200 nothing changes.
 */
export async function handleSessionEnd(request: NextRequest): Promise<NextResponse> {
  const params = request.nextUrl.searchParams;
  const locale = resolveLocale(params.get('locale') ?? undefined);
  const login = `/${locale}/login?reason=session_expired`;
  const next = safeNext(params.get('next')) ?? `/${locale}`;
  const marked = Boolean(request.cookies.get(SESSION_RETRY_COOKIE)?.value);

  const finish = (location: string, mutate?: (res: NextResponse) => void) => {
    const res = redirectTo(location);
    mutate?.(res);
    // The marker only lives for one retry: every exit but the retry itself drops it.
    if (marked && location !== next) clearSessionRetryCookie(res);
    return res;
  };

  const access = request.cookies.get(ACCESS_COOKIE)?.value;
  const refresh = request.cookies.get(REFRESH_COOKIE)?.value;

  if (!access && !refresh) return finish(login);
  // The refresh failed transiently: `next` shows the "session unavailable" panel.
  if (!access) return redirectTo(next);

  const result = await upstreamStatus(request, access);
  if (result?.status !== 401) {
    const res = redirectTo(next);
    if (marked) clearSessionRetryCookie(res);
    return res;
  }

  if (result.code === 'auth.session_revoked') return finish(login, clearSessionCookies);

  // Second pass with the marker: stop looping. mp_refresh is left alone, and login lets this
  // reason through even with a live access cookie.
  if (marked) return finish(login);

  const res = redirectTo(next);
  setSessionRetryCookie(res);
  // Only an `auth.unauthenticated` access token is dropped, so the proxy refreshes it.
  if (result.code === 'auth.unauthenticated') clearAccessCookie(res);
  return res;
}
