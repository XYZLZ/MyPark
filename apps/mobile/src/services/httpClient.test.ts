import { ApiError, parseEnvelope } from '@mypark/contracts';
import { z } from 'zod';

import {
  ARGON2_TIMEOUT_MS,
  DEFAULT_TIMEOUT_MS,
  REFRESH_TIMEOUT_MS,
  createHttpClient,
  unwrap,
  type AuthAdapter,
  type RefreshResult,
} from './httpClient';

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function hangingFetch() {
  return jest.fn(
    (_url: string, init?: RequestInit) =>
      new Promise<Response>((_resolve, reject) => {
        init?.signal?.addEventListener('abort', () => reject(new Error('Aborted')));
      }),
  );
}

test('the production default timeout is 3000 ms', () => {
  expect(DEFAULT_TIMEOUT_MS).toBe(3000);
});

test('an abort during the body read rejects with client.timeout instead of resolving an empty body', async () => {
  // Headers arrive, then the body stalls until the timeout aborts the request.
  const stalledBody = jest.fn(async (_url: string, init?: RequestInit) => {
    const res = new Response(null, { status: 200 });
    Object.defineProperty(res, 'json', {
      value: () =>
        new Promise((_resolve, reject) => {
          init?.signal?.addEventListener('abort', () => reject(new Error('Aborted')));
        }),
    });
    return res;
  });
  const client = createHttpClient('http://api.test', stalledBody, { timeoutMs: 10 });
  await expect(client.request({ method: 'GET', path: '/x' })).rejects.toMatchObject({ code: 'client.timeout' });
});

test('a non-JSON body resolves with an undefined body instead of rejecting', async () => {
  const fetchImpl = jest.fn().mockResolvedValue(new Response('<html>Bad gateway</html>', { status: 502 }));
  const client = createHttpClient('http://api.test', fetchImpl);
  await expect(client.request({ method: 'GET', path: '/x' })).resolves.toMatchObject({ status: 502, body: undefined });
});

// ---------------------------------------------------------------------------
// request(): authenticated JSON client (Task 8)
// ---------------------------------------------------------------------------

function makeAdapter(overrides: Partial<AuthAdapter> = {}): jest.Mocked<AuthAdapter> {
  return {
    getAccessToken: jest.fn(() => 'access-1'),
    refresh: jest.fn(async () => ({ kind: 'refreshed' }) as RefreshResult),
    onSessionRevoked: jest.fn(async () => undefined),
    onLegalRequired: jest.fn(),
    ...overrides,
  } as jest.Mocked<AuthAdapter>;
}

function errorBody(code: string, details: Record<string, unknown> = {}) {
  return { error: { code, message: 'server text', details }, meta: { request_id: 'r1' } };
}

function headerOf(call: unknown[], name: string): string | undefined {
  const init = call[1] as RequestInit;
  return (init.headers as Record<string, string>)[name];
}

test('request sends method, JSON body and Accept', async () => {
  const fetchImpl = jest.fn().mockResolvedValue(jsonResponse(200, { data: 1 }));
  const client = createHttpClient('http://api.test/', fetchImpl);
  const res = await client.request({ method: 'POST', path: '/v1/vehicles', body: { plate: 'A1' } });
  expect(res).toEqual({ status: 200, body: { data: 1 }, retryAfter: null });
  const [url, init] = fetchImpl.mock.calls[0] as [string, RequestInit];
  expect(url).toBe('http://api.test/v1/vehicles');
  expect(init.method).toBe('POST');
  expect(init.body).toBe(JSON.stringify({ plate: 'A1' }));
  expect(headerOf(fetchImpl.mock.calls[0], 'Accept')).toBe('application/json');
  expect(headerOf(fetchImpl.mock.calls[0], 'Content-Type')).toBe('application/json');
});

test('auth true sends the bearer from the adapter; auth false sends none', async () => {
  const fetchImpl = jest.fn().mockImplementation(async () => jsonResponse(200, { data: 1 }));
  const client = createHttpClient('http://api.test', fetchImpl);
  client.setAuthAdapter(makeAdapter());
  await client.request({ method: 'GET', path: '/v1/me', auth: true });
  await client.request({ method: 'POST', path: '/v1/auth/login', body: {}, auth: false });
  expect(headerOf(fetchImpl.mock.calls[0], 'Authorization')).toBe('Bearer access-1');
  expect(headerOf(fetchImpl.mock.calls[1], 'Authorization')).toBeUndefined();
});

