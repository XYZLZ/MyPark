import { ApiError, parseNoContent } from '@mypark/contracts';
import { hardNavigate } from '@/lib/browser/navigate';
import { resolveLocale } from '@/i18n/routing';

const CSRF_COOKIE = 'mp_csrf';

type ApiInit = { method?: 'GET' | 'POST' | 'PATCH' | 'DELETE'; body?: unknown };

/** The CSRF token the BFF set for this browser, or an empty string. It is readable by script on purpose (spec 5.4). */
export function readCsrfToken(): string {
  for (const part of document.cookie.split(';')) {
    const [name, ...value] = part.trim().split('=');
    if (name === CSRF_COOKIE) {
      try {
        return decodeURIComponent(value.join('='));
      } catch {
        return '';
      }
    }
  }
  return '';
}

/** For 204 responses: `apiFetch('/api/v1/auth/verify', parseNoContentBody, { method: 'POST', body })`. */
export function parseNoContentBody(body: unknown, status?: number): void {
  parseNoContent(body, status ?? 0);
}

function withRetryAfter(error: unknown, response: Response): unknown {
  const header = response.headers.get('Retry-After');
  if (!(error instanceof ApiError) || header === null) return error;
  return new ApiError({
    code: error.code,
    message: error.message,
    details: { ...error.details, retry_after_header: header },
    requestId: error.requestId,
    status: error.status,
  });
}

/**
 * The only way the browser calls the app's own `/api` routes (the BFF, spec 5.1).
 * `path` includes the prefix, e.g. `/api/v1/vehicles` or `/api/auth/login`.
 * Failures arrive as an ApiError: `client.network` when the request never got an
 * answer, `internal.unexpected` for a body that is not an envelope, otherwise the
 * server's code. `ApiError.message` is never meant for display.
 */
export async function apiFetch<T>(
  path: string,
  parse: (body: unknown, status?: number) => T,
  init: ApiInit = {},
): Promise<T> {
  const { method = 'GET', body } = init;
  const headers = new Headers({ Accept: 'application/json' });
  if (body !== undefined) headers.set('Content-Type', 'application/json');
  if (method !== 'GET') headers.set('X-CSRF-Token', readCsrfToken());

  let response: Response;
  try {
    response = await fetch(path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      credentials: 'same-origin',
      cache: 'no-store',
    });
  } catch {
    throw new ApiError({ code: 'client.network', message: 'network failure' });
  }

  let parsed: unknown;
  try {
    const text = await response.text();
    parsed = text === '' ? undefined : JSON.parse(text);
  } catch {
    parsed = undefined;
  }

  try {
    return parse(parsed, response.status);
  } catch (error) {
    throw withRetryAfter(error, response);
  }
}

/** The codes that mean the session is gone. `auth.invalid_credentials` is also a 401, but it is a failed login, not an ended session. */
const SESSION_ENDED = new Set(['auth.unauthenticated', 'auth.session_revoked']);

/**
 * Cross-cutting reactions to API errors (spec 5.8): an ended session goes through
 * `/api/session/end`, which clears the cookies and lands on login; a missing
 * legal acceptance re-renders the server tree so the gate appears.
 */
export function handleSessionError(error: unknown, effects: { refresh(): void }): void {
  if (!(error instanceof ApiError)) return;

  if (SESSION_ENDED.has(error.code)) {
    const { pathname, search } = window.location;
    const params = new URLSearchParams({
      locale: resolveLocale(pathname.split('/')[1]),
      next: `${pathname}${search}`,
    });
    hardNavigate(`/api/session/end?${params}`);
    return;
  }
  if (error.code === 'legal.acceptance_required') effects.refresh();
}
