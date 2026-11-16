// @vitest-environment node
import { adminFixture, errorBody, simulatorStatusRunningFixture, successBody, userFixture } from '@mypark/contracts/fixtures';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { bffRequest } from '@/testing/bff-request';
import { handleSimulator } from './handler';

const TOKEN = 'sim-token-secret-1';
const SESSION = { mp_access: 'access-1', mp_refresh: 'refresh-1' };
const fetchMock = vi.fn<typeof fetch>();
let me: () => Response;
let sim: () => Response;
const json = (body: unknown, status = 200, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json', ...headers } });

beforeEach(() => {
  fetchMock.mockReset();
  me = () => json(successBody(adminFixture));
  sim = () => json(successBody(simulatorStatusRunningFixture), 200, { 'x-request-id': 'req-9' });
  fetchMock.mockImplementation(async (input) => {
    const url = String(input);
    if (url === 'http://api.test/v1/me') return me();
    if (url.startsWith('http://sim.test/control/')) return sim();
    throw new Error(`unexpected fetch ${url}`);
  });
  vi.stubGlobal('fetch', fetchMock);
  vi.stubEnv('API_INTERNAL_URL', 'http://api.test');
  vi.stubEnv('WEB_ALLOWED_ORIGINS', 'http://localhost:3000');
  vi.stubEnv('SIMULATOR_INTERNAL_URL', 'http://sim.test/');
  vi.stubEnv('SIMULATOR_CONTROL_TOKEN', TOKEN);
});
afterEach(() => vi.restoreAllMocks());

const call = (path: string, options: Parameters<typeof bffRequest>[1] = {}) => {
  const [pathname, query] = path.split('?');
  return handleSimulator(
    bffRequest(`/api/admin/simulator/${path}`, { cookies: SESSION, ...options }),
    pathname!.split('/'),
  ).then(async (res) => ({ res, text: await res.text(), query }));
};
const simCalls = () => fetchMock.mock.calls.filter(([url]) => String(url).startsWith('http://sim.test'));
const code = (text: string) => JSON.parse(text).error.code;

describe('handleSimulator', () => {
  test('missing config is 404 and calls nothing', async () => {
    vi.stubEnv('SIMULATOR_CONTROL_TOKEN', '');
    const { res, text } = await call('status');
    expect([res.status, code(text)]).toEqual([404, 'http.not_found']);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test.each([
    ['GET', 'events?after=-1'],
    ['GET', 'events?after=1e3'],
    ['GET', 'events?after=1&after=2'],
    ['GET', 'events?after=99999999999999999999'],
    ['POST', 'cars/car-000001%2F..%2Fstatus/leave'],
    ['POST', 'cars/car-000001/../status/leave'],
    ['POST', 'cars/CAR-000001/leave'],
    ['POST', 'sensors/E1000/fault'],
    ['POST', 'status'],
    ['GET', 'control/status'],
  ])('rejects crafted ids and after values without calling upstream: %s %s', async (method, path) => {
    const { res, text } = await call(path, method === 'POST' ? { method, body: {} } : {});
    expect([res.status, code(text)]).toEqual([404, 'http.not_found']);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test('forwards only a valid after', async () => {
    await call('events?after=5&x=1');
    expect(String(simCalls()[0]![0])).toBe('http://sim.test/control/events?after=5');
  });

  test('no session is 401; a refresh cookie alone is 503', async () => {
    const none = await call('status', { cookies: {} });
    expect([none.res.status, code(none.text)]).toEqual([401, 'auth.unauthenticated']);
    const refreshOnly = await call('status', { cookies: { mp_refresh: 'refresh-1' } });
    expect([refreshOnly.res.status, code(refreshOnly.text)]).toEqual([503, 'internal.unexpected']);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test('checks the role with the bearer on every request', async () => {
    await call('status');
    await call('status');
    const meCalls = fetchMock.mock.calls.filter(([url]) => String(url) === 'http://api.test/v1/me');
    expect(meCalls).toHaveLength(2);
    expect(new Headers(meCalls[0]![1]?.headers).get('authorization')).toBe('Bearer access-1');
  });

  test.each([
    ['a driver gets 403 auth.forbidden', () => json(successBody(userFixture)), 403, 'auth.forbidden'],
    ['an expired bearer is 401', () => json(errorBody('auth.unauthenticated'), 401), 401, 'auth.unauthenticated'],
    ['a suspended admin is 403', () => json(errorBody('auth.account_suspended'), 403), 403, 'auth.forbidden'],
    ['a 5xx from /v1/me is 502', () => json(errorBody('internal.unexpected'), 500), 502, 'internal.unexpected'],
    ['a malformed /v1/me body is 502', () => json(successBody({ role: 'admin' })), 502, 'internal.unexpected'],
    ['an unreachable API is 502', () => { throw new TypeError('fetch failed'); }, 502, 'internal.unexpected'],
  ])('%s', async (_name, answer, status, errorCode) => {
    me = answer;
    const { res, text } = await call('status');
    expect([res.status, code(text)]).toEqual([status, errorCode]);
    expect(simCalls()).toHaveLength(0);
  });

  test('POST without CSRF is 403 and never reaches the simulator', async () => {
    for (const override of [{ csrf: null }, { csrf: 'wrong' }, { origin: null }, { origin: 'http://evil.example' }]) {
      const { res, text } = await call('scenarios/stop', { method: 'POST', body: {}, ...override });
      expect([res.status, code(text)]).toEqual([403, 'auth.forbidden']);
    }
    expect(simCalls()).toHaveLength(0);
  });

  test('an admin GET sets X-Simulator-Token upstream and never echoes it', async () => {
    const logs = [vi.spyOn(console, 'log'), vi.spyOn(console, 'warn'), vi.spyOn(console, 'error')];
    const { res, text } = await call('status', { headers: { 'x-forwarded-for': '203.0.113.9' } });
    expect(res.status).toBe(200);
    expect(JSON.parse(text).data.running).toBe(true);
    const [url, init] = simCalls()[0]!;
    expect(String(url)).toBe('http://sim.test/control/status');
    const headers = new Headers(init?.headers);
    expect(headers.get('x-simulator-token')).toBe(TOKEN);
    expect(headers.get('accept')).toBe('application/json');
    for (const name of ['cookie', 'authorization', 'x-forwarded-for', 'content-type']) expect(headers.has(name)).toBe(false);
    expect(init).toMatchObject({ method: 'GET', redirect: 'manual' });
    expect(init?.signal).toBeInstanceOf(AbortSignal);
    expect(res.headers.get('cache-control')).toBe('no-store');
    expect(res.headers.get('x-request-id')).toBe('req-9');
    expect([...res.headers.values()].join(' ')).not.toContain(TOKEN);
    expect(text).not.toContain(TOKEN);
    for (const spy of logs) expect(JSON.stringify(spy.mock.calls)).not.toContain(TOKEN);
  });

  test('a POST sends Content-Type application/json upstream', async () => {
    sim = () => json(successBody({ car_id: 'car-000001' }), 201);
    const { res } = await call('cars', { method: 'POST', body: { card: 'EMP-001' } });
    expect(res.status).toBe(201);
    const [, init] = simCalls()[0]!;
    const headers = new Headers(init?.headers);
    expect(headers.get('content-type')).toBe('application/json');
    expect(headers.get('x-simulator-token')).toBe(TOKEN);
    expect(headers.has('cookie')).toBe(false);
    expect(init?.method).toBe('POST');
  });

  test('a 204 passes through with no body', async () => {
    sim = () => new Response(null, { status: 204 });
    const { res, text } = await call('scenarios/stop', { method: 'POST', body: {} });
    expect([res.status, text]).toEqual([204, '']);
  });

  test('a body over 4 KB is 413 and a non-object body is 400', async () => {
    // 2 049 two-byte characters: under 4 096 characters, over 4 096 bytes.
    const big = await call('cars', { method: 'POST', rawBody: JSON.stringify({ card: 'é'.repeat(2_049) }) });
    expect([big.res.status, code(big.text)]).toEqual([413, 'http.payload_too_large']);
    for (const rawBody of ['', '[]', 'null', '42', '"x"', '{bad']) {
      const { res, text } = await call('cars', { method: 'POST', rawBody });
      expect([res.status, code(text)], rawBody).toEqual([400, 'http.invalid_json']);
    }
    expect(simCalls()).toHaveLength(0);
  });

  test('seed digits are forwarded byte for byte', async () => {
    const rawBody = '{"scenario":"weekday","speed":30,"seed":9007199254740993}';
    await call('scenarios/start', { method: 'POST', rawBody });
    expect(simCalls()[0]![1]?.body).toBe(rawBody);
  });

  test('upstream down is 502 internal.unexpected', async () => {
    const logs = [vi.spyOn(console, 'log'), vi.spyOn(console, 'warn'), vi.spyOn(console, 'error')];
    sim = () => { throw new TypeError('fetch failed'); };
    const { res, text } = await call('status');
    expect([res.status, code(text)]).toEqual([502, 'internal.unexpected']);
    for (const spy of logs) expect(JSON.stringify(spy.mock.calls)).not.toContain(TOKEN);
  });

  test.each([
    [401, 'simulator.unauthorized', 'status'],
    [409, 'simulator.not_running', 'cars'],
    [422, 'validation.failed', 'scenarios/start'],
    [404, 'simulator.car_not_found', 'cars/car-000123/leave'],
  ])('upstream %i %s passes through', async (status, errorCode, path) => {
    sim = () => json(errorBody(errorCode), status, { 'retry-after': '3' });
    const { res, text } = await call(path, path === 'status' ? {} : { method: 'POST', body: {} });
    expect([res.status, code(text)]).toEqual([status, errorCode]);
    expect(res.headers.get('retry-after')).toBe('3');
  });
});
