import { vi } from 'vitest';

/** A JSON response, or an empty body when `body` is undefined (204). */
export function jsonResponse(status: number, body?: unknown, headers: Record<string, string> = {}): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json', ...headers },
  });
}

/**
 * Replaces `fetch` at the boundary. Each call takes the next response, in order;
 * the last one repeats. A function receives the request and may return a promise.
 */
export function stubFetch(...responses: Array<Response | ((url: string, init: RequestInit) => Response | Promise<Response>)>) {
  let index = 0;
  const mock = vi.fn(async (url: string, init: RequestInit = {}) => {
    const next = responses[Math.min(index++, responses.length - 1)]!;
    return typeof next === 'function' ? next(url, init) : next.clone();
  });
  vi.stubGlobal('fetch', mock);
  return mock;
}

/** The JSON body of the n-th call. */
export function sentBody(mock: ReturnType<typeof stubFetch>, call = 0): Record<string, unknown> {
  return JSON.parse(String(mock.mock.calls[call]![1]!.body)) as Record<string, unknown>;
}
