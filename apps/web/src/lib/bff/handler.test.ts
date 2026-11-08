// @vitest-environment node
import { errorBody, successBody, vehicleFixture } from '@mypark/contracts/fixtures';
import type { NextResponse } from 'next/server';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { bffRequest } from '@/testing/bff-request';
import { BFF_ENTRIES } from './allowlist';
import { handleBff } from './handler';

const fetchMock = vi.fn<typeof fetch>();
const SESSION = { mp_access: 'access-1', mp_refresh: 'refresh-1' };

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal('fetch', fetchMock);
  vi.stubEnv('API_INTERNAL_URL', 'http://api.test');
  vi.stubEnv('WEB_ALLOWED_ORIGINS', 'http://localhost:3000');
});

const upstreamOk = (body: unknown = successBody(vehicleFixture), init: ResponseInit = {}) =>
  new Response(JSON.stringify(body), { status: 200, headers: { 'content-type': 'application/json' }, ...init });

const upstreamCall = (n = 0) => {
  const [url, init] = fetchMock.mock.calls[n]!;
  const headers = new Headers(init?.headers);
  return { url: String(url), init: init!, headers };
};

const cleared = (res: NextResponse) =>
  res.cookies.get('mp_access')?.maxAge === 0 && res.cookies.get('mp_refresh')?.maxAge === 0;

