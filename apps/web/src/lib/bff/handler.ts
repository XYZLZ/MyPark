import { NextResponse, type NextRequest } from 'next/server';
import { ACCESS_COOKIE, REFRESH_COOKIE, clearSessionCookies } from '@/lib/session/cookies';
import { matchBffRoute } from './allowlist';
import { verifyMutation } from './csrf';
import { apiUrl, bffError, forwardedHeaders, passthrough } from './upstream';

const MUTATIONS = new Set(['POST', 'PATCH', 'DELETE']);
const NO_BODY = new Set(['GET', 'HEAD']);

const noContent = () => new NextResponse(null, { status: 204, headers: { 'Cache-Control': 'no-store' } });

/**
 * The BFF catch-all (spec 5.5). Allow-list, CSRF, bearer from the cookie, then
 * a pass-through of status and body. The proxy has already refreshed the
 * session, so the cookies read here are fresh.
 */
export async function handleBff(request: NextRequest, segments: string[]): Promise<NextResponse> {
  const route = matchBffRoute(request.method, segments);
  if (!route) return bffError(404, 'http.not_found');

  if (MUTATIONS.has(request.method)) {
    const denied = verifyMutation(request);
    if (denied) return denied;
  }

  const headers: Record<string, string> = { Accept: 'application/json', ...forwardedHeaders(request) };

  if (route.auth === 'bearer') {
    const access = request.cookies.get(ACCESS_COOKIE)?.value;
    if (!access) {
      if (route.after === 'clear-always') {
        // The user asked to leave, so a transient refresh failure must not trap them (PF4).
        // The refresh token is not revoked server-side in this case.
        const res = noContent();
        clearSessionCookies(res);
        return res;
      }
      return request.cookies.has(REFRESH_COOKIE) ? bffError(503, 'internal.unexpected') : bffError(401, 'auth.unauthenticated');
    }
    headers.Authorization = `Bearer ${access}`;
  }

  let body: string | undefined;
  if (!NO_BODY.has(request.method)) {
    const text = await request.text();
    if (text !== '') {
      body = text;
      headers['Content-Type'] = request.headers.get('content-type') ?? 'application/json';
    }
  }

  let response: NextResponse;
  try {
    const upstream = await fetch(apiUrl(route.upstreamPath), {
      method: request.method,
      headers,
      body,
      cache: 'no-store',
      redirect: 'manual',
      signal: AbortSignal.timeout(route.timeoutMs),
    });
    // A logout that Go says is unauthenticated means the session is already gone: that is success.
    response =
      route.after === 'clear-always' && upstream.status === 401 ? noContent() : passthrough(upstream, await upstream.text());
    if (route.after === 'clear-always' || (route.after === 'clear-on-204' && upstream.status === 204)) {
      clearSessionCookies(response);
    }
  } catch {
    response = bffError(502, 'internal.unexpected');
    // Logout clears cookies whatever happened upstream (PF3).
    if (route.after === 'clear-always') clearSessionCookies(response);
  }
  return response;
}
