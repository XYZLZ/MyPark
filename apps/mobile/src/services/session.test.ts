import { QueryClient } from '@tanstack/react-query';
import { ApiError } from '@mypark/contracts';
import { errorBody, successBody, tokenResponseFixture, userFixture } from '@mypark/contracts/fixtures';

import { useAuthFlowStore } from '@/store/auth-flow';
import { useAuthStore } from '@/store/auth';
import { useSettingsStore } from '@/store/settings';

import { REFRESH_TIMEOUT_MS, createHttpClient, type HttpClient } from './httpClient';
import { createAuthAdapter, createSessionService, type SessionService } from './session';
import type { TokenStorage } from './tokenStorage';

jest.mock('expo-secure-store', () => ({
  getItemAsync: jest.fn(),
  setItemAsync: jest.fn().mockResolvedValue(undefined),
  deleteItemAsync: jest.fn().mockResolvedValue(undefined),
}));

const BASE = 'http://api.test';
const REFRESH = 'POST /v1/auth/refresh';
const LOGOUT = 'POST /v1/auth/logout';
const VEHICLES = 'GET /v1/vehicles';

function json(status: number, body: unknown, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json', ...headers } });
}

const tokens = (n: number, overrides: Record<string, unknown> = {}) => ({
  ...tokenResponseFixture,
  access_token: `access-${n}`,
  refresh_token: `refresh-${n}`,
  ...overrides,
});

type Handler = (init: RequestInit) => Response | Promise<Response>;

/** A fake API keyed by "METHOD /path". Unrouted calls fail the test loudly. */
function createApi() {
  const routes = new Map<string, Handler>();
  const calls: { key: string; init: RequestInit }[] = [];
  const fetchImpl = jest.fn(async (input: string, init: RequestInit = {}) => {
    const key = `${init.method ?? 'GET'} ${input.replace(BASE, '')}`;
    calls.push({ key, init });
    const handler = routes.get(key);
    if (!handler) throw new Error(`unrouted ${key}`);
    return handler(init);
  });
  return {
    fetchImpl,
    calls,
    on: (key: string, handler: Handler) => routes.set(key, handler),
    count: (key: string) => calls.filter((c) => c.key === key).length,
    keys: () => calls.map((c) => c.key),
  };
}

const bearer = (init: RequestInit) => (init.headers as Record<string, string>).Authorization;

/** 401 for the signed-in token `access-0`, 200 for anything newer. */
const vehiclesByToken: Handler = (init) =>
  bearer(init) === 'Bearer access-0'
    ? json(401, errorBody('auth.unauthenticated'))
    : json(200, successBody([]));

type Env = ReturnType<typeof setup>;

function setup(options: { stored?: string | null; signedIn?: boolean; fetchImpl?: ReturnType<typeof createApi>['fetchImpl'] } = {}) {
  const api = createApi();
  const storage = {
    get: jest.fn<Promise<string | null>, []>().mockResolvedValue(options.stored === undefined ? 'refresh-0' : options.stored),
    set: jest.fn<Promise<void>, [string]>().mockResolvedValue(undefined),
    remove: jest.fn<Promise<void>, []>().mockResolvedValue(undefined),
  } satisfies TokenStorage;
  const http: HttpClient = createHttpClient(BASE, options.fetchImpl ?? api.fetchImpl);
  const queryClient = new QueryClient();
  const clear = jest.spyOn(queryClient, 'clear');
  const onUserLocale = jest.fn();
  const clock = { t: 1_000_000 };
  const session: SessionService = createSessionService({
    http,
    storage,
    store: useAuthStore,
    queryClient,
    onUserLocale,
    now: () => clock.t,
  });
  http.setAuthAdapter(createAuthAdapter(session, useAuthStore));
  if (options.signedIn !== false) {
    useAuthStore.getState().setSession({ accessToken: 'access-0', user: userFixture, legalPending: false });
  }
  return { api, storage, http, queryClient, clear, onUserLocale, clock, session };
}

