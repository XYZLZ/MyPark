// @vitest-environment node
import { errorBody, successBody, userFixture } from '@mypark/contracts/fixtures';
import { NextRequest } from 'next/server';
import { describe, expect, test } from 'vitest';
import { jsonResponse, stubFetch } from '@/testing/fetch';
import { handleSessionEnd } from './session-end';

const end = (query: Record<string, string>, cookie?: string) => {
  const params = new URLSearchParams({ locale: 'es-DO', next: '/es-DO/vehicles', ...query });
  return new NextRequest(`http://localhost:3000/api/session/end?${params}`, { headers: cookie ? { cookie } : {} });
};

const LIVE = 'mp_access=a; mp_refresh=r';
const to = (res: Response) => new URL(res.headers.get('location') ?? '', 'http://localhost:3000');
const setCookies = (res: Response) => res.headers.getSetCookie();

describe('handleSessionEnd', () => {
  test('no cookies redirect to login with session_expired', async () => {
    const fetchMock = stubFetch(jsonResponse(200, successBody(userFixture)));
    const res = await handleSessionEnd(end({ locale: 'en' }));
    expect(to(res).pathname + to(res).search).toBe('/en/login?reason=session_expired');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test('session_revoked clears both cookies and redirects to login', async () => {
    stubFetch(jsonResponse(401, errorBody('auth.session_revoked')));
    const res = await handleSessionEnd(end({}, LIVE));
    expect(to(res).pathname).toBe('/es-DO/login');
    expect(to(res).searchParams.get('reason')).toBe('session_expired');
    const cookies = setCookies(res).join('\n');
    expect(cookies).toMatch(/mp_access=;/);
    expect(cookies).toMatch(/mp_refresh=;/);
  });

  test('unauthenticated deletes only mp_access and retries next once', async () => {
    stubFetch(jsonResponse(401, errorBody('auth.unauthenticated')));
    const res = await handleSessionEnd(end({}, LIVE));
    expect(to(res).pathname).toBe('/es-DO/vehicles');
    expect(to(res).searchParams.get('retried')).toBe('1');
    const cookies = setCookies(res).join('\n');
    expect(cookies).toMatch(/mp_access=;/);
    expect(cookies).not.toMatch(/mp_refresh/);
  });

  test('a second unauthenticated with retried=1 goes to login without clearing mp_refresh', async () => {
    stubFetch(jsonResponse(401, errorBody('auth.unauthenticated')));
    const res = await handleSessionEnd(end({ next: '/es-DO/vehicles?retried=1' }, LIVE));
    expect(to(res).pathname).toBe('/es-DO/login');
    expect(setCookies(res).join('\n')).not.toMatch(/mp_refresh/);
  });

  test('a lapsed access cookie never clears the refresh cookie', async () => {
    const fetchMock = stubFetch(jsonResponse(200, successBody(userFixture)));
    const res = await handleSessionEnd(end({}, 'mp_refresh=r'));
    expect(to(res).pathname).toBe('/es-DO/vehicles');
    expect(setCookies(res)).toEqual([]);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test.each([200, 403, 500])('upstream %s redirects to next, cookies untouched', async (status) => {
    stubFetch(jsonResponse(status, status === 200 ? successBody(userFixture) : errorBody('internal.unexpected')));
    const res = await handleSessionEnd(end({}, LIVE));
    expect(to(res).pathname).toBe('/es-DO/vehicles');
    expect(to(res).searchParams.has('retried')).toBe(false);
    expect(setCookies(res)).toEqual([]);
  });

  test('an unreachable API redirects to next with cookies untouched', async () => {
    stubFetch(() => Promise.reject(new TypeError('fetch failed')));
    const res = await handleSessionEnd(end({}, LIVE));
    expect(to(res).pathname).toBe('/es-DO/vehicles');
    expect(setCookies(res)).toEqual([]);
  });

  test.each(['//evil.example', '/\\evil.example', 'https://evil.example', '/%2F%2Fevil.example'])('unsafe next %s is replaced', async (next) => {
    stubFetch(jsonResponse(200, successBody(userFixture)));
    const res = await handleSessionEnd(end({ next }, LIVE));
    expect(to(res).origin).toBe('http://localhost:3000');
    expect(to(res).pathname).toBe('/es-DO');
  });

  test('session end works for a GET from another site without logging out a valid session', async () => {
    stubFetch(jsonResponse(200, successBody(userFixture)));
    const request = new NextRequest(end({}, LIVE), { headers: { cookie: LIVE, origin: 'https://evil.example', 'sec-fetch-site': 'cross-site' } });
    const res = await handleSessionEnd(request);
    expect(to(res).pathname).toBe('/es-DO/vehicles');
    expect(setCookies(res)).toEqual([]);
  });
});
