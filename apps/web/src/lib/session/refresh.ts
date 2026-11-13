// Only proxy.ts may import this module (enforced by ESLint, spec 5.3): a second
// entry point would compile a second copy of the lock and rotate a token twice.
import { createHash } from 'node:crypto';
import { errorEnvelopeSchema, parseEnvelope, tokenResponseSchema } from '@mypark/contracts';
import type { NextRequest } from 'next/server';
import { apiUrl } from '@/lib/bff/upstream';
import {
  ACCESS_COOKIE,
  CSRF_COOKIE,
  REFRESH_COOKIE,
  newCsrfToken,
  type RefreshOutcome,
  type SessionState,
} from './cookies';

export const RESULT_TTL_MS = 30_000;
export const MAX_RESULTS = 1_000;
export const REFRESH_TIMEOUT_MS = 10_000;

export type RefreshInput = {
  refreshToken: string;
  /** The caller's `mp_csrf`, or undefined when it has none. An absent value never matches (CC-26). */
  csrf: string | undefined;
  forwardedFor: string | null;
};

type Flight = { csrf: string | undefined; promise: Promise<RefreshOutcome> };
type CachedResult = { outcome: RefreshOutcome; csrf: string | undefined; expiresAt: number };

const SINGLETON = Symbol.for('mypark.session.refresh');
const ENDED_CODES = new Set(['auth.refresh_invalid', 'auth.session_revoked']);
const parseTokens = parseEnvelope(tokenResponseSchema);

// Keys are a hash so a refresh token never sits in a Map key, a heap dump label or an error message.
const keyOf = (refreshToken: string) => createHash('sha256').update(refreshToken).digest('hex');

export function createRefreshCoordinator(deps: { fetchImpl?: typeof fetch; now?: () => number }) {
  const now = deps.now ?? Date.now;
  const flights = new Map<string, Flight>();
  // Insertion order is expiry order, so the oldest entry is always first.
  const results = new Map<string, CachedResult>();

  /**
   * One upstream rotation. Never throws. A failure is `unavailable`, which keeps the cookies, except
   * a 200 that cannot be read: Go has already rotated (spent) the token it was sent, so keeping that
   * cookie would only trip reuse detection later. That case is `ended`, as on mobile.
   */
  async function callUpstream(input: RefreshInput): Promise<RefreshOutcome> {
    let rotated = false;
    try {
      const response = await (deps.fetchImpl ?? fetch)(apiUrl('/v1/auth/refresh'), {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Accept: 'application/json',
          ...(input.forwardedFor ? { 'X-Forwarded-For': input.forwardedFor } : {}),
        },
        body: JSON.stringify({ refresh_token: input.refreshToken }),
        cache: 'no-store',
        // Deliberately not the incoming request's signal: a client that
        // navigates away must not abort a rotation other requests wait on.
        signal: AbortSignal.timeout(REFRESH_TIMEOUT_MS),
      });
      rotated = response.status === 200;
      const body: unknown = await response.json();

      if (rotated) {
        const tokens = parseTokens(body, 200);
        return {
          kind: 'refreshed',
          tokens: {
            accessToken: tokens.access_token,
            refreshToken: tokens.refresh_token,
            expiresIn: tokens.expires_in,
            refreshExpiresIn: tokens.refresh_expires_in,
          },
        };
      }
      if (response.status === 401) {
        const error = errorEnvelopeSchema.safeParse(body);
        if (error.success && ENDED_CODES.has(error.data.error.code)) return { kind: 'ended' };
      }
      return { kind: 'unavailable' };
    } catch {
      return rotated ? { kind: 'ended' } : { kind: 'unavailable' };
    }
  }

  function remember(key: string, outcome: RefreshOutcome, csrf: string | undefined) {
    const at = now();
    for (const [oldKey, entry] of results) {
      if (entry.expiresAt > at) break;
      results.delete(oldKey);
    }
    results.delete(key);
    results.set(key, { outcome, csrf, expiresAt: at + RESULT_TTL_MS });
    while (results.size > MAX_RESULTS) results.delete(results.keys().next().value!);
  }

  return {
    async refresh(input: RefreshInput): Promise<RefreshOutcome> {
      const key = keyOf(input.refreshToken);
      const csrfMatches = (recorded: string | undefined) => !!input.csrf && recorded === input.csrf;

      const flight = flights.get(key);
      if (flight && csrfMatches(flight.csrf)) return flight.promise;

      const cached = results.get(key);
      if (cached && cached.expiresAt > now() && csrfMatches(cached.csrf)) return cached.outcome;

      // A request that does not match goes to Go and trips reuse detection, as it should.
      // It is not registered: it must not displace a flight others are waiting on.
      if (flight) return callUpstream(input);

      const promise = callUpstream(input).then((outcome) => {
        if (outcome.kind === 'refreshed') remember(key, outcome, input.csrf);
        flights.delete(key);
        return outcome;
      });
      flights.set(key, { csrf: input.csrf, promise });
      return promise;
    },
  };
}

export type RefreshCoordinator = ReturnType<typeof createRefreshCoordinator>;

/** One coordinator per process, kept on `globalThis` so dev hot reload does not reset the lock. */
export function getRefreshCoordinator(): RefreshCoordinator {
  const store = globalThis as typeof globalThis & { [SINGLETON]?: RefreshCoordinator };
  return (store[SINGLETON] ??= createRefreshCoordinator({}));
}

/**
 * Refreshes the session when the access cookie has lapsed and a refresh cookie
 * exists, and reports what the proxy must write. Also issues `mp_csrf` when absent.
 */
export async function ensureSession(request: NextRequest): Promise<SessionState> {
  const access = request.cookies.get(ACCESS_COOKIE)?.value;
  const refreshToken = request.cookies.get(REFRESH_COOKIE)?.value;
  const existingCsrf = request.cookies.get(CSRF_COOKIE)?.value || undefined;

  const refresh =
    !access && refreshToken
      ? await getRefreshCoordinator().refresh({
          refreshToken,
          csrf: existingCsrf,
          forwardedFor: request.headers.get('x-forwarded-for'),
        })
      : null;

  return {
    refresh,
    csrf: existingCsrf ? { value: existingCsrf, issued: false } : { value: newCsrfToken(), issued: true },
  };
}
