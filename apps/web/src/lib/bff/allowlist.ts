export type BffRoute = {
  upstreamPath: string;
  auth: 'public' | 'bearer';
  timeoutMs: number;
  /** What happens to the session cookies after the upstream answers. */
  after: 'none' | 'clear-always' | 'clear-on-204';
  /** Answer the "not confirmed" signal of `./logout-all.ts` unless Go answered 2xx. Logout-all only. */
  signalUnconfirmed: boolean;
};

export const ARGON2_TIMEOUT_MS = 20_000;
export const DEFAULT_TIMEOUT_MS = 5_000;

const ID = ':id';
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export type Entry = {
  method: string;
  path: string;
  auth: BffRoute['auth'];
  timeoutMs?: number;
  after?: BffRoute['after'];
  signalUnconfirmed?: boolean;
};

// Spec 5.5. Paths are relative to /api/v1. Login and session/end have their own handlers.
/** The allow-list itself, exported so tests can iterate every route. */
export const BFF_ENTRIES: readonly Entry[] = [
  { method: 'POST', path: 'auth/register', auth: 'public', timeoutMs: ARGON2_TIMEOUT_MS },
  { method: 'POST', path: 'auth/verify', auth: 'public' },
  { method: 'POST', path: 'auth/verify/resend', auth: 'public' },
  { method: 'POST', path: 'auth/password/forgot', auth: 'public' },
  { method: 'POST', path: 'auth/password/reset', auth: 'public', timeoutMs: ARGON2_TIMEOUT_MS },
  { method: 'POST', path: 'auth/invitations/accept', auth: 'public', timeoutMs: ARGON2_TIMEOUT_MS },
  { method: 'POST', path: 'auth/logout', auth: 'bearer', after: 'clear-always' },
  { method: 'POST', path: 'auth/logout-all', auth: 'bearer', after: 'clear-always', signalUnconfirmed: true },
  { method: 'GET', path: 'legal/current', auth: 'public' },
  { method: 'GET', path: 'me', auth: 'bearer' },
  { method: 'PATCH', path: 'me', auth: 'bearer' },
  { method: 'DELETE', path: 'me', auth: 'bearer', timeoutMs: ARGON2_TIMEOUT_MS, after: 'clear-on-204' },
  { method: 'POST', path: 'me/legal-acceptance', auth: 'bearer' },
  { method: 'GET', path: 'vehicles', auth: 'bearer' },
  { method: 'POST', path: 'vehicles', auth: 'bearer' },
  { method: 'GET', path: `vehicles/${ID}`, auth: 'bearer' },
  { method: 'PATCH', path: `vehicles/${ID}`, auth: 'bearer' },
  { method: 'DELETE', path: `vehicles/${ID}`, auth: 'bearer' },
];

/**
 * Finds the allow-list entry for a request, or null. A literal segment must
 * equal the pattern exactly, and an id must be a UUID, so no segment can carry
 * a separator, a dot-dot or anything else that reshapes the upstream path.
 */
export function matchBffRoute(method: string, segments: string[]): BffRoute | null {
  for (const entry of BFF_ENTRIES) {
    if (entry.method !== method) continue;
    const pattern = entry.path.split('/');
    if (pattern.length !== segments.length) continue;
    if (!pattern.every((part, i) => (part === ID ? UUID.test(segments[i]!) : part === segments[i]))) continue;
    return {
      upstreamPath: `/v1/${segments.join('/')}`,
      auth: entry.auth,
      timeoutMs: entry.timeoutMs ?? DEFAULT_TIMEOUT_MS,
      after: entry.after ?? 'none',
      signalUnconfirmed: entry.signalUnconfirmed ?? false,
    };
  }
  return null;
}
