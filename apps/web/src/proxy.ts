import createMiddleware from 'next-intl/middleware';
import { NextResponse, type NextRequest } from 'next/server';
import { routing, unsupportedLocaleRedirect } from './i18n/routing';

const intl = createMiddleware(routing);

// Locale routing only for now; session refresh is added in M1.
export default function proxy(request: NextRequest) {
  const target = unsupportedLocaleRedirect(request.nextUrl.pathname);
  if (target) {
    const url = request.nextUrl.clone();
    url.pathname = target;
    return NextResponse.redirect(url);
  }
  return intl(request);
}

export const config = {
  // Everything except API routes, Next internals and files with an extension.
  matcher: '/((?!api|trpc|_next|_vercel|.*\\..*).*)',
};