/** Polls a condition by yielding to the event loop; real response parsing needs more than microtasks. */
async function until(condition: () => boolean) {
  for (let i = 0; i < 200 && !condition(); i++) await new Promise<void>((resolve) => setImmediate(() => resolve()));
  if (!condition()) throw new Error('condition never became true');
}

const getVehicles = (env: Env) => env.http.request({ method: 'GET', path: '/v1/vehicles', auth: true });

beforeEach(() => {
  useAuthStore.setState(useAuthStore.getInitialState(), true);
  useAuthFlowStore.setState(useAuthFlowStore.getInitialState(), true);
  useSettingsStore.setState(useSettingsStore.getInitialState(), true);
});

afterEach(() => jest.useRealTimers());

describe('refresh on 401', () => {
  test('single-flight on concurrent 401s', async () => {
    const env = setup();
    env.api.on(VEHICLES, vehiclesByToken);
    env.api.on(REFRESH, () => json(200, successBody(tokens(1))));
    const results = await Promise.all(Array.from({ length: 5 }, () => getVehicles(env)));
    expect(results.map((r) => r.status)).toEqual([200, 200, 200, 200, 200]);
    expect(env.api.count(REFRESH)).toBe(1);
    expect(env.api.count(VEHICLES)).toBe(10);
    expect(env.storage.set).toHaveBeenCalledTimes(1);
  });

  test('no refresh on 403 password_incorrect', async () => {
    const env = setup();
    env.api.on('DELETE /v1/me', () => json(403, errorBody('auth.password_incorrect')));
    const res = await env.http.request({ method: 'DELETE', path: '/v1/me', auth: true });
    expect(res.status).toBe(403);
    expect(env.api.count(REFRESH)).toBe(0);
    expect(useAuthStore.getState().status).toBe('signedIn');
  });

  test('no refresh on session_revoked', async () => {
    const env = setup();
    env.api.on('GET /v1/me', () => json(401, errorBody('auth.session_revoked')));
    await env.http.request({ method: 'GET', path: '/v1/me', auth: true });
    expect(env.api.count(REFRESH)).toBe(0);
    expect(useAuthStore.getState()).toMatchObject({ status: 'signedOut', accessToken: null, notice: 'session_ended' });
  });

  test('wrong password on login never refreshes', async () => {
    const env = setup({ signedIn: false });
    env.api.on('POST /v1/auth/login', () => json(401, errorBody('auth.invalid_credentials')));
    const res = await env.http.request({ method: 'POST', path: '/v1/auth/login', body: {}, auth: false });
    expect(res.status).toBe(401);
    expect(env.api.count(REFRESH)).toBe(0);
    expect(env.storage.remove).not.toHaveBeenCalled();
  });

  test('refresh_invalid clears storage, memory and queries and pre-fills the email', async () => {
    const env = setup();
    env.api.on('GET /v1/me', () => json(401, errorBody('auth.unauthenticated')));
    env.api.on(REFRESH, () => json(401, errorBody('auth.refresh_invalid')));
    const res = await env.http.request({ method: 'GET', path: '/v1/me', auth: true });
    expect(res.status).toBe(401);
    expect(env.storage.remove).toHaveBeenCalled();
    expect(env.clear).toHaveBeenCalled();
    expect(useAuthStore.getState()).toMatchObject({ status: 'signedOut', accessToken: null, user: null, notice: 'session_ended' });
    expect(useAuthFlowStore.getState().email).toBe(userFixture.email);
  });

  test('network and 5xx keep tokens and the next request refreshes again', async () => {
    const env = setup();
    env.api.on(VEHICLES, vehiclesByToken);
    env.api.on(REFRESH, () => {
      throw new TypeError('Network request failed');
    });
    await expect(getVehicles(env)).rejects.toMatchObject({ code: 'client.network' });
    env.api.on(REFRESH, () => json(503, errorBody('internal.unexpected')));
    await expect(getVehicles(env)).rejects.toMatchObject({ code: 'client.network' });
    expect(env.storage.remove).not.toHaveBeenCalled();
    expect(env.storage.set).not.toHaveBeenCalled();
    expect(useAuthStore.getState()).toMatchObject({ status: 'signedIn', accessToken: 'access-0' });
    env.api.on(REFRESH, () => json(200, successBody(tokens(1))));
    expect((await getVehicles(env)).status).toBe(200);
    expect(env.api.count(REFRESH)).toBe(3);
  });

  test('a refresh timeout keeps tokens', async () => {
    jest.useFakeTimers();
    const env = setup();
    env.api.on(REFRESH, (init) => new Promise<Response>((_resolve, reject) => init.signal?.addEventListener('abort', () => reject(new Error('Aborted')))));
    const outcome = env.session.refresh();
    await jest.advanceTimersByTimeAsync(REFRESH_TIMEOUT_MS);
    await expect(outcome).resolves.toEqual({ kind: 'unavailable', code: 'client.timeout' });
    expect(env.storage.remove).not.toHaveBeenCalled();
  });

  test('429 blocks refresh until Retry-After passes', async () => {
    const env = setup();
    env.api.on(VEHICLES, vehiclesByToken);
    env.api.on(REFRESH, () => json(429, errorBody('rate_limit.exceeded'), { 'Retry-After': '30' }));
    await expect(getVehicles(env)).rejects.toMatchObject({ code: 'rate_limit.exceeded', details: { retry_after_seconds: 30 } });
    expect(env.api.count(REFRESH)).toBe(1);

    env.clock.t += 29_000;
    await expect(getVehicles(env)).rejects.toMatchObject({ code: 'rate_limit.exceeded' });
    expect(env.api.count(REFRESH)).toBe(1);

    env.clock.t += 2_000;
    env.api.on(REFRESH, () => json(200, successBody(tokens(1))));
    expect((await getVehicles(env)).status).toBe(200);
    expect(env.api.count(REFRESH)).toBe(2);
  });
});

