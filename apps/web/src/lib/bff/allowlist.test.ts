import { describe, expect, test } from 'vitest';
import { ARGON2_TIMEOUT_MS, DEFAULT_TIMEOUT_MS, matchBffRoute } from './allowlist';

const ID = '7d8e9f0a-1b2c-4d3e-8f4a-5b6c7d8e9f0a';

// Spec section 5.5, minus the two dedicated handlers (login, session/end).
const rows: Array<[string, string[], string, 'public' | 'bearer']> = [
  ['POST', ['auth', 'register'], '/v1/auth/register', 'public'],
  ['POST', ['auth', 'verify'], '/v1/auth/verify', 'public'],
  ['POST', ['auth', 'verify', 'resend'], '/v1/auth/verify/resend', 'public'],
  ['POST', ['auth', 'password', 'forgot'], '/v1/auth/password/forgot', 'public'],
  ['POST', ['auth', 'password', 'reset'], '/v1/auth/password/reset', 'public'],
  ['POST', ['auth', 'invitations', 'accept'], '/v1/auth/invitations/accept', 'public'],
  ['POST', ['auth', 'logout'], '/v1/auth/logout', 'bearer'],
  ['POST', ['auth', 'logout-all'], '/v1/auth/logout-all', 'bearer'],
  ['GET', ['legal', 'current'], '/v1/legal/current', 'public'],
  ['GET', ['me'], '/v1/me', 'bearer'],
  ['PATCH', ['me'], '/v1/me', 'bearer'],
  ['DELETE', ['me'], '/v1/me', 'bearer'],
  ['POST', ['me', 'legal-acceptance'], '/v1/me/legal-acceptance', 'bearer'],
  ['GET', ['vehicles'], '/v1/vehicles', 'bearer'],
  ['POST', ['vehicles'], '/v1/vehicles', 'bearer'],
  ['GET', ['vehicles', ID], `/v1/vehicles/${ID}`, 'bearer'],
  ['PATCH', ['vehicles', ID], `/v1/vehicles/${ID}`, 'bearer'],
  ['DELETE', ['vehicles', ID], `/v1/vehicles/${ID}`, 'bearer'],
];

describe('matchBffRoute', () => {
  test.each(rows)('allows %s %j', (method, segments, upstreamPath, auth) => {
    expect(matchBffRoute(method, segments)).toMatchObject({ upstreamPath, auth });
  });

  test('denies unknown paths', () => {
    expect(matchBffRoute('GET', ['admin', 'admins'])).toBeNull();
    expect(matchBffRoute('GET', [])).toBeNull();
    expect(matchBffRoute('GET', ['me', 'extra'])).toBeNull();
  });

  test('denies login and refresh through the catch-all', () => {
    expect(matchBffRoute('POST', ['auth', 'login'])).toBeNull();
    expect(matchBffRoute('POST', ['auth', 'refresh'])).toBeNull();
  });

  test('denies a method mismatch', () => {
    expect(matchBffRoute('PUT', ['vehicles'])).toBeNull();
    expect(matchBffRoute('DELETE', ['auth', 'logout'])).toBeNull();
    expect(matchBffRoute('POST', ['legal', 'current'])).toBeNull();
    expect(matchBffRoute('DELETE', ['vehicles'])).toBeNull();
  });

  test('denies traversal, encoded separators and non-uuid ids', () => {
    expect(matchBffRoute('GET', ['vehicles', '..%2Fadmin'])).toBeNull();
    expect(matchBffRoute('GET', ['vehicles', '../admin'])).toBeNull();
    expect(matchBffRoute('GET', ['vehicles', '..', 'admin'])).toBeNull();
    expect(matchBffRoute('GET', ['vehicles', 'not-a-uuid'])).toBeNull();
    expect(matchBffRoute('GET', ['vehicles', `${ID}%2F..`])).toBeNull();
    expect(matchBffRoute('GET', ['vehicles', ID, 'extra'])).toBeNull();
    expect(matchBffRoute('GET', ['vehicles', ''])).toBeNull();
  });

  test('uses 20 s for argon2 routes and 5 s otherwise', () => {
    expect(ARGON2_TIMEOUT_MS).toBe(20_000);
    expect(DEFAULT_TIMEOUT_MS).toBe(5_000);
    const argon2: Array<[string, string[]]> = [
      ['POST', ['auth', 'register']],
      ['POST', ['auth', 'password', 'reset']],
      ['POST', ['auth', 'invitations', 'accept']],
      ['DELETE', ['me']],
    ];
    for (const [method, segments] of argon2) {
      expect(matchBffRoute(method, segments)?.timeoutMs, `${method} ${segments.join('/')}`).toBe(20_000);
    }
    for (const [method, segments] of rows) {
      if (argon2.some(([m, s]) => m === method && s.join('/') === segments.join('/'))) continue;
      expect(matchBffRoute(method, segments)?.timeoutMs, `${method} ${segments.join('/')}`).toBe(5_000);
    }
  });

  test('marks what happens to cookies afterwards', () => {
    expect(matchBffRoute('POST', ['auth', 'logout'])?.after).toBe('clear-always');
    expect(matchBffRoute('POST', ['auth', 'logout-all'])?.after).toBe('clear-always');
    expect(matchBffRoute('DELETE', ['me'])?.after).toBe('clear-on-204');
    expect(matchBffRoute('GET', ['me'])?.after).toBe('none');
  });

  test('only logout-all signals an unconfirmed revoke', () => {
    expect(matchBffRoute('POST', ['auth', 'logout-all'])?.signalUnconfirmed).toBe(true);
    expect(matchBffRoute('POST', ['auth', 'logout'])?.signalUnconfirmed).toBe(false);
    expect(matchBffRoute('DELETE', ['me'])?.signalUnconfirmed).toBe(false);
  });
});
