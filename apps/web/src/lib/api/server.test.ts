// @vitest-environment node
import { ApiError, parseEnvelope, userSchema } from '@mypark/contracts';
import { legalCurrentFixture, successBody, userFixture } from '@mypark/contracts/fixtures';
import { beforeEach, describe, expect, test, vi } from 'vitest';

const state = { cookies: {} as Record<string, string>, headers: {} as Record<string, string> };

vi.mock('next/headers', () => ({
  cookies: async () => ({
    get: (name: string) => (name in state.cookies ? { name, value: state.cookies[name] } : undefined),
  }),
  headers: async () => new Headers(state.headers),
}));

const fetchMock = vi.fn<typeof fetch>();

beforeEach(() => {
  state.cookies = { mp_access: 'access-1' };
  state.headers = {};
  fetchMock.mockReset();
  vi.stubGlobal('fetch', fetchMock);
  vi.stubEnv('API_INTERNAL_URL', 'http://api.test');
});

const callHeaders = (n = 0) => new Headers(fetchMock.mock.calls[n]![1]?.headers);

describe('serverFetch', () => {
  test('getMe sends the bearer from cookies, no-store and a 5 s timeout', async () => {
    fetchMock.mockResolvedValue(Response.json(successBody(userFixture)));
    const timeout = vi.spyOn(AbortSignal, 'timeout');
    const { getMe } = await import('./server');
    expect(await getMe()).toEqual(userFixture);

    const [url, init] = fetchMock.mock.calls[0]!;
    expect(String(url)).toBe('http://api.test/v1/me');
    expect(callHeaders().get('authorization')).toBe('Bearer access-1');
    expect(init?.cache).toBe('no-store');
    expect(timeout).toHaveBeenCalledWith(5_000);
    expect(init?.signal).toBe(timeout.mock.results[0]?.value);
    timeout.mockRestore();
  });

  test('public reads send no bearer', async () => {
    fetchMock.mockResolvedValue(Response.json(successBody(legalCurrentFixture)));
    const { getLegalCurrent } = await import('./server');
    expect(await getLegalCurrent()).toEqual(legalCurrentFixture);
    expect(callHeaders().has('authorization')).toBe(false);
  });

  test('forwards the incoming X-Forwarded-For and omits it when absent', async () => {
    fetchMock.mockImplementation(async () => Response.json(successBody(userFixture)));
    const { serverFetch } = await import('./server');
    state.headers = { 'x-forwarded-for': '203.0.113.9, 10.0.0.2' };
    await serverFetch('/v1/me', parseEnvelope(userSchema));
    expect(callHeaders(0).get('x-forwarded-for')).toBe('203.0.113.9, 10.0.0.2');
    state.headers = {};
    await serverFetch('/v1/me', parseEnvelope(userSchema));
    expect(callHeaders(1).has('x-forwarded-for')).toBe(false);
  });

  test('sends a JSON body with the method given', async () => {
    fetchMock.mockResolvedValue(Response.json(successBody(userFixture)));
    const { serverFetch } = await import('./server');
    await serverFetch('/v1/me', parseEnvelope(userSchema), { method: 'PATCH', body: { locale: 'en' }, timeoutMs: 20_000 });
    const init = fetchMock.mock.calls[0]![1]!;
    expect(init.method).toBe('PATCH');
    expect(JSON.parse(String(init.body))).toEqual({ locale: 'en' });
    expect(callHeaders().get('content-type')).toBe('application/json');
  });

  test('throws the ApiError of an error envelope with its status', async () => {
    fetchMock.mockResolvedValue(
      Response.json({ error: { code: 'auth.account_suspended', message: 'm', details: {} }, meta: { request_id: 'r' } }, { status: 403 }),
    );
    const { getMe } = await import('./server');
    await expect(getMe()).rejects.toMatchObject({ code: 'auth.account_suspended', status: 403 });
  });

  test('throws internal.unexpected when upstream is unreachable or not JSON', async () => {
    const { serverFetch } = await import('./server');
    fetchMock.mockRejectedValueOnce(new TypeError('fetch failed'));
    await expect(serverFetch('/v1/me', parseEnvelope(userSchema))).rejects.toMatchObject({ code: 'internal.unexpected', status: 502 });
    fetchMock.mockResolvedValueOnce(new Response('<html>', { status: 200 }));
    await expect(serverFetch('/v1/me', parseEnvelope(userSchema))).rejects.toBeInstanceOf(ApiError);
  });

  test('without an access cookie it throws before calling upstream', async () => {
    const { serverFetch } = await import('./server');
    state.cookies = { mp_refresh: 'refresh-1' };
    await expect(serverFetch('/v1/me', parseEnvelope(userSchema))).rejects.toMatchObject({ code: 'internal.unexpected', status: 503 });
    state.cookies = {};
    await expect(serverFetch('/v1/me', parseEnvelope(userSchema))).rejects.toMatchObject({ code: 'auth.unauthenticated', status: 401 });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test('an ended session (cookies cleared, value undefined) reports 401, not 503', async () => {
    const { serverFetch } = await import('./server');
    // Next merges the proxy's cleared cookies into cookies(): the entry stays, its value is gone.
    state.cookies = { mp_access: undefined as unknown as string, mp_refresh: undefined as unknown as string };
    await expect(serverFetch('/v1/me', parseEnvelope(userSchema))).rejects.toMatchObject({ code: 'auth.unauthenticated', status: 401 });
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