describe('refresh()', () => {
  test('writes the new token before resolving', async () => {
    const env = setup();
    let release: () => void = () => undefined;
    env.storage.set.mockReturnValue(new Promise<void>((resolve) => (release = resolve)));
    env.api.on(REFRESH, () => json(200, successBody(tokens(1))));
    let settled = false;
    const outcome = env.session.refresh().then((r) => {
      settled = true;
      return r;
    });
    await until(() => env.storage.set.mock.calls.length > 0);
    expect(env.storage.set).toHaveBeenCalledWith('refresh-1');
    expect(settled).toBe(false);
    expect(useAuthStore.getState().accessToken).toBe('access-0');
    release();
    await expect(outcome).resolves.toEqual({ kind: 'refreshed' });
    expect(useAuthStore.getState().accessToken).toBe('access-1');
  });

  test('uses the 10 s timeout, auth false and the stored token', async () => {
    const env = setup();
    env.api.on(REFRESH, () => json(200, successBody(tokens(1))));
    const spy = jest.spyOn(env.http, 'request');
    await env.session.refresh();
    expect(spy).toHaveBeenCalledWith({
      method: 'POST',
      path: '/v1/auth/refresh',
      body: { refresh_token: 'refresh-0' },
      auth: false,
      timeoutMs: REFRESH_TIMEOUT_MS,
    });
    expect(REFRESH_TIMEOUT_MS).toBe(10_000);
  });

  test('exposes the in-flight refresh to waiting requests and nothing once idle', async () => {
    const env = setup();
    let answer: (r: Response) => void = () => undefined;
    env.api.on(REFRESH, () => new Promise<Response>((resolve) => (answer = resolve)));
    expect(env.session.pendingRefresh()).toBeNull();
    const first = env.session.refresh();
    expect(env.session.refresh()).toBe(first);
    expect(env.session.pendingRefresh()).toBe(first);
    await until(() => env.api.count(REFRESH) === 1);
    answer(json(200, successBody(tokens(1))));
    await first;
    expect(env.session.pendingRefresh()).toBeNull();
  });

  test('a refresh that lands after sign-out does not resurrect the session', async () => {
    const env = setup();
    let answer: (r: Response) => void = () => undefined;
    env.api.on(REFRESH, () => new Promise<Response>((resolve) => (answer = resolve)));
    const outcome = env.session.refresh();
    await until(() => env.api.count(REFRESH) === 1);
    await env.session.signOutLocal();
    answer(json(200, successBody(tokens(1))));
    await expect(outcome).resolves.toEqual({ kind: 'ended' });
    expect(useAuthStore.getState()).toMatchObject({ status: 'signedOut', accessToken: null });
    expect(env.storage.set).not.toHaveBeenCalled();
  });

  test('a mid-session refresh does not override the app language', async () => {
    const env = setup();
    env.api.on(REFRESH, () => json(200, successBody(tokens(1))));
    await env.session.refresh();
    expect(env.onUserLocale).not.toHaveBeenCalled();
  });

  test('a malformed 200 is treated as unavailable and keeps the stored token', async () => {
    const env = setup();
    env.api.on(REFRESH, () => json(200, { data: { nope: true } }));
    await expect(env.session.refresh()).resolves.toEqual({ kind: 'unavailable', code: 'client.network' });
    expect(env.storage.remove).not.toHaveBeenCalled();
  });
});

