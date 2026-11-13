import { ApiError, parseEnvelope, parseNoContent, tokenResponseSchema, type TokenResponse } from '@mypark/contracts';
import type { QueryClient } from '@tanstack/react-query';

import type { AppLanguage } from '@/lib/i18n/pickLanguage';
import { useAuthFlowStore } from '@/store/auth-flow';
import type { AuthNotice, useAuthStore } from '@/store/auth';

import { REFRESH_TIMEOUT_MS, type AuthAdapter, type HttpClient, type RefreshResult, type RequestResult } from './httpClient';
import type { TokenStorage } from './tokenStorage';

export type SessionService = {
  /** App start: read the stored token and exchange it (spec 6.4). Never throws. */
  restore(): Promise<void>;
  /** Login result: persist the refresh token, then set the in-memory session. An admin account is signed out instead, with notice `admin_web_only`. */
  applyTokens(tokens: TokenResponse): Promise<void>;
  /** Single-flight: concurrent callers share one request, because refresh tokens are single-use. */
  refresh(): Promise<RefreshResult>;
  /** The refresh in flight, if any. It contains no `auth: true` call, so waiting on it cannot deadlock. */
  pendingRefresh(): Promise<RefreshResult> | null;
  logout(scope?: 'this' | 'all'): Promise<void>;
  signOutLocal(notice?: AuthNotice): Promise<void>;
};

export type SessionDeps = {
  http: HttpClient;
  storage: TokenStorage;
  store: typeof useAuthStore;
  queryClient: QueryClient;
  onUserLocale(locale: AppLanguage): void;
  now?: () => number;
};

/** Used when a 429 carries no usable wait time. */
const DEFAULT_RETRY_AFTER_SECONDS = 60;

/** Written over a refresh token that could not be deleted. */
const TOMBSTONE = '';

const parseTokens = parseEnvelope(tokenResponseSchema);

/** The ApiError carried by an error envelope, if the response is one. */
function apiErrorOf(res: RequestResult): ApiError | undefined {
  try {
    parseNoContent(res.body, res.status);
  } catch (error) {
    return error instanceof ApiError ? error : undefined;
  }
  return undefined;
}

function retryAfterSeconds(res: RequestResult): number {
  const header = res.retryAfter === null ? NaN : Number(res.retryAfter);
  if (Number.isFinite(header) && header > 0) return Math.ceil(header);
  const fromBody = apiErrorOf(res)?.details.retry_after_seconds;
  if (typeof fromBody === 'number' && Number.isFinite(fromBody) && fromBody > 0) return Math.ceil(fromBody);
  return DEFAULT_RETRY_AFTER_SECONDS;
}

