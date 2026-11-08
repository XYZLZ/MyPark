// @vitest-environment node
import { errorBody, successBody, tokenResponseFixture } from '@mypark/contracts/fixtures';
import { loginResultSchema } from '@mypark/contracts';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { CSRF, bffRequest } from '@/testing/bff-request';
import { handleLogin } from './login';

const fetchMock = vi.fn<typeof fetch>();
const credentials = { email: 'ana@example.do', password: 'correct horse battery' };

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal('fetch', fetchMock);
  vi.stubEnv('API_INTERNAL_URL', 'http://api.test');
  vi.stubEnv('WEB_ALLOWED_ORIGINS', 'http://localhost:3000');
  fetchMock.mockImplementation(async () => Response.json(successBody(tokenResponseFixture), { status: 200 }));
});

const login = (body: unknown = credentials, extra: Parameters<typeof bffRequest>[1] = {}) =>
  handleLogin(bffRequest('/api/auth/login', { method: 'POST', body, ...extra }));

describe('handleLogin', () => {
  test('login response has no token and sets cookies', async () => {
    const res = await login();
    expect(res.status).toBe(200);
    const text = await res.text();
    expect(text).not.toContain('access-1');
    expect(text).not.toContain('refresh-1');
    expect(res.cookies.get('mp_access')).toMatchObject({ value: 'access-1', httpOnly: true, maxAge: 840 });
    expect(res.cookies.get('mp_refresh')).toMatchObject({ value: 'refresh-1', httpOnly: true, maxAge: 2_592_000 });
  });

  test('returns only user and legal_acceptance_required', async () => {
    const res = await login();
    const body = await res.json();
    expect(loginResultSchema.parse(body.data)).toEqual({
      user: tokenResponseFixture.user,
      legal_acceptance_required: false,
    });
    expect(body.meta.request_id).toBeTruthy();
  });

  test('rotates the csrf cookie', async () => {
    const res = await login();
    const rotated = res.cookies.get('mp_csrf');
    expect(rotated?.value).toMatch(/^[A-Za-z0-9_-]{43}$/);
    expect(rotated?.value).not.toBe(CSRF);
    expect(rotated?.httpOnly).toBeFalsy();
  });

  test('forces client_kind web even if the browser sends mobile', async () => {
    await login({ ...credentials, client_kind: 'mobile' });
    const [url, init] = fetchMock.mock.calls[0]!;
    expect(String(url)).toBe('http://api.test/v1/auth/login');
    expect(JSON.parse(String(init?.body))).toEqual({ ...credentials, client_kind: 'web' });
  });

  test('forwards X-Forwarded-For unchanged and omits it when absent', async () => {
    await login(credentials, { headers: { 'x-forwarded-for': '203.0.113.9' } });
    expect(new Headers(fetchMock.mock.calls[0]![1]?.headers).get('x-forwarded-for')).toBe('203.0.113.9');
    await login();
    expect(new Headers(fetchMock.mock.calls[1]![1]?.headers).has('x-forwarded-for')).toBe(false);
  });

  test('rejects bad Origin and bad CSRF', async () => {
    expect((await login(credentials, { origin: 'http://evil.example' })).status).toBe(403);
    expect((await login(credentials, { origin: null })).status).toBe(403);
    const badToken = await login(credentials, { csrf: 'wrong' });
    expect(badToken.status).toBe(403);
    expect((await badToken.json()).error.code).toBe('auth.forbidden');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test('passes 401 auth.invalid_credentials through and sets no cookies', async () => {
    fetchMock.mockResolvedValue(
      new Response(JSON.stringify(errorBody('auth.invalid_credentials')), {
        status: 401,
        headers: { 'content-type': 'application/json', 'x-request-id': 'req-1' },
      }),
    );
    const res = await login();
    expect(res.status).toBe(401);
    expect((await res.json()).error.code).toBe('auth.invalid_credentials');
    expect(res.headers.get('x-request-id')).toBe('req-1');
    expect(res.headers.getSetCookie()).toEqual([]);
  });

  test('passes a 429 Retry-After through', async () => {
    fetchMock.mockResolvedValue(
      new Response(JSON.stringify(errorBody('rate_limit.exceeded')), { status: 429, headers: { 'retry-after': '42' } }),
    );
    const res = await login();
    expect(res.status).toBe(429);
    expect(res.headers.get('retry-after')).toBe('42');
  });

  test('a non-JSON body returns 400 http.invalid_json', async () => {
    const res = await handleLogin(bffRequest('/api/auth/login', { method: 'POST', rawBody: '{not json' }));
    expect(res.status).toBe(400);
    expect((await res.json()).error.code).toBe('http.invalid_json');
    const array = await login([1, 2]);
    expect(array.status).toBe(400);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test('a malformed upstream success returns 502 internal.unexpected', async () => {
    fetchMock.mockResolvedValue(Response.json({ data: { access_token: 'x' }, meta: { request_id: 'r' } }, { status: 200 }));
    const res = await login();
    expect(res.status).toBe(502);
    expect((await res.json()).error.code).toBe('internal.unexpected');
    expect(res.headers.getSetCookie()).toEqual([]);

    fetchMock.mockResolvedValue(new Response('<html>', { status: 200 }));
    expect((await login()).status).toBe(502);
  });

  test('returns 502 when upstream is unreachable', async () => {
    fetchMock.mockRejectedValue(new TypeError('fetch failed'));
    expect((await login()).status).toBe(502);
  });

  test('uses the 20 s argon2 timeout', async () => {
    const timeout = vi.spyOn(AbortSignal, 'timeout');
    await login();
    expect(timeout).toHaveBeenCalledWith(20_000);
    expect(fetchMock.mock.calls[0]![1]?.signal).toBe(timeout.mock.results[0]?.value);
    timeout.mockRestore();
  });
});
