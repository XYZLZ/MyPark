import 'server-only';
import {
  ApiError,
  legalCurrentSchema,
  parseEnvelope,
  userSchema,
  type LegalCurrent,
  type User,
} from '@mypark/contracts';
import { cookies, headers } from 'next/headers';
import { cache } from 'react';
import { DEFAULT_TIMEOUT_MS } from '@/lib/bff/allowlist';
import { apiUrl } from '@/lib/bff/upstream';
import { ACCESS_COOKIE, REFRESH_COOKIE } from '@/lib/session/cookies';

type ServerFetchInit = { method?: string; body?: unknown; timeoutMs?: number; auth?: boolean };

/**
 * The only way a Server Component calls Go (spec 5.1). It reads the bearer from
 * the cookies the proxy has already refreshed, never from a client value.
 * Failures arrive as an ApiError with a code and a status; `message` is never shown.
 */
export async function serverFetch<T>(
  path: string,
  parse: (body: unknown, status?: number) => T,
  init: ServerFetchInit = {},
): Promise<T> {
  const { method = 'GET', body, timeoutMs = DEFAULT_TIMEOUT_MS, auth = true } = init;
  const [cookieStore, incoming] = await Promise.all([cookies(), headers()]);

  const requestHeaders: Record<string, string> = { Accept: 'application/json' };
  const forwarded = incoming.get('x-forwarded-for');
  if (forwarded) requestHeaders['X-Forwarded-For'] = forwarded;

  if (auth) {
    const access = cookieStore.get(ACCESS_COOKIE)?.value;
    if (!access) {
      // Access absent with a refresh cookie means the refresh failed transiently (spec 5.3).
      const transient = cookieStore.get(REFRESH_COOKIE) !== undefined;
      throw new ApiError({
        code: transient ? 'internal.unexpected' : 'auth.unauthenticated',
        message: 'no access token',
        status: transient ? 503 : 401,
      });
    }
    requestHeaders.Authorization = `Bearer ${access}`;
  }
  if (body !== undefined) requestHeaders['Content-Type'] = 'application/json';

  let response: Response;
  try {
    response = await fetch(apiUrl(path), {
      method,
      headers: requestHeaders,
      body: body === undefined ? undefined : JSON.stringify(body),
      cache: 'no-store',
      signal: AbortSignal.timeout(timeoutMs),
    });
  } catch {
    throw new ApiError({ code: 'internal.unexpected', message: 'upstream unreachable', status: 502 });
  }

  const text = await response.text();
  let parsed: unknown;
  try {
    parsed = text === '' ? undefined : JSON.parse(text);
  } catch {
    parsed = undefined;
  }
  return parse(parsed, response.status);
}

/** The signed-in user, once per render. */
export const getMe = cache(async (): Promise<User> => serverFetch('/v1/me', parseEnvelope(userSchema)));

export const getLegalCurrent = cache(
  async (): Promise<LegalCurrent> => serverFetch('/v1/legal/current', parseEnvelope(legalCurrentSchema), { auth: false }),
);