describe('logout', () => {
  test('refreshes first on auth.unauthenticated, then revokes and clears', async () => {
    const env = setup();
    env.api.on(LOGOUT, (init) =>
      bearer(init) === 'Bearer access-0' ? json(401, errorBody('auth.unauthenticated')) : new Response(null, { status: 204 }),
    );
    env.api.on(REFRESH, () => json(200, successBody(tokens(1))));
    await env.session.logout();
    expect(env.api.keys()).toEqual([LOGOUT, REFRESH, LOGOUT]);
    expect(bearer(env.api.calls[2]!.init)).toBe('Bearer access-1');
    expect(useAuthStore.getState()).toMatchObject({ status: 'signedOut', accessToken: null, notice: null });
    expect(env.storage.remove).toHaveBeenCalled();
    expect(env.clear).toHaveBeenCalled();
  });

  test('logout-all uses its own endpoint', async () => {
    const env = setup();
    env.api.on('POST /v1/auth/logout-all', () => new Response(null, { status: 204 }));
    await env.session.logout('all');
    expect(env.api.keys()).toEqual(['POST /v1/auth/logout-all']);
    expect(useAuthStore.getState().status).toBe('signedOut');
  });

  test('clears locally on a final 401', async () => {
    const env = setup();
    env.api.on(LOGOUT, () => json(401, errorBody('auth.unauthenticated')));
    env.api.on(REFRESH, () => json(200, successBody(tokens(1))));
    await env.session.logout();
    expect(env.api.keys()).toEqual([LOGOUT, REFRESH, LOGOUT]);
    expect(useAuthStore.getState().status).toBe('signedOut');
    expect(env.storage.remove).toHaveBeenCalled();
  });

  test('clears locally on a network error', async () => {
    const env = setup();
    env.api.on(LOGOUT, () => {
      throw new TypeError('Network request failed');
    });
    await expect(env.session.logout()).resolves.toBeUndefined();
    expect(useAuthStore.getState().status).toBe('signedOut');
    expect(env.storage.remove).toHaveBeenCalled();
  });

  test('with no access token clears locally without calling the server', async () => {
    const env = setup({ signedIn: false });
    useAuthStore.getState().setStatus('restoreFailed');
    await env.session.logout();
    expect(env.api.fetchImpl).not.toHaveBeenCalled();
    expect(useAuthStore.getState().status).toBe('signedOut');
    expect(env.storage.remove).toHaveBeenCalled();
  });

  test('keeps the language setting', async () => {
    const env = setup();
    useSettingsStore.getState().setLanguage('en');
    env.api.on(LOGOUT, () => new Response(null, { status: 204 }));
    await env.session.logout();
    expect(useSettingsStore.getState().language).toBe('en');
  });
});

