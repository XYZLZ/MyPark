// @vitest-environment node
import { errorBody, successBody, tokenResponseFixture } from '@mypark/contracts/fixtures';
import { afterEach, describe, expect, test, vi } from 'vitest';
import type { RefreshOutcome } from './cookies';
import { MAX_RESULTS, REFRESH_TIMEOUT_MS, RESULT_TTL_MS, createRefreshCoordinator, getRefreshCoordinator } from './refresh';

const SINGLETON = Symbol.for('mypark.session.refresh');

afterEach(() => {
  vi.restoreAllMocks();
  delete (globalThis as Record<symbol, unknown>)[SINGLETON];
});

const tokenBody = (n: number) =>
  successBody({ ...tokenResponseFixture, access_token: `access-${n}`, refresh_token: `refresh-${n}` });

const ok = (n: number) => Response.json(tokenBody(n), { status: 200 });
const input = (over: Partial<{ refreshToken: string; csrf: string | undefined; forwardedFor: string | null }> = {}) => ({
  refreshToken: 'R1',
  csrf: 'csrf-1' as string | undefined,
  forwardedFor: null as string | null,
  ...over,
});

/** A fetch stub that numbers its successes (call 1 returns access-1) and can be held open. */
function setup(respond: (call: number) => Response | Promise<Response> = ok) {
  let gate: Promise<void> = Promise.resolve();
  let open = () => {};
  const fetchImpl: ReturnType<typeof vi.fn<typeof fetch>> = vi.fn<typeof fetch>(async () => {
    const call = fetchImpl.mock.calls.length;
    await gate;
    return respond(call);
  });
  const hold = () => {
    gate = new Promise<void>((resolve) => {
      open = resolve;
    });
  };
  let now = 1_000_000;
  const coordinator = createRefreshCoordinator({
    fetchImpl: fetchImpl as unknown as typeof fetch,
    now: () => now,
  });
  return {
    fetchImpl,
    coordinator,
    hold,
    release: () => open(),
    advance: (ms: number) => {
      now += ms;
    },
  };
}

const access = (outcome: RefreshOutcome) => (outcome.kind === 'refreshed' ? outcome.tokens.accessToken : null);

