import { errorEnvelopeSchema } from '@mypark/contracts';
import { NextResponse, type NextRequest } from 'next/server';
import { resolveLocale } from '@/i18n/routing';
import { safeNext } from '@/lib/auth/safe-next';
import { DEFAULT_TIMEOUT_MS } from '@/lib/bff/allowlist';
import { apiUrl } from '@/lib/bff/upstream';
import { ACCESS_COOKIE, REFRESH_COOKIE, clearAccessCookie, clearSessionCookies } from './cookies';

/** A relative `Location` keeps the redirect on this origin whatever host the proxy chain reports. */
function redirectTo(location: string): NextResponse {
  return new NextResponse(null, { status: 307, headers: { Location: location, 'Cache-Control': 'no-store' } });
}

/** Adds `retried=1` to a local path, keeping its own query. */
function markRetried(next: string): string {
  const url = new URL(next, 'http://same-origin.invalid');
  url.searchParams.set('retried', '1');
  return `${url.pathname}${url.search}`;
}

const wasRetried = (next: string) => new URL(next, 'http://same-origin.invalid').searchParams.get('retried') === '1';

async function upstreamCode(request: NextRequest, access: string): Promise<{ status: number; code?: string } | null> {
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
    const parsed = errorEnvelopeSchema.safeParse(await response.json().catch(() => undefined));
    return { status: 401, code: parsed.success ? parsed.data.error.code : undefined };
  } catch {
    return null;
  }
}

/**
 * `GET /api/session/end?locale=&next=` (spec 5.6). A Server Component that sees a 401
 * cannot clear cookies, so it sends the browser here. The proxy has already run, so a
 * lapsed access cookie was refreshed. Only a revoked session, or having no cookies,
 * ends the session; every other outcome goes back to `next`. A valid session survives
 * a link from another site: with a 200 from the API nothing is touched.
 */
export async function handleSessionEnd(request: NextRequest): Promise<NextResponse> {
  const params = request.nextUrl.searchParams;
  const locale = resolveLocale(params.get('locale') ?? undefined);
  const login = `/${locale}/login?reason=session_expired`;
  const next = safeNext(params.get('next')) ?? `/${locale}`;

  const access = request.cookies.get(ACCESS_COOKIE)?.value;
  const refresh = request.cookies.get(REFRESH_COOKIE)?.value;

  if (!access && !refresh) return redirectTo(login);
  // The refresh failed transiently: `next` shows the "session unavailable" panel.
  if (!access) return redirectTo(next);

  const result = await upstreamCode(request, access);
  if (result?.status !== 401) return redirectTo(next);

  if (result.code === 'auth.session_revoked') {
    const res = redirectTo(login);
    clearSessionCookies(res);
    return res;
  }
  if (result.code === 'auth.unauthenticated') {
    // Never clear the refresh cookie here: the access token may just be one the proxy cannot trust.
    if (wasRetried(next)) return redirectTo(login);
    const res = redirectTo(markRetried(next));
    clearAccessCookie(res);
    return res;
  }
  return redirectTo(next);
}