describe('signOutLocal', () => {
  test('clears storage, memory and queries', async () => {
    const env = setup();
    await env.session.signOutLocal();
    expect(env.storage.remove).toHaveBeenCalledTimes(1);
    expect(env.clear).toHaveBeenCalledTimes(1);
    expect(useAuthStore.getState()).toMatchObject({ status: 'signedOut', accessToken: null, user: null, notice: null });
  });

  test('survives a storage failure', async () => {
    const env = setup();
    env.storage.remove.mockRejectedValue(new Error('keystore locked'));
    await expect(env.session.signOutLocal('session_ended')).resolves.toBeUndefined();
    expect(useAuthStore.getState()).toMatchObject({ status: 'signedOut', notice: 'session_ended' });
  });
});

describe('restore (spec 6.4)', () => {
  const refreshOk = (env: Env, overrides: Record<string, unknown> = {}) =>
    env.api.on(REFRESH, () => json(200, successBody(tokens(1, overrides))));

  test('a null token is signedOut', async () => {
    const env = setup({ stored: null, signedIn: false });
    await env.session.restore();
    expect(useAuthStore.getState()).toMatchObject({ status: 'signedOut', notice: null });
    expect(env.api.fetchImpl).not.toHaveBeenCalled();
  });

  test('a rejected read is signedOut with a best-effort delete', async () => {
    const env = setup({ signedIn: false });
    env.storage.get.mockRejectedValue(new Error('keystore locked'));
    await env.session.restore();
    expect(useAuthStore.getState().status).toBe('signedOut');
    expect(env.storage.remove).toHaveBeenCalled();
    expect(env.api.fetchImpl).not.toHaveBeenCalled();
  });

  test('200 is signedIn with legalPending from the response and the access token set', async () => {
    const env = setup({ signedIn: false });
    refreshOk(env, { legal_acceptance_required: true, user: { ...userFixture, locale: 'en' } });
    await env.session.restore();
    expect(useAuthStore.getState()).toMatchObject({ status: 'signedIn', accessToken: 'access-1', legalPending: true });
    expect(env.storage.set).toHaveBeenCalledWith('refresh-1');
    expect(env.onUserLocale).toHaveBeenCalledWith('en');
  });

  test.each(['auth.refresh_invalid', 'auth.session_revoked'])('%s deletes the token and is signedOut', async (code) => {
    const env = setup({ signedIn: false });
    env.api.on(REFRESH, () => json(401, errorBody(code)));
    await env.session.restore();
    expect(useAuthStore.getState().status).toBe('signedOut');
    expect(env.storage.remove).toHaveBeenCalled();
  });

  test.each(['network', 'timeout', 503, 429] as const)('%s is restoreFailed and keeps the token', async (failure) => {
    if (failure === 'timeout') jest.useFakeTimers();
    const env = setup({ signedIn: false });
    env.api.on(REFRESH, (init) => {
      if (failure === 'network') throw new TypeError('Network request failed');
      if (failure === 'timeout') {
        return new Promise<Response>((_resolve, reject) => init.signal?.addEventListener('abort', () => reject(new Error('Aborted'))));
      }
      return json(failure, errorBody(failure === 429 ? 'rate_limit.exceeded' : 'internal.unexpected'), failure === 429 ? { 'Retry-After': '30' } : {});
    });
    const restoring = env.session.restore();
    if (failure === 'timeout') await jest.advanceTimersByTimeAsync(REFRESH_TIMEOUT_MS);
    await restoring;
    expect(useAuthStore.getState()).toMatchObject({ status: 'restoreFailed', accessToken: null });
    expect(env.storage.remove).not.toHaveBeenCalled();
    expect(env.storage.set).not.toHaveBeenCalled();
  });

  test('retry after restoreFailed can succeed', async () => {
    const env = setup({ signedIn: false });
    env.api.on(REFRESH, () => {
      throw new TypeError('Network request failed');
    });
    await env.session.restore();
    expect(useAuthStore.getState().status).toBe('restoreFailed');
    refreshOk(env);
    await env.session.restore();
    expect(useAuthStore.getState()).toMatchObject({ status: 'signedIn', accessToken: 'access-1' });
  });
});

