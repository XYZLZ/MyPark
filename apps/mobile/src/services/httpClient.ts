import { toHealthState, type ApiHealthState } from '@mypark/contracts';

export const DEFAULT_TIMEOUT_MS = 3000;

export type HttpResponse = { status: number; body: unknown };

export type HttpClient = {
  /** Resolves with any HTTP status. Rejects only on network failure or timeout. */
  get(path: string, init?: { timeoutMs?: number }): Promise<HttpResponse>;
};

type FetchImpl = (input: string, init?: RequestInit) => Promise<Response>;

async function readJson(res: Response): Promise<unknown> {
  try {
    return await res.json();
  } catch {
    // Non-JSON body (e.g. an HTML 502 from a proxy): not a network error.
    return undefined;
  }
}

export function createHttpClient(baseUrl: string, fetchImpl: FetchImpl = fetch): HttpClient {
  const root = baseUrl.replace(/\/+$/, '');

  return {
    async get(path, init) {
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), init?.timeoutMs ?? DEFAULT_TIMEOUT_MS);
      try {
        const res = await fetchImpl(`${root}${path}`, {
          method: 'GET',
          headers: { Accept: 'application/json' },
          signal: controller.signal,
        });
        return { status: res.status, body: await readJson(res) };
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