describe('single flight', () => {
  test('joins an in-flight refresh', async () => {
    const s = setup();
    s.hold();
    const a = s.coordinator.refresh(input());
    const b = s.coordinator.refresh(input());
    s.release();
    const [ra, rb] = await Promise.all([a, b]);
    expect(s.fetchImpl).toHaveBeenCalledTimes(1);
    expect(ra).toEqual(rb);
    expect(access(ra)).toBe('access-1');
  });

  test('refreshes exactly once for N concurrent requests', async () => {
    const s = setup();
    s.hold();
    const all = Array.from({ length: 20 }, () => s.coordinator.refresh(input()));
    s.release();
    const outcomes = await Promise.all(all);
    expect(s.fetchImpl).toHaveBeenCalledTimes(1);
    expect(new Set(outcomes.map(access))).toEqual(new Set(['access-1']));
  });

  test('late arrival is served from the result cache', async () => {
    const s = setup();
    const first = await s.coordinator.refresh(input());
    s.advance(RESULT_TTL_MS - 1_000);
    const late = await s.coordinator.refresh(input());
    expect(s.fetchImpl).toHaveBeenCalledTimes(1);
    expect(late).toEqual(first);
  });

  test('a late arrival with a different mp_csrf is not served', async () => {
    const s = setup();
    await s.coordinator.refresh(input());
    await s.coordinator.refresh(input({ csrf: 'other' }));
    expect(s.fetchImpl).toHaveBeenCalledTimes(2);
  });

  test('a late arrival with no mp_csrf is not served', async () => {
    const s = setup();
    await s.coordinator.refresh(input());
    await s.coordinator.refresh(input({ csrf: undefined }));
    expect(s.fetchImpl).toHaveBeenCalledTimes(2);
  });

  test('a flight started without mp_csrf is not joined by a request without mp_csrf', async () => {
    const s = setup();
    s.hold();
    const a = s.coordinator.refresh(input({ csrf: undefined }));
    const b = s.coordinator.refresh(input({ csrf: undefined }));
    s.release();
    await Promise.all([a, b]);
    expect(s.fetchImpl).toHaveBeenCalledTimes(2);
  });

  test('a concurrent request with a different mp_csrf does not join and does not displace the flight', async () => {
    const s = setup();
    s.hold();
    const a = s.coordinator.refresh(input({ csrf: 'c1' }));
    const b = s.coordinator.refresh(input({ csrf: 'c2' }));
    expect(s.fetchImpl).toHaveBeenCalledTimes(2);
    s.release();
    const [ra, rb] = await Promise.all([a, b]);
    expect(access(ra)).toBe('access-1');
    expect(access(rb)).toBe('access-2');

    // The registered flight (c1) still feeds the cache; the stray call (c2) does not.
    const lateC1 = await s.coordinator.refresh(input({ csrf: 'c1' }));
    expect(access(lateC1)).toBe('access-1');
    expect(s.fetchImpl).toHaveBeenCalledTimes(2);
    await s.coordinator.refresh(input({ csrf: 'c2' }));
    expect(s.fetchImpl).toHaveBeenCalledTimes(3);
  });

  test('flights are keyed per refresh token', async () => {
    const s = setup();
    s.hold();
    const a = s.coordinator.refresh(input({ refreshToken: 'RA' }));
    const b = s.coordinator.refresh(input({ refreshToken: 'RB' }));
    expect(s.fetchImpl).toHaveBeenCalledTimes(2);
    s.release();
    await Promise.all([a, b]);
    const bodies = s.fetchImpl.mock.calls.map(([, init]) => JSON.parse(String(init?.body)).refresh_token);
    expect(bodies.sort()).toEqual(['RA', 'RB']);
  });

  test('an empty mp_csrf never matches a cache entry or joins a flight', async () => {
    const s = setup();
    s.hold();
    const a = s.coordinator.refresh(input({ csrf: '' }));
    const b = s.coordinator.refresh(input({ csrf: '' }));
    s.release();
    await Promise.all([a, b]);
    expect(s.fetchImpl).toHaveBeenCalledTimes(2);
    await s.coordinator.refresh(input({ csrf: '' }));
    expect(s.fetchImpl).toHaveBeenCalledTimes(3);
  });

  test('cache entry expires after 30 s', async () => {
    const s = setup();
    await s.coordinator.refresh(input());
    s.advance(RESULT_TTL_MS + 1);
    await s.coordinator.refresh(input());
    expect(s.fetchImpl).toHaveBeenCalledTimes(2);
    expect(RESULT_TTL_MS).toBe(30_000);
  });

  test('evicts the oldest entry past 1,000 results', async () => {
    const s = setup();
    for (let i = 0; i <= MAX_RESULTS; i++) {
      await s.coordinator.refresh(input({ refreshToken: `token-${i}` }));
    }
    expect(s.fetchImpl).toHaveBeenCalledTimes(MAX_RESULTS + 1);
    await s.coordinator.refresh(input({ refreshToken: `token-${MAX_RESULTS}` }));
    expect(s.fetchImpl).toHaveBeenCalledTimes(MAX_RESULTS + 1);
    await s.coordinator.refresh(input({ refreshToken: 'token-0' }));
    expect(s.fetchImpl).toHaveBeenCalledTimes(MAX_RESULTS + 2);
  });
});

