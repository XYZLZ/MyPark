import { NextResponse, type NextRequest } from 'next/server';
import { safeNext } from '@/lib/auth/safe-next';
import { defaultLocale, locales, type Locale } from '@/i18n/routing';
import { ACCESS_COOKIE, REFRESH_COOKIE } from './cookies';

const PROTECTED = [
  { prefix: '/vehicles', login: '/login' },
  { prefix: '/profile', login: '/login' },
  { prefix: '/admin', login: '/admin/login' },
] as const;
const ADMIN_LOGIN = '/admin/login';
const SIGNED_OUT_ONLY = new Set(['/login', '/register', ADMIN_LOGIN]);

const isUnder = (path: string, prefix: string) => path === prefix || path.startsWith(`${prefix}/`);

/** Splits an optional leading locale off the path: `/en/vehicles` -> `en`, `/vehicles`. */
function splitLocale(pathname: string): { locale: Locale; path: string } {
  const [, first = '', ...rest] = pathname.split('/');
  const locale = locales.find((candidate) => candidate === first);
  if (!locale) return { locale: defaultLocale, path: pathname };
  return { locale, path: `/${rest.join('/')}` };
}

function redirectTo(request: NextRequest, pathname: string, next?: string): NextResponse {
  const url = request.nextUrl.clone();
  url.pathname = pathname;
  url.search = next ? `?${new URLSearchParams({ next })}` : '';
  return NextResponse.redirect(url);
}

/**
 * The page half of the guards (spec 5.6), run by `proxy.ts` after the refresh, so
 * `request` carries the cookies the page will see. With no session cookies at all,
 * protected pages go to the matching login; with a live access cookie, the login
 * and register pages go to Vehicles. Cookies are tested by `?.value`, because a
 * cleared cookie is still listed with no value. A refresh cookie alone changes
 * nothing: the access cookie may only be lapsed, and a layout decides from there.
 * Returns null when the request should continue.
 */
export function guardPublicAndProtected(request: NextRequest): NextResponse | null {
  const { locale, path } = splitLocale(request.nextUrl.pathname);
  const access = Boolean(request.cookies.get(ACCESS_COOKIE)?.value);
  const refresh = Boolean(request.cookies.get(REFRESH_COOKIE)?.value);

  // `/api/session/end` gives up with this reason while the access cookie may still be live:
  // bouncing it to Vehicles would restart the loop it just ended.
  const sessionEnded = request.nextUrl.searchParams.get('reason') === 'session_expired';

  if (access && !sessionEnded && SIGNED_OUT_ONLY.has(path)) return redirectTo(request, `/${locale}/vehicles`);

  if (access || refresh || path === ADMIN_LOGIN) return null;
  const area = PROTECTED.find(({ prefix }) => isUnder(path, prefix));
  if (!area) return null;

  const next = safeNext(`${request.nextUrl.pathname}${request.nextUrl.search}`) ?? undefined;
  return redirectTo(request, `/${locale}${area.login}`, next);
}
