import { NextRequest } from 'next/server';

export const ORIGIN = 'http://localhost:3000';
export const CSRF = 'csrf-token-1';

type Options = {
  method?: string;
  cookies?: Record<string, string>;
  body?: unknown;
  /** Sent as is when it is a string, JSON-encoded otherwise. */
  rawBody?: string;
  origin?: string | null;
  csrf?: string | null;
  headers?: Record<string, string>;
};

/** A browser request to a BFF endpoint: same-origin, with the double-submit CSRF pair unless overridden. */
export function bffRequest(path: string, options: Options = {}): NextRequest {
  const { method = 'GET', body, rawBody, headers = {} } = options;
  const cookies = { mp_csrf: CSRF, ...options.cookies };
  const origin = options.origin === undefined ? ORIGIN : options.origin;
  const csrf = options.csrf === undefined ? CSRF : options.csrf;

  const init: Record<string, string> = { ...headers };
  const cookie = Object.entries(cookies)
    .map(([name, value]) => `${name}=${value}`)
    .join('; ');
  if (cookie) init.cookie = cookie;
  if (origin !== null) init.origin = origin;
  if (csrf !== null) init['x-csrf-token'] = csrf;

  const payload = rawBody ?? (body === undefined ? undefined : JSON.stringify(body));
  if (payload !== undefined) init['content-type'] ??= 'application/json';
  return new NextRequest(`${ORIGIN}${path}`, { method, headers: init, body: payload });
}
