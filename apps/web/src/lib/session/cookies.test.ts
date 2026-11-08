// @vitest-environment node
import { NextRequest, NextResponse } from 'next/server';
import { describe, expect, test, vi } from 'vitest';
import {
  ACCESS_COOKIE,
  CSRF_COOKIE,
  CSRF_MAX_AGE_SECONDS,
  REFRESH_COOKIE,
  applySetCookies,
  clearSessionCookies,
  newCsrfToken,
  setCsrfCookie,
  setSessionCookies,
  withSessionCookieHeader,
  type SessionState,
  type TokenPair,
} from './cookies';

const tokens: TokenPair = { accessToken: 'access-1', refreshToken: 'refresh-1', expiresIn: 900, refreshExpiresIn: 2_592_000 };
const csrf = { value: 'csrf-1', issued: false };

function requestWith(cookie: string) {
  return new NextRequest('http://localhost:3000/api/v1/me', { headers: { cookie } });
}

describe('cookie attributes', () => {
  test('access cookie lives expires_in minus 60', () => {
    const res = NextResponse.next();
    setSessionCookies(res, tokens);
    expect(res.cookies.get(ACCESS_COOKIE)).toMatchObject({
      value: 'access-1',
      maxAge: 840,
      httpOnly: true,
      sameSite: 'lax',
      path: '/',
    });
  });

  test('refresh cookie lives refresh_expires_in', () => {
    const res = NextResponse.next();
    setSessionCookies(res, tokens);
    expect(res.cookies.get(REFRESH_COOKIE)).toMatchObject({
      value: 'refresh-1',
      maxAge: 2_592_000,
      httpOnly: true,
      sameSite: 'lax',
      path: '/',
    });
  });

  test('csrf cookie is readable by JS and lasts 30 days', () => {
    const res = NextResponse.next();
    setCsrfCookie(res, 'csrf-1');
    const cookie = res.cookies.get(CSRF_COOKIE);
    expect(cookie).toMatchObject({ value: 'csrf-1', maxAge: CSRF_MAX_AGE_SECONDS, sameSite: 'lax', path: '/' });
    expect(cookie?.httpOnly).toBeFalsy();
    expect(CSRF_MAX_AGE_SECONDS).toBe(2_592_000);
  });

  test('Secure only when APP_ENV is production', () => {
    vi.stubEnv('NODE_ENV', 'production');
    vi.stubEnv('APP_ENV', 'development');
    const dev = NextResponse.next();
    setSessionCookies(dev, tokens);
    setCsrfCookie(dev, 'c');
    expect(dev.cookies.get(ACCESS_COOKIE)?.secure).toBeFalsy();
    expect(dev.cookies.get(CSRF_COOKIE)?.secure).toBeFalsy();

    vi.stubEnv('APP_ENV', 'production');
    const prod = NextResponse.next();
    setSessionCookies(prod, tokens);
    setCsrfCookie(prod, 'c');
    expect(prod.cookies.get(ACCESS_COOKIE)?.secure).toBe(true);
    expect(prod.cookies.get(REFRESH_COOKIE)?.secure).toBe(true);
    expect(prod.cookies.get(CSRF_COOKIE)?.secure).toBe(true);
  });

  test('clearSessionCookies expires both session cookies', () => {
    const res = NextResponse.next();
    clearSessionCookies(res);
    for (const name of [ACCESS_COOKIE, REFRESH_COOKIE]) {
      expect(res.cookies.get(name)).toMatchObject({ value: '', maxAge: 0, path: '/', httpOnly: true });
    }
  });

  test('newCsrfToken is 32 random bytes in base64url', () => {
    const a = newCsrfToken();
    expect(a).toMatch(/^[A-Za-z0-9_-]{43}$/);
    expect(newCsrfToken()).not.toBe(a);
  });
});

describe('withSessionCookieHeader', () => {
  const stale = 'theme=dark; mp_access=old-a; mp_refresh=old-r; mp_csrf=csrf-1';

  test('replaces tokens after a refresh', () => {
    const state: SessionState = { refresh: { kind: 'refreshed', tokens }, csrf };
    const cookie = withSessionCookieHeader(requestWith(stale), state).get('cookie') ?? '';
    expect(cookie).toContain('mp_access=access-1');
    expect(cookie).toContain('mp_refresh=refresh-1');
    expect(cookie).toContain('theme=dark');
    expect(cookie).not.toContain('old-a');
    expect(cookie).not.toContain('old-r');
  });

  test('replaces tokens when the request had no cookie header', () => {
    const state: SessionState = { refresh: { kind: 'refreshed', tokens }, csrf };
    const request = new NextRequest('http://localhost:3000/api/v1/me');
    expect(withSessionCookieHeader(request, state).get('cookie')).toBe('mp_access=access-1; mp_refresh=refresh-1');
  });

  test('removes tokens when the session ended', () => {
    const state: SessionState = { refresh: { kind: 'ended' }, csrf };
    const cookie = withSessionCookieHeader(requestWith(stale), state).get('cookie') ?? '';
    expect(cookie).not.toContain('mp_access');
    expect(cookie).not.toContain('mp_refresh');
    expect(cookie).toContain('theme=dark');
  });

  test('keeps the refresh cookie when unavailable', () => {
    const state: SessionState = { refresh: { kind: 'unavailable' }, csrf };
    const cookie = withSessionCookieHeader(requestWith('mp_refresh=old-r; theme=dark'), state).get('cookie') ?? '';
    expect(cookie).toContain('mp_refresh=old-r');
    expect(cookie).not.toContain('mp_access');
  });

  test('does not mutate the incoming request headers', () => {
    const request = requestWith(stale);
    withSessionCookieHeader(request, { refresh: { kind: 'ended' }, csrf });
    expect(request.headers.get('cookie')).toBe(stale);
  });
});

describe('applySetCookies', () => {
  test('sets tokens after a refresh and csrf only when issued', () => {
    const res = NextResponse.next();
    applySetCookies(res, { refresh: { kind: 'refreshed', tokens }, csrf: { value: 'fresh', issued: true } });
    expect(res.cookies.get(ACCESS_COOKIE)?.value).toBe('access-1');
    expect(res.cookies.get(CSRF_COOKIE)?.value).toBe('fresh');

    const quiet = NextResponse.next();
    applySetCookies(quiet, { refresh: null, csrf });
    expect(quiet.headers.getSetCookie()).toEqual([]);
  });

  test('clears both cookies when the session ended and leaves them when unavailable', () => {
    const ended = NextResponse.next();
    applySetCookies(ended, { refresh: { kind: 'ended' }, csrf });
    expect(ended.cookies.get(ACCESS_COOKIE)?.maxAge).toBe(0);
    expect(ended.cookies.get(REFRESH_COOKIE)?.maxAge).toBe(0);

    const unavailable = NextResponse.next();
    applySetCookies(unavailable, { refresh: { kind: 'unavailable' }, csrf });
    expect(unavailable.headers.getSetCookie()).toEqual([]);
  });
});
