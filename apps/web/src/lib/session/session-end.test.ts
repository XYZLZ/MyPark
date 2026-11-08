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

  test('unauthenticated deletes only mp_access, sets the retry marker and returns to next', async () => {
    stubFetch(jsonResponse(401, errorBody('auth.unauthenticated')));
    const res = await handleSessionEnd(end({}, LIVE));
    expect(to(res).pathname).toBe('/es-DO/vehicles');
    expect(to(res).search).toBe('');
    const cookies = setCookies(res);
    expect(cookies.find((c) => c.startsWith('mp_access='))).toMatch(/^mp_access=;.*Max-Age=0/i);
    expect(cookies.find((c) => c.startsWith('mp_session_retry='))).toMatch(/^mp_session_retry=1;.*Max-Age=60.*HttpOnly/i);
    expect(cookies.join('\n')).not.toMatch(/mp_refresh/);
  });

  test('a second 401 with the marker goes to login, clears the marker and leaves mp_access and mp_refresh alone', async () => {
    stubFetch(jsonResponse(401, errorBody('auth.unauthenticated')));
    const res = await handleSessionEnd(end({}, `${LIVE}; mp_session_retry=1`));
    expect(to(res).pathname).toBe('/es-DO/login');
    expect(to(res).searchParams.get('reason')).toBe('session_expired');
    const cookies = setCookies(res).join('\n');
    expect(cookies).toMatch(/mp_session_retry=;.*Max-Age=0/i);
    expect(cookies).not.toMatch(/mp_access|mp_refresh/);
  });

  test.each([
    ['an unknown code', () => jsonResponse(401, errorBody('auth.something_new'))],
    ['a malformed body', () => new Response('<html>nope</html>', { status: 401 })],
  ])('a 401 with %s retries once, leaves mp_access alone, then gives up', async (_name, answer) => {
    stubFetch(answer());
    const first = await handleSessionEnd(end({}, LIVE));
    expect(to(first).pathname).toBe('/es-DO/vehicles');
    expect(setCookies(first).join('\n')).toMatch(/mp_session_retry=1/);
    expect(setCookies(first).join('\n')).not.toMatch(/mp_access|mp_refresh/);

    stubFetch(answer());
    const second = await handleSessionEnd(end({}, `${LIVE}; mp_session_retry=1`));
    expect(to(second).pathname).toBe('/es-DO/login');
    expect(setCookies(second).join('\n')).not.toMatch(/mp_access|mp_refresh/);
  });

  test('session_revoked with the marker still clears both cookies and the marker', async () => {
    stubFetch(jsonResponse(401, errorBody('auth.session_revoked')));
    const res = await handleSessionEnd(end({}, `${LIVE}; mp_session_retry=1`));
    const cookies = setCookies(res).join('\n');
    expect(cookies).toMatch(/mp_access=;/);
    expect(cookies).toMatch(/mp_refresh=;/);
    expect(cookies).toMatch(/mp_session_retry=;/);
  });

  test('a 200 with the marker returns to next and drops the marker', async () => {
    stubFetch(jsonResponse(200, successBody(userFixture)));
    const res = await handleSessionEnd(end({}, `${LIVE}; mp_session_retry=1`));
    expect(to(res).pathname).toBe('/es-DO/vehicles');
    expect(setCookies(res).join('\n')).toMatch(/mp_session_retry=;/);
    expect(setCookies(res).join('\n')).not.toMatch(/mp_access|mp_refresh/);
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


  test('a link from another site is served like any other: the API sees only the bearer, and nothing is cleared', async () => {
    const fetchMock = stubFetch(jsonResponse(200, successBody(userFixture)));
    const request = new NextRequest(end({}, LIVE), {
      headers: { cookie: LIVE, origin: 'https://evil.example', referer: 'https://evil.example/', 'sec-fetch-site': 'cross-site' },
    });
    const res = await handleSessionEnd(request);

    expect(to(res).pathname).toBe('/es-DO/vehicles');
    expect(setCookies(res)).toEqual([]);
    const sent = new Headers(fetchMock.mock.calls[0]![1]!.headers);
    expect(sent.get('authorization')).toBe('Bearer a');
    expect(sent.get('cookie')).toBeNull();
    expect(sent.get('origin')).toBeNull();
  });
});
