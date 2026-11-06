import { toHealthState, type ApiHealthState } from '@mypark/contracts';

export const DEFAULT_TIMEOUT_MS = 3000;

export type HttpResponse = { status: number; body: unknown };

export type HttpClient = {
  /** Resolves with any HTTP status. Rejects only on network failure or timeout. */
  get(path: string, init?: { timeoutMs?: number }): Promise<HttpResponse>;
};

type FetchImpl = (input: string, init?: RequestInit) => Promise<Response>;

export type HttpClientOptions = {
  /** Default per-request timeout. A request can still override it. */
  timeoutMs?: number;
};

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

  return {
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