test('abort maps to client.timeout', async () => {
  const client = createHttpClient('http://api.test', hangingFetch());
  await expect(client.request({ method: 'GET', path: '/x', timeoutMs: 10 })).rejects.toMatchObject({
    name: 'ApiError',
    code: 'client.timeout',
  });
});

test('a rejected fetch maps to client.network', async () => {
  const client = createHttpClient('http://api.test', jest.fn().mockRejectedValue(new TypeError('Network request failed')));
  await expect(client.request({ method: 'GET', path: '/x' })).rejects.toMatchObject({ code: 'client.network' });
});

test('a 204 resolves with an undefined body', async () => {
  const client = createHttpClient('http://api.test', jest.fn().mockResolvedValue(new Response(null, { status: 204 })));
  await expect(client.request({ method: 'DELETE', path: '/x' })).resolves.toEqual({
    status: 204,
    body: undefined,
    retryAfter: null,
  });
});

test('exposes Retry-After', async () => {
  const res = new Response(JSON.stringify(errorBody('rate_limit.exceeded')), { status: 429, headers: { 'Retry-After': '30' } });
  const client = createHttpClient('http://api.test', jest.fn().mockResolvedValue(res));
  expect((await client.request({ method: 'POST', path: '/x' })).retryAfter).toBe('30');
});

test('unwrap copies Retry-After into the ApiError details', () => {
  const res = { status: 429, body: errorBody('rate_limit.exceeded', { retry_after_seconds: 5 }), retryAfter: '30' };
  let thrown: unknown;
  try {
    unwrap(res, parseEnvelope(z.object({})));
  } catch (error) {
    thrown = error;
  }
  expect(thrown).toBeInstanceOf(ApiError);
  expect((thrown as ApiError).code).toBe('rate_limit.exceeded');
  expect((thrown as ApiError).details).toEqual({ retry_after_seconds: 5, retry_after_header: '30' });
});

test('unwrap returns the parsed data and leaves details alone without a header', () => {
  const ok = { status: 200, body: { data: { a: 1 }, meta: { request_id: 'r1' } }, retryAfter: null };
  expect(unwrap(ok, parseEnvelope(z.object({ a: z.number() })))).toEqual({ a: 1 });
  const bad = { status: 429, body: errorBody('rate_limit.exceeded'), retryAfter: null };
  expect(() => unwrap(bad, parseEnvelope(z.object({})))).toThrow(expect.objectContaining({ details: {} }));
});

describe('timeouts abort at the configured value', () => {
  // Proves behavior: each call class aborts exactly at its limit, not before.
  beforeEach(() => jest.useFakeTimers());
  afterEach(() => jest.useRealTimers());

  async function abortsAt(timeoutMs: number | undefined, expectedMs: number) {
    let signal: AbortSignal | undefined;
    const fetchImpl = jest.fn(
      (_url: string, init?: RequestInit) =>
        new Promise<Response>((_resolve, reject) => {
          signal = init?.signal ?? undefined;
          signal?.addEventListener('abort', () => reject(new Error('Aborted')));
        }),
    );
    const client = createHttpClient('http://api.test', fetchImpl);
    const settled = client.request({ method: 'POST', path: '/x', timeoutMs }).catch((e: unknown) => e);
    await jest.advanceTimersByTimeAsync(expectedMs - 1);
    expect(signal?.aborted).toBe(false);
    await jest.advanceTimersByTimeAsync(1);
    expect(signal?.aborted).toBe(true);
    expect(await settled).toMatchObject({ code: 'client.timeout' });
  }

  test('timeout constants match the spec', async () => {
    expect([DEFAULT_TIMEOUT_MS, ARGON2_TIMEOUT_MS, REFRESH_TIMEOUT_MS]).toEqual([3000, 20_000, 10_000]);
    await abortsAt(undefined, 3000);
    await abortsAt(ARGON2_TIMEOUT_MS, 20_000);
    await abortsAt(REFRESH_TIMEOUT_MS, 10_000);
  });

  test('a per-request timeout overrides the default', async () => {
    await abortsAt(500, 500);
  });
});

