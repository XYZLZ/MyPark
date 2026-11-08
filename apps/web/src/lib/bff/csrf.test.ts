// @vitest-environment node
import { NextRequest } from 'next/server';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { verifyMutation } from './csrf';

beforeEach(() => {
  vi.stubEnv('WEB_ALLOWED_ORIGINS', 'http://localhost:3000,http://127.0.0.1:3000');
});

function mutation(headers: Record<string, string>) {
  return new NextRequest('http://localhost:3000/api/v1/vehicles', { method: 'POST', headers });
}

const good = { origin: 'http://localhost:3000', cookie: 'mp_csrf=token-1', 'x-csrf-token': 'token-1' };

async function expectForbidden(request: NextRequest) {
  const res = verifyMutation(request);
  expect(res?.status).toBe(403);
  expect((await res!.json()).error.code).toBe('auth.forbidden');
}

describe('verifyMutation', () => {
  test('rejects a missing Origin', async () => {
    await expectForbidden(mutation({ cookie: good.cookie, 'x-csrf-token': good['x-csrf-token'] }));
  });

  test('rejects an Origin not in WEB_ALLOWED_ORIGINS', async () => {
    await expectForbidden(mutation({ ...good, origin: 'http://evil.example' }));
    await expectForbidden(mutation({ ...good, origin: 'http://localhost:3000.evil.example' }));
    await expectForbidden(mutation({ ...good, origin: 'http://localhost:3000/' }));
  });

  test('rejects a missing token', async () => {
    await expectForbidden(mutation({ origin: good.origin, cookie: good.cookie }));
    await expectForbidden(mutation({ origin: good.origin, 'x-csrf-token': 'token-1' }));
  });

  test('rejects a token that does not equal the cookie', async () => {
    await expectForbidden(mutation({ ...good, 'x-csrf-token': 'token-2' }));
    await expectForbidden(mutation({ ...good, 'x-csrf-token': 'token-12' }));
  });

  test('rejects an empty cookie and empty header', async () => {
    await expectForbidden(mutation({ origin: good.origin, cookie: 'mp_csrf=', 'x-csrf-token': '' }));
  });

  test('accepts an exact Origin and matching token', () => {
    expect(verifyMutation(mutation(good))).toBeNull();
    expect(verifyMutation(mutation({ ...good, origin: 'http://127.0.0.1:3000' }))).toBeNull();
  });

  test('allows the local dev origins when WEB_ALLOWED_ORIGINS is unset', () => {
    vi.stubEnv('WEB_ALLOWED_ORIGINS', '');
    expect(verifyMutation(mutation(good))).toBeNull();
  });
});