describe('routing and CSRF', () => {
  test('unknown path returns 404 http.not_found without calling upstream', async () => {
    const res = await handleBff(bffRequest('/api/v1/admin/admins', { cookies: SESSION }), ['admin', 'admins']);
    expect(res.status).toBe(404);
    expect((await res.json()).error.code).toBe('http.not_found');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test('rejects a mutation with bad CSRF before calling upstream', async () => {
    const res = await handleBff(
      bffRequest('/api/v1/vehicles', { method: 'POST', cookies: SESSION, body: {}, csrf: 'wrong' }),
      ['vehicles'],
    );
    expect(res.status).toBe(403);
    expect((await res.json()).error.code).toBe('auth.forbidden');
    const noOrigin = await handleBff(
      bffRequest('/api/v1/vehicles', { method: 'POST', cookies: SESSION, body: {}, origin: null }),
      ['vehicles'],
    );
    expect(noOrigin.status).toBe(403);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  const ID = vehicleFixture.id;
  // Derived from the allow-list so a new mutating route cannot be left out.
  const mutating: Array<[string, string[]]> = BFF_ENTRIES.filter((e) => e.method !== 'GET').map((e) => [
    e.method,
    e.path.split('/').map((part) => (part === ':id' ? ID : part)),
  ]);

  test('covers all 14 mutating allow-list routes', () => {
    expect(mutating).toHaveLength(14);
  });

  test.each(mutating)('%s %j requires Origin and a matching CSRF token', async (method, segments) => {
    const path = `/api/v1/${segments.join('/')}`;
    const bad: Array<Parameters<typeof bffRequest>[1]> = [
      { csrf: 'wrong' },
      { csrf: null },
      { origin: null },
      { origin: 'http://evil.example' },
    ];
    for (const override of bad) {
      const res = await handleBff(bffRequest(path, { method, cookies: SESSION, body: {}, ...override }), segments);
      expect(res.status, `${method} ${path} ${JSON.stringify(override)}`).toBe(403);
      expect(res.cookies.get('mp_access')).toBeUndefined();
    }
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test('reads need no CSRF token', async () => {
    fetchMock.mockResolvedValue(upstreamOk());
    const res = await handleBff(bffRequest('/api/v1/vehicles', { cookies: SESSION, csrf: null, origin: null }), ['vehicles']);
    expect(res.status).toBe(200);
  });
});

describe('proxying', () => {
  test('adds the bearer token on bearer routes only', async () => {
    fetchMock.mockImplementation(async () => upstreamOk());
    await handleBff(bffRequest('/api/v1/vehicles', { cookies: SESSION }), ['vehicles']);
    expect(upstreamCall(0).url).toBe('http://api.test/v1/vehicles');
    expect(upstreamCall(0).headers.get('authorization')).toBe('Bearer access-1');

    await handleBff(bffRequest('/api/v1/legal/current', { cookies: SESSION }), ['legal', 'current']);
    expect(upstreamCall(1).url).toBe('http://api.test/v1/legal/current');
    expect(upstreamCall(1).headers.has('authorization')).toBe(false);
  });

  test('forwards the body and content type, and never the query string', async () => {
    fetchMock.mockResolvedValue(upstreamOk());
    const body = { plate: 'A123456', make: 'Kia', model: 'Rio', color: 'Red' };
    await handleBff(bffRequest('/api/v1/vehicles?debug=1', { method: 'POST', cookies: SESSION, body }), ['vehicles']);
    const call = upstreamCall();
    expect(call.url).toBe('http://api.test/v1/vehicles');
    expect(call.init.method).toBe('POST');
    expect(JSON.parse(String(call.init.body))).toEqual(body);
    expect(call.headers.get('content-type')).toBe('application/json');
  });

  test('passes status, body, Retry-After and X-Request-Id through', async () => {
    const body = errorBody('rate_limit.exceeded', { retry_after_seconds: 30 });
    fetchMock.mockResolvedValue(
      new Response(JSON.stringify(body), {
        status: 429,
        headers: { 'content-type': 'application/json', 'retry-after': '30', 'x-request-id': 'req-9' },
      }),
    );
    const res = await handleBff(bffRequest('/api/v1/vehicles', { cookies: SESSION }), ['vehicles']);
    expect(res.status).toBe(429);
    expect(res.headers.get('retry-after')).toBe('30');
    expect(res.headers.get('x-request-id')).toBe('req-9');
    expect(res.headers.get('content-type')).toContain('application/json');
    expect(await res.json()).toEqual(body);
  });

  test('never forwards the client Cookie or Authorization to Go', async () => {
    fetchMock.mockImplementation(async () => upstreamOk());
    const spoof = { authorization: 'Bearer attacker', 'x-api-key': 'k' };
    await handleBff(bffRequest('/api/v1/legal/current', { cookies: SESSION, headers: spoof }), ['legal', 'current']);
    await handleBff(bffRequest('/api/v1/vehicles', { cookies: SESSION, headers: spoof }), ['vehicles']);
    for (const n of [0, 1]) {
      expect(upstreamCall(n).headers.has('cookie')).toBe(false);
      expect(upstreamCall(n).headers.has('x-api-key')).toBe(false);
    }
    expect(upstreamCall(0).headers.has('authorization')).toBe(false);
    expect(upstreamCall(1).headers.get('authorization')).toBe('Bearer access-1');
  });

  test('forwards X-Forwarded-For unchanged and omits it when absent', async () => {
    fetchMock.mockImplementation(async () => upstreamOk());
    await handleBff(
      bffRequest('/api/v1/vehicles', { cookies: SESSION, headers: { 'x-forwarded-for': '203.0.113.9, 10.0.0.2' } }),
      ['vehicles'],
    );
    expect(upstreamCall(0).headers.get('x-forwarded-for')).toBe('203.0.113.9, 10.0.0.2');
    await handleBff(bffRequest('/api/v1/vehicles', { cookies: SESSION }), ['vehicles']);
    expect(upstreamCall(1).headers.has('x-forwarded-for')).toBe(false);
  });

  test('returns 503 when refresh exists but access does not', async () => {
    const res = await handleBff(bffRequest('/api/v1/vehicles', { cookies: { mp_refresh: 'refresh-1' } }), ['vehicles']);
    expect(res.status).toBe(503);
    expect((await res.json()).error.code).toBe('internal.unexpected');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test('returns 401 when neither cookie exists', async () => {
    const res = await handleBff(bffRequest('/api/v1/vehicles'), ['vehicles']);
    expect(res.status).toBe(401);
    expect((await res.json()).error.code).toBe('auth.unauthenticated');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test('returns 502 when upstream is unreachable', async () => {
    fetchMock.mockRejectedValue(new TypeError('fetch failed'));
    const res = await handleBff(bffRequest('/api/v1/vehicles', { cookies: SESSION }), ['vehicles']);
    expect(res.status).toBe(502);
    expect((await res.json()).error.code).toBe('internal.unexpected');
  });

  test('uses a 20 s timeout on register and 5 s on reads', async () => {
    fetchMock.mockImplementation(async () => upstreamOk({ data: { status: 'verification_pending' }, meta: { request_id: 'r' } }));
    const timeout = vi.spyOn(AbortSignal, 'timeout');
    await handleBff(bffRequest('/api/v1/auth/register', { method: 'POST', body: {} }), ['auth', 'register']);
    expect(timeout).toHaveBeenLastCalledWith(20_000);
    expect(upstreamCall(0).init.signal).toBe(timeout.mock.results.at(-1)?.value);

    await handleBff(bffRequest('/api/v1/vehicles', { cookies: SESSION }), ['vehicles']);
    expect(timeout).toHaveBeenLastCalledWith(5_000);
    timeout.mockRestore();
  });

  test('never forwards upstream Set-Cookie', async () => {
    fetchMock.mockResolvedValue(upstreamOk(successBody(vehicleFixture), { headers: { 'set-cookie': 'evil=1; Path=/' } }));
    const res = await handleBff(bffRequest('/api/v1/vehicles', { cookies: SESSION }), ['vehicles']);
    expect(res.headers.getSetCookie()).toEqual([]);
  });

  test('answers 204 with no body', async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }));
    const res = await handleBff(
      bffRequest(`/api/v1/vehicles/${vehicleFixture.id}`, { method: 'DELETE', cookies: SESSION }),
      ['vehicles', vehicleFixture.id],
    );
    expect(res.status).toBe(204);
    expect(await res.text()).toBe('');
  });
});

describe('cookie clearing', () => {
  const logout = (cookies: Record<string, string> = SESSION) =>
    handleBff(bffRequest('/api/v1/auth/logout', { method: 'POST', cookies }), ['auth', 'logout']);

  test('logout clears cookies when upstream says 401 and answers 204', async () => {
    fetchMock.mockResolvedValue(Response.json(errorBody('auth.unauthenticated'), { status: 401 }));
    const res = await logout();
    expect(res.status).toBe(204);
    expect(cleared(res)).toBe(true);
  });

  test('logout clears cookies on success', async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }));
    const res = await logout();
    expect(res.status).toBe(204);
    expect(cleared(res)).toBe(true);
  });

  test('logout with no access cookie clears cookies and answers 204', async () => {
    const withRefresh = await logout({ mp_refresh: 'refresh-1' });
    expect(withRefresh.status).toBe(204);
    expect(cleared(withRefresh)).toBe(true);
    const anonymous = await handleBff(bffRequest('/api/v1/auth/logout-all', { method: 'POST' }), ['auth', 'logout-all']);
    expect(anonymous.status).toBe(204);
    expect(cleared(anonymous)).toBe(true);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test('logout clears cookies when upstream fails', async () => {
    fetchMock.mockResolvedValueOnce(Response.json(errorBody('internal.unexpected'), { status: 500 }));
    const failed = await logout();
    expect(failed.status).toBe(500);
    expect(cleared(failed)).toBe(true);

    fetchMock.mockRejectedValueOnce(new TypeError('fetch failed'));
    const unreachable = await logout();
    expect(unreachable.status).toBe(502);
    expect(cleared(unreachable)).toBe(true);
  });

  test('logout clears cookies for a suspended account and passes 403 through', async () => {
    fetchMock.mockResolvedValue(Response.json(errorBody('auth.account_suspended'), { status: 403 }));
    const res = await logout();
    expect(res.status).toBe(403);
    expect((await res.json()).error.code).toBe('auth.account_suspended');
    expect(cleared(res)).toBe(true);
  });

  test('DELETE /v1/me clears cookies on 204 only', async () => {
    const remove = () => handleBff(bffRequest('/api/v1/me', { method: 'DELETE', cookies: SESSION, body: { password: 'x' } }), ['me']);
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }));
    expect(cleared(await remove())).toBe(true);

    fetchMock.mockResolvedValueOnce(Response.json(errorBody('auth.password_incorrect'), { status: 403 }));
    const wrong = await remove();
    expect(wrong.status).toBe(403);
    expect(wrong.cookies.get('mp_access')).toBeUndefined();
    expect(wrong.headers.getSetCookie()).toEqual([]);
  });
});
