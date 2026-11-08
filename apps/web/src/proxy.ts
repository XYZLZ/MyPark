import createMiddleware from 'next-intl/middleware';
import { NextRequest, NextResponse } from 'next/server';
import { routing, unsupportedLocaleRedirect } from './i18n/routing';
import { applySetCookies, withSessionCookieHeader } from './lib/session/cookies';
import { ensureSession } from './lib/session/refresh';

const intl = createMiddleware(routing);

/**
 * The only place a session is refreshed (spec 5.3): cookies cannot be set while
 * a Server Component renders, so route handlers and pages read tokens that are
 * already fresh. Pages get locale routing, API paths pass straight through.
 */
export default async function proxy(request: NextRequest): Promise<NextResponse> {
  const { pathname } = request.nextUrl;
  // `/api/session/end` is an API route, not a locale path: test for `/api/`, not `/api/v1/`.
  const isApi = pathname.startsWith('/api/');

  if (!isApi) {
    const target = unsupportedLocaleRedirect(pathname);
    if (target) {
      const url = request.nextUrl.clone();
      url.pathname = target;
      return NextResponse.redirect(url);
    }
  }

  const session = await ensureSession(request);
  const headers = withSessionCookieHeader(request, session);
  const response = isApi
    ? NextResponse.next({ request: { headers } })
    : intl(new NextRequest(request, { headers }));

  applySetCookies(response, session);
  return response;
}

export const config = {
  // Pages (everything except API routes, Next internals and files with an
  // extension), the BFF catch-all and the session routes. `/api/auth/login`
  // stays out on purpose: a login needs no session.
  matcher: ['/((?!api|trpc|_next|_vercel|.*\\..*).*)', '/api/v1/:path*', '/api/session/:path*'],
};