describe('outcomes', () => {
  test('401 clears cookies', async () => {
    const s = setup(() => Response.json(errorBody('auth.refresh_invalid'), { status: 401 }));
    expect(await s.coordinator.refresh(input())).toEqual({ kind: 'ended' });
  });

  test('session_revoked clears cookies', async () => {
    const s = setup(() => Response.json(errorBody('auth.session_revoked'), { status: 401 }));
    expect(await s.coordinator.refresh(input())).toEqual({ kind: 'ended' });
  });

  test('a 401 with any other code keeps cookies', async () => {
    const s = setup(() => Response.json(errorBody('auth.unauthenticated'), { status: 401 }));
    expect(await s.coordinator.refresh(input())).toEqual({ kind: 'unavailable' });
  });

  test('5xx keeps cookies and is not cached', async () => {
    const s = setup(() => Response.json(errorBody('internal.unexpected'), { status: 503 }));
    expect(await s.coordinator.refresh(input())).toEqual({ kind: 'unavailable' });
    await s.coordinator.refresh(input());
    expect(s.fetchImpl).toHaveBeenCalledTimes(2);
  });

  test('an ended outcome is not cached either', async () => {
    const s = setup(() => Response.json(errorBody('auth.refresh_invalid'), { status: 401 }));
    await s.coordinator.refresh(input());
    await s.coordinator.refresh(input());
    expect(s.fetchImpl).toHaveBeenCalledTimes(2);
  });

  test('429 and network errors keep cookies', async () => {
    const limited = setup(() => Response.json(errorBody('rate_limit.exceeded'), { status: 429 }));
    expect(await limited.coordinator.refresh(input())).toEqual({ kind: 'unavailable' });

    const down = setup(() => {
      throw new TypeError('fetch failed');
    });
    expect(await down.coordinator.refresh(input())).toEqual({ kind: 'unavailable' });
  });

  test('a malformed 200 keeps cookies', async () => {
    const s = setup(() => Response.json({ data: { foo: 1 }, meta: { request_id: 'x' } }, { status: 200 }));
    expect(await s.coordinator.refresh(input())).toEqual({ kind: 'unavailable' });
    const text = setup(() => new Response('<html>', { status: 200 }));
    expect(await text.coordinator.refresh(input())).toEqual({ kind: 'unavailable' });
  });

  test('maps the token response to a token pair', async () => {
    const s = setup();
    expect(await s.coordinator.refresh(input())).toEqual({
      kind: 'refreshed',
      tokens: { accessToken: 'access-1', refreshToken: 'refresh-1', expiresIn: 900, refreshExpiresIn: 2_592_000 },
    });
  });
});

describe('upstream call', () => {
  test('does not abort when the caller disconnects', async () => {
    const timeout = vi.spyOn(AbortSignal, 'timeout');
    let seen: AbortSignal | undefined;
    let release = () => {};
    const gate = new Promise<void>((resolve) => (release = resolve));
    // Honors its signal the way real fetch does: an abort rejects the call.
    const fetchImpl = vi.fn<typeof fetch>((_url, init) => {
      seen = init?.signal ?? undefined;
      return new Promise<Response>((resolve, reject) => {
        seen?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')));
        void gate.then(() => resolve(ok(1)));
      });
    });
    const coordinator = createRefreshCoordinator({ fetchImpl: fetchImpl as unknown as typeof fetch });

    // The initiating request's own signal: the coordinator never receives it.
    const initiator = new AbortController();
    const initiating = coordinator.refresh(input());
    const waiter = coordinator.refresh(input());
    initiator.abort();
    release();

    const [a, b] = await Promise.all([initiating, waiter]);
    expect(access(a)).toBe('access-1');
    expect(b).toEqual(a);
    expect(fetchImpl).toHaveBeenCalledTimes(1);
    expect(timeout).toHaveBeenCalledWith(REFRESH_TIMEOUT_MS);
    expect(REFRESH_TIMEOUT_MS).toBe(10_000);
    expect(seen).toBe(timeout.mock.results[0]?.value);
    expect(seen?.aborted).toBe(false);
  });

  test('sends refresh_token and forwards X-Forwarded-For unchanged', async () => {
    const s = setup();
    await s.coordinator.refresh(input({ forwardedFor: '203.0.113.9, 10.0.0.2' }));
    const [url, init] = s.fetchImpl.mock.calls[0]!;
    expect(String(url)).toMatch(/\/v1\/auth\/refresh$/);
    expect(init?.method).toBe('POST');
    expect(JSON.parse(String(init?.body))).toEqual({ refresh_token: 'R1' });
    expect((init?.headers as Record<string, string>)['X-Forwarded-For']).toBe('203.0.113.9, 10.0.0.2');
  });

  test('omits X-Forwarded-For when there is none', async () => {
    const s = setup();
    await s.coordinator.refresh(input({ forwardedFor: null }));
    const headers = s.fetchImpl.mock.calls[0]![1]?.headers as Record<string, string>;
    expect(Object.keys(headers).map((h) => h.toLowerCase())).not.toContain('x-forwarded-for');
  });
});

describe('singleton', () => {
  test('survives a module reload through the globalThis singleton', async () => {
    const first = getRefreshCoordinator();
    vi.resetModules();
    const reloaded = await import('./refresh');
    expect(reloaded.getRefreshCoordinator()).toBe(first);
    expect((globalThis as Record<symbol, unknown>)[SINGLETON]).toBe(first);
  });
});
