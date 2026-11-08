import { randomBytes } from 'node:crypto';
import type { NextRequest, NextResponse } from 'next/server';

export const ACCESS_COOKIE = 'mp_access';
export const REFRESH_COOKIE = 'mp_refresh';
export const CSRF_COOKIE = 'mp_csrf';
/** 30 days, matching `mp_refresh` (spec 5.2). */
export const CSRF_MAX_AGE_SECONDS = 2_592_000;

/** Seconds shaved off the access cookie so it lapses before the JWT does. */
const ACCESS_MARGIN_SECONDS = 60;

export type TokenPair = {
  accessToken: string;
  refreshToken: string;
  expiresIn: number;
  refreshExpiresIn: number;
};

export type RefreshOutcome =
  | { kind: 'refreshed'; tokens: TokenPair }
  | { kind: 'ended' }
  | { kind: 'unavailable' };

export type SessionState = {
  /** What the refresher decided this request, or null when it did not run. */
  refresh: RefreshOutcome | null;
  csrf: { value: string; issued: boolean };
};

/** `Secure` only in production (CC-4): plain-HTTP localhost would drop the cookies otherwise. */
export function isSecureCookie(): boolean {
  return process.env.APP_ENV === 'production';
}

export function newCsrfToken(): string {
  return randomBytes(32).toString('base64url');
}

const sessionCookie = () => ({ httpOnly: true, sameSite: 'lax' as const, path: '/', secure: isSecureCookie() });

export function setSessionCookies(res: NextResponse, tokens: TokenPair): void {
  res.cookies.set(ACCESS_COOKIE, tokens.accessToken, {
    ...sessionCookie(),
    maxAge: Math.max(tokens.expiresIn - ACCESS_MARGIN_SECONDS, 1),
  });
  res.cookies.set(REFRESH_COOKIE, tokens.refreshToken, { ...sessionCookie(), maxAge: tokens.refreshExpiresIn });
}

export function clearSessionCookies(res: NextResponse): void {
  for (const name of [ACCESS_COOKIE, REFRESH_COOKIE]) {
    res.cookies.set(name, '', { ...sessionCookie(), maxAge: 0 });
  }
}

/** Readable by JS on purpose: the client echoes it in `X-CSRF-Token` (double submit). */
export function setCsrfCookie(res: NextResponse, value: string): void {
  res.cookies.set(CSRF_COOKIE, value, {
    httpOnly: false,
    sameSite: 'lax',
    path: '/',
    secure: isSecureCookie(),
    maxAge: CSRF_MAX_AGE_SECONDS,
  });
}

/**
 * Request headers for whatever runs after the proxy, with the `Cookie` header
 * rewritten to match the refresh outcome, so a handler or page in the same
 * request already sees the new tokens. Other cookies pass through untouched.
 */
export function withSessionCookieHeader(request: NextRequest, state: SessionState): Headers {
  const headers = new Headers(request.headers);
  const outcome = state.refresh;
  if (!outcome || outcome.kind === 'unavailable') return headers;

  const kept = (request.headers.get('cookie') ?? '')
    .split(';')
    .map((pair) => pair.trim())
    .filter((pair) => pair !== '')
    .filter((pair) => {
      const name = pair.split('=', 1)[0];
      return name !== ACCESS_COOKIE && name !== REFRESH_COOKIE;
    });
  if (outcome.kind === 'refreshed') {
    kept.push(`${ACCESS_COOKIE}=${outcome.tokens.accessToken}`, `${REFRESH_COOKIE}=${outcome.tokens.refreshToken}`);
  }

  if (kept.length > 0) headers.set('cookie', kept.join('; '));
  else headers.delete('cookie');
  return headers;
}

/** Writes the refresh outcome and a freshly issued CSRF token to the response. */
export function applySetCookies(res: NextResponse, state: SessionState): void {
  if (state.refresh?.kind === 'refreshed') setSessionCookies(res, state.refresh.tokens);
  if (state.refresh?.kind === 'ended') clearSessionCookies(res);
  // A refresh slides mp_csrf with mp_refresh, so an active session never outlives its token.
  if (state.csrf.issued || state.refresh?.kind === 'refreshed') setCsrfCookie(res, state.csrf.value);
}