describe('secure-store failure outcomes (spec 6.4)', () => {
  test('a write failure deletes the stale token and stays in memory', async () => {
    const env = setup({ signedIn: false });
    env.storage.set.mockRejectedValue(new Error('keystore locked'));
    await env.session.applyTokens(tokens(1));
    expect(env.storage.remove).toHaveBeenCalled();
    expect(useAuthStore.getState()).toMatchObject({ status: 'signedIn', accessToken: 'access-1', notice: 'storage_failed' });
  });

  test('a write failure where delete also fails does not throw', async () => {
    const env = setup({ signedIn: false });
    env.storage.set.mockRejectedValue(new Error('keystore locked'));
    env.storage.remove.mockRejectedValue(new Error('keystore locked'));
    await expect(env.session.applyTokens(tokens(1))).resolves.toBeUndefined();
    expect(useAuthStore.getState()).toMatchObject({ status: 'signedIn', notice: 'storage_failed' });
  });

  test('applyTokens writes storage, sets state and calls onUserLocale with user.locale', async () => {
    const env = setup({ signedIn: false });
    useAuthStore.getState().setStatus('signedOut');
    await env.session.applyTokens(tokens(1, { user: { ...userFixture, locale: 'en' }, legal_acceptance_required: true }));
    expect(env.storage.set).toHaveBeenCalledWith('refresh-1');
    expect(env.onUserLocale).toHaveBeenCalledWith('en');
    expect(useAuthStore.getState()).toMatchObject({ status: 'signedIn', accessToken: 'access-1', legalPending: true });
  });

  test('a successful login clears an old session notice', async () => {
    const env = setup({ signedIn: false });
    useAuthStore.getState().clearSession('session_ended');
    await env.session.applyTokens(tokens(1));
    expect(useAuthStore.getState().notice).toBeNull();
  });
});