export function createSessionService(deps: SessionDeps): SessionService {
  const { http, storage, store, queryClient, onUserLocale } = deps;
  const now = deps.now ?? Date.now;
  let inFlight: Promise<RefreshResult> | null = null;
  /** No refresh request before this instant (a 429 with Retry-After). */
  let blockedUntil = 0;
  /** Bumped on every local sign-out so a refresh that lands afterwards cannot revive the session. */
  let epoch = 0;
  /** A rotated refresh token that could not be written to secure storage. Memory only, this run only. */
  let memoryToken: string | null = null;

  /**
   * Best effort, never throws. When the delete fails the old token would sign the user back in on the next start,
   * so it is overwritten with an empty tombstone, which readStoredToken treats as no token.
   */
  async function removeStoredToken(): Promise<void> {
    try {
      await storage.remove();
    } catch {
      try {
        await storage.set(TOMBSTONE);
      } catch {
        // Nothing else can be done about a locked keystore.
      }
    }
  }

  /** The stored refresh token, or null when there is none or it is the tombstone. */
  async function readStoredToken(): Promise<string | null> {
    const token = await storage.get();
    return token === TOMBSTONE ? null : token;
  }

  /**
   * The write lands before state changes and before the caller resumes (single-use tokens).
   * Returns false when the result was discarded: an admin account, or a sign-out that happened meanwhile.
   */
  async function commit(tokens: TokenResponse, applyLocale: boolean, startedEpoch: number): Promise<boolean> {
    if (tokens.user.role === 'admin') {
      // The driver app has nothing for admins: they use the web admin area (ruling T9).
      await signOutLocal('admin_web_only');
      return false;
    }
    let stored = true;
    try {
      await storage.set(tokens.refresh_token);
    } catch {
      // The stored token is now stale and would revoke the session on the next start (CC-17).
      stored = false;
      await removeStoredToken();
    }
    if (epoch !== startedEpoch) {
      // Signed out while the write was pending: leave no token behind and do not revive the session.
      memoryToken = null;
      await removeStoredToken();
      return false;
    }
    // On a write failure the rotated token lives in memory for this run so the next 401 can still refresh.
    // The one-time notice is raised only on the move from stored to memory-only.
    const becameMemoryOnly = !stored && memoryToken === null;
    memoryToken = stored ? null : tokens.refresh_token;
    const state = store.getState();
    state.clearNotice();
    state.setSession({
      accessToken: tokens.access_token,
      user: tokens.user,
      legalPending: tokens.legal_acceptance_required,
    });
    if (becameMemoryOnly) store.getState().setNotice('storage_failed');
    if (applyLocale) onUserLocale(tokens.user.locale);
    return true;
  }

  async function signOutLocal(notice: AuthNotice = null): Promise<void> {
    epoch += 1;
    memoryToken = null;
    // A late request must not wait on a refresh of the session that just ended (its own .finally only clears itself).
    inFlight = null;
    const { user } = store.getState();
    try {
      // A lost or revoked session shows the login screen with the email pre-filled when it is known.
      if (notice === 'session_ended' && user) useAuthFlowStore.getState().setEmail(user.email);
      store.getState().clearSession(notice);
      queryClient.clear();
    } finally {
      await removeStoredToken();
    }
  }

  async function runRefresh(): Promise<RefreshResult> {
    const remaining = blockedUntil - now();
    if (remaining > 0) return { kind: 'rate_limited', retryAfterSeconds: Math.ceil(remaining / 1000) };

    // Captured before the first await: any sign-out from here on discards this refresh.
    const startedEpoch = epoch;
    let token: string | null = memoryToken;
    if (token === null) {
      try {
        token = await readStoredToken();
      } catch {
        // A keystore hiccup is not a verdict on the session: keep everything and try again later.
        return { kind: 'unavailable', code: 'client.network' };
      }
    }
    if (epoch !== startedEpoch) return { kind: 'ended' };
    if (token === null) {
      // Nothing to exchange: the session cannot continue.
      await signOutLocal('session_ended');
      return { kind: 'ended' };
    }

    const wasSignedIn = store.getState().status === 'signedIn';
    let res: RequestResult;
    try {
      res = await http.request({
        method: 'POST',
        path: '/v1/auth/refresh',
        body: { refresh_token: token },
        auth: false,
        timeoutMs: REFRESH_TIMEOUT_MS,
      });
    } catch (error) {
      const code = error instanceof ApiError && error.code === 'client.timeout' ? 'client.timeout' : 'client.network';
      return { kind: 'unavailable', code };
    }
    if (epoch !== startedEpoch) return { kind: 'ended' };

    if (res.status === 200) {
      let tokens: TokenResponse;
      try {
        tokens = parseTokens(res.body, res.status);
      } catch {
        // A 200 means the server already rotated the token: the old one is dead and replaying it revokes the family.
        await signOutLocal('session_ended');
        return { kind: 'ended' };
      }
      return (await commit(tokens, !wasSignedIn, startedEpoch)) ? { kind: 'refreshed' } : { kind: 'ended' };
    }
    if (res.status === 429) {
      const seconds = retryAfterSeconds(res);
      blockedUntil = now() + seconds * 1000;
      return { kind: 'rate_limited', retryAfterSeconds: seconds };
    }
    const code = apiErrorOf(res)?.code;
    if (res.status === 401 && (code === 'auth.refresh_invalid' || code === 'auth.session_revoked')) {
      await signOutLocal('session_ended');
      return { kind: 'ended' };
    }
    // 5xx and anything unexpected: keep the tokens, the next request tries again.
    return { kind: 'unavailable', code: 'client.network' };
  }

  function refresh(): Promise<RefreshResult> {
    if (inFlight) return inFlight;
    const running = runRefresh().finally(() => {
      if (inFlight === running) inFlight = null;
    });
    inFlight = running;
    return running;
  }

  return {
    refresh,
    pendingRefresh: () => inFlight,
    signOutLocal,

    async applyTokens(tokens) {
      await commit(tokens, true, epoch);
    },

    async restore() {
      let token: string | null;
      try {
        token = await readStoredToken();
      } catch {
        await removeStoredToken();
        store.getState().clearSession();
        return;
      }
      if (token === null) {
        store.getState().clearSession();
        return;
      }
      const outcome = await refresh();
      // The token stays in storage: only a definitive answer from the server removes it.
      if (outcome.kind === 'rate_limited' || outcome.kind === 'unavailable') store.getState().setStatus('restoreFailed');
    },

    async logout(scope = 'this') {
      if (store.getState().accessToken !== null) {
        try {
          // An expired access token is refreshed first by the client, so the server session is really revoked.
          await http.request({ method: 'POST', path: scope === 'all' ? '/v1/auth/logout-all' : '/v1/auth/logout', auth: true });
        } catch {
          // Network, timeout or refresh trouble: the local session still ends.
        }
      }
      await signOutLocal();
    },
  };
}

/** What the HTTP client may ask of the session layer. Shared by the runtime and the tests. */
export function createAuthAdapter(session: SessionService, store: typeof useAuthStore): AuthAdapter {
  return {
    getAccessToken: () => store.getState().accessToken,
    refresh: () => session.refresh(),
    pendingRefresh: () => session.pendingRefresh(),
    onSessionRevoked: () => session.signOutLocal('session_ended'),
    onLegalRequired: () => store.getState().setLegalPending(true),
  };
}
