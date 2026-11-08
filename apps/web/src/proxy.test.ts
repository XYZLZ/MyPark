// @vitest-environment node
import { NextRequest } from 'next/server';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import type { RefreshOutcome, TokenPair } from '@/lib/session/cookies';
import proxy from './proxy';

// Only the coordinator is replaced: it is installed where the singleton lives.
const SINGLETON = Symbol.for('mypark.session.refresh');
const refresh = vi.fn<(input: { refreshToken: string; csrf: string | undefined; forwardedFor: string | null }) => Promise<RefreshOutcome>>();

const tokens: TokenPair = { accessToken: 'access-1', refreshToken: 'refresh-2', expiresIn: 900, refreshExpiresIn: 2_592_000 };

beforeEach(() => {
  refresh.mockReset();
  refresh.mockResolvedValue({ kind: 'refreshed', tokens });
  (globalThis as Record<symbol, unknown>)[SINGLETON] = { refresh };
});

const get = (path: string, cookie?: string, headers: Record<string, string> = {}) =>
  new NextRequest(`http://localhost:3000${path}`, { headers: { ...(cookie ? { cookie } : {}), ...headers } });

const EXPIRED = 'mp_refresh=refresh-1; mp_csrf=csrf-1';

describe('proxy session refresh', () => {
  test('proxy forwards the rewritten Cookie header to route handlers', async () => {
    const res = await proxy(get('/api/v1/vehicles', EXPIRED));
    const forwarded = res.headers.get('x-middleware-request-cookie') ?? '';
    expect(forwarded).toContain('mp_access=access-1');
    expect(forwarded).toContain('mp_refresh=refresh-2');
    expect(forwarded).not.toContain('refresh-1');
  });

  test('page requests keep the next-intl rewrite with new tokens', async () => {
    const res = await proxy(get('/es-DO/vehicles', EXPIRED));
    // With localePrefix "always", an already-prefixed path is passed on (no rewrite), and next-intl has run.
    expect(res.headers.get('x-middleware-next')).toBe('1');
    expect(res.headers.get('x-middleware-request-x-next-intl-locale')).toBe('es-DO');
    expect(res.headers.get('location')).toBeNull();
    expect(res.headers.get('x-middleware-request-cookie')).toContain('mp_access=access-1');
  });

  test('refreshes only when access is absent and refresh is present', async () => {
    await proxy(get('/api/v1/vehicles', EXPIRED, { 'x-forwarded-for': '203.0.113.9' }));
    expect(refresh).toHaveBeenCalledTimes(1);
    expect(refresh).toHaveBeenCalledWith({ refreshToken: 'refresh-1', csrf: 'csrf-1', forwardedFor: '203.0.113.9' });

    refresh.mockClear();
    await proxy(get('/api/v1/vehicles'));
    await proxy(get('/api/v1/vehicles', 'mp_csrf=csrf-1'));
    expect(refresh).not.toHaveBeenCalled();
  });

  test('does not refresh when mp_access is present', async () => {
    await proxy(get('/api/v1/vehicles', 'mp_access=a; mp_refresh=r; mp_csrf=c'));
    await proxy(get('/es-DO/vehicles', 'mp_access=a; mp_refresh=r; mp_csrf=c'));
    expect(refresh).not.toHaveBeenCalled();
  });

  test('passes an undefined csrf and a null forwardedFor when there are none', async () => {
    await proxy(get('/api/v1/vehicles', 'mp_refresh=refresh-1'));
    expect(refresh).toHaveBeenCalledWith({ refreshToken: 'refresh-1', csrf: undefined, forwardedFor: null });
  });

  test('issues mp_csrf when missing and keeps an existing one', async () => {
    const issued = await proxy(get('/es-DO/login'));
    expect(issued.cookies.get('mp_csrf')?.value).toMatch(/^[A-Za-z0-9_-]{43}$/);
    const kept = await proxy(get('/es-DO/login', 'mp_csrf=csrf-1'));
    expect(kept.cookies.get('mp_csrf')).toBeUndefined();
  });

  test('sets new cookies on the response after a refresh', async () => {
    const res = await proxy(get('/api/v1/vehicles', EXPIRED));
    expect(res.cookies.get('mp_access')).toMatchObject({ value: 'access-1', httpOnly: true, maxAge: 840 });
    expect(res.cookies.get('mp_refresh')).toMatchObject({ value: 'refresh-2', httpOnly: true });
  });

  test('clears both cookies when the session ended', async () => {
    refresh.mockResolvedValue({ kind: 'ended' });
    const res = await proxy(get('/api/v1/vehicles', EXPIRED));
    expect(res.cookies.get('mp_access')).toMatchObject({ value: '', maxAge: 0 });
    expect(res.cookies.get('mp_refresh')).toMatchObject({ value: '', maxAge: 0 });
    expect(res.headers.get('x-middleware-request-cookie') ?? '').not.toContain('mp_refresh');
  });

  test('leaves cookies alone when refresh is unavailable', async () => {
    refresh.mockResolvedValue({ kind: 'unavailable' });
    const res = await proxy(get('/api/v1/vehicles', EXPIRED));
    expect(res.cookies.get('mp_access')).toBeUndefined();
    expect(res.cookies.get('mp_refresh')).toBeUndefined();
    expect(res.headers.get('x-middleware-request-cookie')).toContain('mp_refresh=refresh-1');
  });
});

describe('proxy routing', () => {
  test('/api/session/end is treated as an API route, not a locale path', async () => {
    const res = await proxy(get('/api/session/end', EXPIRED));
    expect(res.headers.get('location')).toBeNull();
    expect(res.headers.get('x-middleware-rewrite')).toBeNull();
    expect(res.headers.get('x-middleware-next')).toBe('1');
    expect(res.cookies.get('mp_access')?.value).toBe('access-1');
  });

  test('still redirects an unsupported locale without touching the session', async () => {
    const res = await proxy(get('/fr/vehicles', EXPIRED));
    expect(res.headers.get('location')).toContain('/es-DO/vehicles');
    expect(refresh).not.toHaveBeenCalled();
  });
});

describe('proxy page guard', () => {
  test('the page guard runs after the refresh', async () => {
    // Access lapsed, refresh succeeds: the guard sees the new cookies, so no redirect to login.
    const kept = await proxy(get('/es-DO/vehicles', EXPIRED));
    expect(kept.headers.get('location')).toBeNull();
    expect(kept.cookies.get('mp_access')?.value).toBe('access-1');

    // Refresh ends the session: the guard sees no cookies, redirects, and the redirect still clears them.
    refresh.mockResolvedValue({ kind: 'ended' });
    const ended = await proxy(get('/es-DO/vehicles', EXPIRED));
    expect(new URL(ended.headers.get('location') ?? '').pathname).toBe('/es-DO/login');
    expect(ended.cookies.get('mp_refresh')).toMatchObject({ value: '', maxAge: 0 });
  });

  test('a freshly refreshed session is sent away from login', async () => {
    const res = await proxy(get('/es-DO/login', EXPIRED));
    expect(new URL(res.headers.get('location') ?? '').pathname).toBe('/es-DO/vehicles');
    expect(res.cookies.get('mp_access')?.value).toBe('access-1');
  });

  test('API routes are never page-guarded', async () => {
    const res = await proxy(get('/api/v1/vehicles'));
    expect(res.headers.get('location')).toBeNull();
  });
});
