import { fetch } from "expo/fetch";
import { ApiError, toHealthState, type ApiHealthState, type ClientErrorCode } from '@mypark/contracts';

export const DEFAULT_TIMEOUT_MS = 3000;
/** Login, register, reset and `DELETE /v1/me`: argon2 sits behind a 4-slot semaphore (CC-16). */
export const ARGON2_TIMEOUT_MS = 20_000;
export const REFRESH_TIMEOUT_MS = 10_000;

export type HttpResponse = { status: number; body: unknown };

export type HttpRequest = {
  method: 'GET' | 'POST' | 'PATCH' | 'DELETE';
  path: string;
  body?: unknown;
  /** Send the bearer and run the 401 refresh flow. Default false. */
  auth?: boolean;
  timeoutMs?: number;
};

export type RequestResult = HttpResponse & { retryAfter: string | null };

export type RefreshResult =
  | { kind: 'refreshed' }
  | { kind: 'ended' }
  | { kind: 'rate_limited'; retryAfterSeconds: number }
  | { kind: 'unavailable'; code: ClientErrorCode };

/** What the session layer lends the client. The client never reads the stores itself. */
export type AuthAdapter = {
  getAccessToken(): string | null;
  /** Must share one in-flight refresh between concurrent callers: refresh tokens are single-use. */
  refresh(): Promise<RefreshResult>;
  /** The in-flight refresh, if any. Requests wait for it before sending so they do not use an expired token. */
  pendingRefresh?(): Promise<unknown> | null;
  onSessionRevoked(): Promise<void>;
  onLegalRequired(): void;
};

export type HttpClient = {
  /** Resolves with any HTTP status. Rejects only on network failure or timeout. */
  get(path: string, init?: { timeoutMs?: number }): Promise<HttpResponse>;
  /**
   * Resolves with any HTTP status. Rejects with an ApiError for `client.network`,
   * `client.timeout` and refresh failures.
   */
  request(req: HttpRequest): Promise<RequestResult>;
  setAuthAdapter(adapter: AuthAdapter): void;
};

type FetchImpl = (input: string, init?: RequestInit) => Promise<Response>;

export type HttpClientOptions = {
  /** Default per-request timeout. A request can still override it. */
  timeoutMs?: number;
};

function errorCode(body: unknown): string | undefined {
  if (typeof body !== 'object' || body === null || !('error' in body)) return undefined;
  const error = (body as { error: unknown }).error;
  if (typeof error !== 'object' || error === null || !('code' in error)) return undefined;
  const code = (error as { code: unknown }).code;
  return typeof code === 'string' ? code : undefined;
}

/**
 * Runs `parse` on a response. When it throws an ApiError and the response carried
 * a Retry-After header, the header value is copied into `details.retry_after_header`.
 */
export function unwrap<T>(res: RequestResult, parse: (body: unknown, status?: number) => T): T {
  try {
    return parse(res.body, res.status);
  } catch (error) {
    if (error instanceof ApiError && res.retryAfter !== null) {
      throw new ApiError({
        code: error.code,
        message: error.message,
        details: { ...error.details, retry_after_header: res.retryAfter },
        requestId: error.requestId,
        status: error.status,
      });
    }
    throw error;
  }
}

async function readJson(res: Response, signal: AbortSignal): Promise<unknown> {
  try {
    return await res.json();
  } catch (error) {
    // A timeout that fires while the body is streaming is a network failure: reject.
    if (signal.aborted) throw error;
    // Non-JSON body (e.g. an HTML 502 from a proxy): not a network error.
    return undefined;
  }
}

export function createHttpClient(
  baseUrl: string,
  fetchImpl: FetchImpl = fetch,
  options: HttpClientOptions = {},
): HttpClient {
  const root = baseUrl.replace(/\/+$/, '');
  const defaultTimeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  let adapter: AuthAdapter | null = null;

  /** One round trip. Abort becomes client.timeout and any other failure client.network. */
  async function send(req: HttpRequest, token: string | null): Promise<RequestResult> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), req.timeoutMs ?? defaultTimeoutMs);
    const headers: Record<string, string> = { Accept: 'application/json' };
    if (req.body !== undefined) headers['Content-Type'] = 'application/json';
    if (token) headers.Authorization = `Bearer ${token}`;
    try {
      const res = await fetchImpl(`${root}${req.path}`, {
        method: req.method,
        headers,
        body: req.body === undefined ? undefined : JSON.stringify(req.body),
        signal: controller.signal,
      });
      return {
        status: res.status,
        body: res.status === 204 ? undefined : await readJson(res, controller.signal),
        retryAfter: res.headers.get('Retry-After'),
      };
    } catch {
      const code: ClientErrorCode = controller.signal.aborted ? 'client.timeout' : 'client.network';
      throw new ApiError({ code, message: code });
    } finally {
      clearTimeout(timer);
    }
  }

  /** Runs the side effects the session layer owns for a final response. */
  async function settle(res: RequestResult): Promise<void> {
    const code = errorCode(res.body);
    if (res.status === 401 && code === 'auth.session_revoked') await adapter?.onSessionRevoked();
    if (res.status === 403 && code === 'legal.acceptance_required') adapter?.onLegalRequired();
  }

  return {
    setAuthAdapter(next) {
      adapter = next;
    },

    async request(req) {
      const auth = adapter;
      if (!req.auth || !auth) return send(req, null);
      // A refresh already running means the stored token is expired: wait instead of spending a 401 on it.
      await auth.pendingRefresh?.()?.catch(() => undefined);
      const sent = auth.getAccessToken();
      let res = await send(req, sent);
      if (res.status === 401 && errorCode(res.body) === 'auth.unauthenticated') {
        const current = auth.getAccessToken();
        if (current !== null && current !== sent) {
          // Another request already rotated the token while this one was in flight: reuse it, never rotate twice.
          res = await send(req, current);
        } else if (current !== null) {
          const outcome = await auth.refresh();
          if (outcome.kind === 'unavailable') throw new ApiError({ code: outcome.code, message: outcome.code });
          if (outcome.kind === 'rate_limited') {
            throw new ApiError({
              code: 'rate_limit.exceeded',
              message: 'refresh rate limited',
              details: { retry_after_seconds: outcome.retryAfterSeconds },
            });
          }
          // 'ended': the session layer already signed out, so hand back the 401.
          // 'refreshed': retry once; a second 401 is returned as is.
          if (outcome.kind === 'refreshed') res = await send(req, auth.getAccessToken());
        }
        // current === null: signed out meanwhile, hand back the 401.
      }
      await settle(res);
      return res;
    },

    async get(path, init) {
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), init?.timeoutMs ?? defaultTimeoutMs);
      try {
        const res = await fetchImpl(`${root}${path}`, {
          method: 'GET',
          headers: { Accept: 'application/json' },
          signal: controller.signal,
        });
        return { status: res.status, body: await readJson(res, controller.signal) };
      } finally {
        clearTimeout(timer);
      }
    },
  };
}

/** Probes /readyz. Never throws: every outcome maps to an ApiHealthState. */
export async function getApiHealth(client: HttpClient): Promise<ApiHealthState> {
  try {
    return toHealthState(await client.get('/readyz'));
  } catch {
    return toHealthState({ networkError: true });
  }
}