describe('401 handling', () => {
  const unauthenticated = () => jsonResponse(401, errorBody('auth.unauthenticated'));

  test('unauthenticated refreshes and retries once', async () => {
    let token = 'old';
    const adapter = makeAdapter({
      getAccessToken: jest.fn(() => token),
      refresh: jest.fn(async () => {
        token = 'new';
        return { kind: 'refreshed' } as RefreshResult;
      }),
    });
    const fetchImpl = jest
      .fn()
      .mockResolvedValueOnce(unauthenticated())
      .mockResolvedValueOnce(jsonResponse(200, { data: 'ok' }));
    const client = createHttpClient('http://api.test', fetchImpl);
    client.setAuthAdapter(adapter);
    const res = await client.request({ method: 'POST', path: '/v1/vehicles', body: { a: 1 }, auth: true });
    expect(res.status).toBe(200);
    expect(fetchImpl).toHaveBeenCalledTimes(2);
    expect(fetchImpl.mock.calls.map((c) => c[0])).toEqual(['http://api.test/v1/vehicles', 'http://api.test/v1/vehicles']);
    expect(headerOf(fetchImpl.mock.calls[0], 'Authorization')).toBe('Bearer old');
    expect(headerOf(fetchImpl.mock.calls[1], 'Authorization')).toBe('Bearer new');
    expect((fetchImpl.mock.calls[1][1] as RequestInit).body).toBe(JSON.stringify({ a: 1 }));
  });

  test('a second 401 after the retry is returned, not retried', async () => {
    const adapter = makeAdapter();
    const fetchImpl = jest.fn().mockImplementation(async () => unauthenticated());
    const client = createHttpClient('http://api.test', fetchImpl);
    client.setAuthAdapter(adapter);
    const res = await client.request({ method: 'GET', path: '/v1/me', auth: true });
    expect(res.status).toBe(401);
    expect(adapter.refresh).toHaveBeenCalledTimes(1);
    expect(fetchImpl).toHaveBeenCalledTimes(2);
  });

  test('an ended session returns the 401 without retrying', async () => {
    const adapter = makeAdapter({ refresh: jest.fn(async () => ({ kind: 'ended' }) as RefreshResult) });
    const fetchImpl = jest.fn().mockImplementation(async () => unauthenticated());
    const client = createHttpClient('http://api.test', fetchImpl);
    client.setAuthAdapter(adapter);
    expect((await client.request({ method: 'GET', path: '/v1/me', auth: true })).status).toBe(401);
    expect(fetchImpl).toHaveBeenCalledTimes(1);
  });

  test('transient refresh failure fails the request with the client code', async () => {
    const adapter = makeAdapter({
      refresh: jest.fn(async () => ({ kind: 'unavailable', code: 'client.network' }) as RefreshResult),
    });
    const fetchImpl = jest.fn().mockImplementation(async () => unauthenticated());
    const client = createHttpClient('http://api.test', fetchImpl);
    client.setAuthAdapter(adapter);
    await expect(client.request({ method: 'GET', path: '/v1/me', auth: true })).rejects.toMatchObject({
      name: 'ApiError',
      code: 'client.network',
    });
    expect(fetchImpl).toHaveBeenCalledTimes(1);
  });

  test('rate-limited refresh fails with rate_limit.exceeded', async () => {
    const adapter = makeAdapter({
      refresh: jest.fn(async () => ({ kind: 'rate_limited', retryAfterSeconds: 42 }) as RefreshResult),
    });
    const client = createHttpClient('http://api.test', jest.fn().mockImplementation(async () => unauthenticated()));
    client.setAuthAdapter(adapter);
    await expect(client.request({ method: 'GET', path: '/v1/me', auth: true })).rejects.toMatchObject({
      code: 'rate_limit.exceeded',
      details: { retry_after_seconds: 42 },
    });
  });

  test('session_revoked signs out without a refresh and returns the 401', async () => {
    const adapter = makeAdapter();
    const client = createHttpClient(
      'http://api.test',
      jest.fn().mockImplementation(async () => jsonResponse(401, errorBody('auth.session_revoked'))),
    );
    client.setAuthAdapter(adapter);
    const res = await client.request({ method: 'GET', path: '/v1/me', auth: true });
    expect(res.status).toBe(401);
    expect(adapter.onSessionRevoked).toHaveBeenCalledTimes(1);
    expect(adapter.refresh).not.toHaveBeenCalled();
  });

  test('an unauthenticated 401 on an auth false request never refreshes', async () => {
    const adapter = makeAdapter();
    const client = createHttpClient('http://api.test', jest.fn().mockImplementation(async () => unauthenticated()));
    client.setAuthAdapter(adapter);
    expect((await client.request({ method: 'POST', path: '/v1/auth/login', auth: false })).status).toBe(401);
    expect(adapter.refresh).not.toHaveBeenCalled();
  });

  test('legal.acceptance_required notifies the adapter', async () => {
    const adapter = makeAdapter();
    const client = createHttpClient(
      'http://api.test',
      jest.fn().mockImplementation(async () => jsonResponse(403, errorBody('legal.acceptance_required'))),
    );
    client.setAuthAdapter(adapter);
    const res = await client.request({ method: 'GET', path: '/v1/vehicles', auth: true });
    expect(res.status).toBe(403);
    expect(adapter.onLegalRequired).toHaveBeenCalledTimes(1);
  });
});