describe('races and failure handling', () => {
  const refreshOk = (env: Env) => env.api.on(REFRESH, () => json(200, successBody(tokens(1))));

  test('a sign-out while the token write is pending is not undone', async () => {
    const env = setup();
    let release: () => void = () => undefined;
    env.storage.set.mockReturnValue(new Promise<void>((resolve) => (release = () => resolve())));
    refreshOk(env);
    const outcome = env.session.refresh();
    await until(() => env.storage.set.mock.calls.length > 0);
    await env.session.signOutLocal();
    env.storage.remove.mockClear();
    release();
    await expect(outcome).resolves.toEqual({ kind: 'ended' });
    expect(useAuthStore.getState()).toMatchObject({ status: 'signedOut', accessToken: null });
    expect(env.storage.remove).toHaveBeenCalled();
  });

  test('a sign-out while the stored token is being read discards the refresh without a notice', async () => {
    const env = setup();
    let release: (token: string | null) => void = () => undefined;
    env.storage.get.mockReturnValue(new Promise<string | null>((resolve) => (release = resolve)));
    const outcome = env.session.refresh();
    await env.session.signOutLocal();
    release(null);
    await expect(outcome).resolves.toEqual({ kind: 'ended' });
    expect(env.api.fetchImpl).not.toHaveBeenCalled();
    expect(useAuthStore.getState()).toMatchObject({ status: 'signedOut', notice: null });
  });

  test('a write failure keeps the rotated token in memory for the next refresh', async () => {
    const env = setup();
    env.storage.set.mockRejectedValue(new Error('keystore locked'));
    refreshOk(env);
    await expect(env.session.refresh()).resolves.toEqual({ kind: 'refreshed' });
    expect(env.storage.remove).toHaveBeenCalled();
    expect(useAuthStore.getState()).toMatchObject({ status: 'signedIn', accessToken: 'access-1', notice: 'storage_failed' });
    env.storage.get.mockResolvedValue(null);
    env.api.on(REFRESH, () => json(200, successBody(tokens(2))));
    await expect(env.session.refresh()).resolves.toEqual({ kind: 'refreshed' });
    expect(JSON.parse(env.api.calls[1]!.init.body as string)).toEqual({ refresh_token: 'refresh-1' });
    expect(useAuthStore.getState().accessToken).toBe('access-2');
  });

  test('the in-memory token is dropped on sign-out', async () => {
    const env = setup();
    env.storage.set.mockRejectedValue(new Error('keystore locked'));
    refreshOk(env);
    await env.session.refresh();
    await env.session.signOutLocal();
    useAuthStore.getState().setSession({ accessToken: 'a', user: userFixture, legalPending: false });
    env.storage.get.mockResolvedValue(null);
    await expect(env.session.refresh()).resolves.toEqual({ kind: 'ended' });
    expect(env.api.count(REFRESH)).toBe(1);
  });

  test('a keystore read rejection mid-session keeps the session and the token', async () => {
    const env = setup();
    env.storage.get.mockRejectedValue(new Error('keystore locked'));
    await expect(env.session.refresh()).resolves.toEqual({ kind: 'unavailable', code: 'client.network' });
    expect(env.storage.remove).not.toHaveBeenCalled();
    expect(useAuthStore.getState()).toMatchObject({ status: 'signedIn', accessToken: 'access-0' });
  });

  test('a true null read mid-session ends the session', async () => {
    const env = setup({ stored: null });
    await expect(env.session.refresh()).resolves.toEqual({ kind: 'ended' });
    expect(useAuthStore.getState()).toMatchObject({ status: 'signedOut', notice: 'session_ended' });
  });

  test('signOutLocal removes the stored token even when clearing queries throws', async () => {
    const env = setup();
    env.clear.mockImplementation(() => {
      throw new Error('boom');
    });
    await expect(env.session.signOutLocal()).rejects.toThrow('boom');
    expect(env.storage.remove).toHaveBeenCalled();
  });

  test('restore signs an admin out locally with the admin_web_only notice', async () => {
    const env = setup({ signedIn: false });
    env.api.on(REFRESH, () => json(200, successBody(tokens(1, { user: { ...userFixture, role: 'admin' } }))));
    await env.session.restore();
    expect(useAuthStore.getState()).toMatchObject({ status: 'signedOut', accessToken: null, notice: 'admin_web_only' });
    expect(env.storage.set).not.toHaveBeenCalled();
    expect(env.storage.remove).toHaveBeenCalled();
  });

  test('applyTokens signs an admin out with the admin_web_only notice', async () => {
    const env = setup({ signedIn: false });
    useAuthStore.getState().setStatus('signedOut');
    await env.session.applyTokens(tokens(1, { user: { ...userFixture, role: 'admin' } }));
    expect(useAuthStore.getState()).toMatchObject({ status: 'signedOut', notice: 'admin_web_only' });
    expect(env.storage.set).not.toHaveBeenCalled();
  });
});

test('the refresh error type used by the client is an ApiError', async () => {
  const env = setup();
  env.api.on(VEHICLES, () => json(401, errorBody('auth.unauthenticated')));
  env.api.on(REFRESH, () => {
    throw new TypeError('Network request failed');
  });
  await expect(getVehicles(env)).rejects.toBeInstanceOf(ApiError);
});
