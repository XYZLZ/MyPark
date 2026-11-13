import type { RequestResult } from '@/services/httpClient';

/** A response as `http.request` resolves it, for stubbing at the HTTP client boundary. */
export function reply(status: number, body?: unknown, retryAfter: string | null = null): RequestResult {
  return { status, body, retryAfter };
}