const flushMicrotasks = async () => {
  for (let i = 0; i < 5; i++) await Promise.resolve();
};

describe('late 401s after a refresh already ran', () => {
  const unauthenticated = () => jsonResponse(401, errorBody('auth.unauthenticated'));

  test('a 401 that resolves after the refresh completed does not refresh again and retries with the new bearer', async () => {
    let token = 'old';
    const adapter = makeAdapter({
      getAccessToken: jest.fn(() => token),
      refresh: jest.fn(async () => {
        token = 'new';
        return { kind: 'refreshed' } as RefreshResult;
      }),
    });
    let release: (r: Response) => void = () => undefined;
    const fetchImpl = jest
      .fn()
      // Request B leaves with the old token and its 401 arrives late.
      .mockImplementationOnce(() => new Promise<Response>((resolve) => (release = resolve)))
      .mockResolvedValueOnce(jsonResponse(200, { data: 'ok' }));
    const client = createHttpClient('http://api.test', fetchImpl);
    client.setAuthAdapter(adapter);
    const pending = client.request({ method: 'GET', path: '/v1/vehicles', auth: true });
    await flushMicrotasks(); // B is on the wire with the old token
    await adapter.refresh(); // request A's refresh settles first
    release(unauthenticated());
    expect((await pending).status).toBe(200);
    expect(adapter.refresh).toHaveBeenCalledTimes(1);
    expect(headerOf(fetchImpl.mock.calls[0], 'Authorization')).toBe('Bearer old');
    expect(headerOf(fetchImpl.mock.calls[1], 'Authorization')).toBe('Bearer new');
  });

  test('a late 401 after the session was cleared returns the 401 without refreshing', async () => {
    let token: string | null = 'old';
    const adapter = makeAdapter({ getAccessToken: jest.fn(() => token) });
    let release: (r: Response) => void = () => undefined;
    const fetchImpl = jest.fn().mockImplementationOnce(() => new Promise<Response>((resolve) => (release = resolve)));
    const client = createHttpClient('http://api.test', fetchImpl);
    client.setAuthAdapter(adapter);
    const pending = client.request({ method: 'GET', path: '/v1/me', auth: true });
    await flushMicrotasks();
    token = null;
    release(unauthenticated());
    expect((await pending).status).toBe(401);
    expect(adapter.refresh).not.toHaveBeenCalled();
    expect(fetchImpl).toHaveBeenCalledTimes(1);
  });

  test('a request started during a refresh waits for it and sends the new bearer', async () => {
    let token = 'old';
    let finish: () => void = () => undefined;
    const inFlight = new Promise<void>((resolve) => {
      finish = () => {
        token = 'new';
        resolve();
      };
    });
    const adapter = makeAdapter({ getAccessToken: jest.fn(() => token), pendingRefresh: jest.fn(() => inFlight) });
    const fetchImpl = jest.fn().mockResolvedValue(jsonResponse(200, { data: 'ok' }));
    const client = createHttpClient('http://api.test', fetchImpl);
    client.setAuthAdapter(adapter);
    const pending = client.request({ method: 'GET', path: '/v1/me', auth: true });
    await Promise.resolve();
    expect(fetchImpl).not.toHaveBeenCalled();
    finish();
    await pending;
    expect(fetchImpl).toHaveBeenCalledTimes(1);
    expect(headerOf(fetchImpl.mock.calls[0], 'Authorization')).toBe('Bearer new');
    expect(adapter.refresh).not.toHaveBeenCalled();
  });
});
