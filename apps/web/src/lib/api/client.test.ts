import { ApiError, parseEnvelope } from '@mypark/contracts';
import { errorBody, successBody } from '@mypark/contracts/fixtures';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { z } from 'zod';
import { hardNavigate } from '@/lib/browser/navigate';
import { jsonResponse, stubFetch } from '@/testing/fetch';
import { apiFetch, handleSessionError, readCsrfToken } from './client';

vi.mock('@/lib/browser/navigate', () => ({ hardNavigate: vi.fn() }));

const parseOk = parseEnvelope(z.object({ ok: z.boolean() }));

function setCsrf(value: string | null) {
  document.cookie = value === null ? 'mp_csrf=; Max-Age=0; Path=/' : `mp_csrf=${value}; Path=/`;
}

function headersOf(mock: ReturnType<typeof stubFetch>): Headers {
  return new Headers(mock.mock.calls[0]![1]!.headers);
}

beforeEach(() => {
  setCsrf(null);
  window.history.pushState({}, '', '/');
});

describe('readCsrfToken', () => {
  test('reads mp_csrf among other cookies', () => {
    document.cookie = 'other=1; Path=/';
    setCsrf('token-1');
    expect(readCsrfToken()).toBe('token-1');
  });

  test('is empty when the cookie is absent', () => {
    expect(readCsrfToken()).toBe('');
  });
});

describe('apiFetch', () => {
  test('adds X-CSRF-Token on mutations, read from mp_csrf', async () => {
    setCsrf('token-1');
    const fetchMock = stubFetch(jsonResponse(200, successBody({ ok: true })));

    await expect(apiFetch('/api/v1/vehicles', parseOk, { method: 'POST', body: { a: 1 } })).resolves.toEqual({ ok: true });

    const [url, init] = fetchMock.mock.calls[0]!;
    expect(url).toBe('/api/v1/vehicles');
    expect(init).toMatchObject({ method: 'POST', body: '{"a":1}', credentials: 'same-origin', cache: 'no-store' });
    expect(headersOf(fetchMock).get('x-csrf-token')).toBe('token-1');
    expect(headersOf(fetchMock).get('content-type')).toBe('application/json');
  });

  test('sends no CSRF header on GET', async () => {
    setCsrf('token-1');
    const fetchMock = stubFetch(jsonResponse(200, successBody({ ok: true })));

    await apiFetch('/api/v1/me', parseOk);

    expect(headersOf(fetchMock).has('x-csrf-token')).toBe(false);
    expect(fetchMock.mock.calls[0]![1]!.method).toBe('GET');
  });

  test('a failed fetch becomes client.network', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')));

    await expect(apiFetch('/api/v1/me', parseOk)).rejects.toMatchObject({ name: 'ApiError', code: 'client.network' });
  });

  test('a non-envelope body becomes internal.unexpected', async () => {
    stubFetch(new Response('<html>bad gateway</html>', { status: 502 }));

    await expect(apiFetch('/api/v1/me', parseOk)).rejects.toMatchObject({ code: 'internal.unexpected', status: 502 });
  });

  test('an error envelope becomes an ApiError with its status and code', async () => {
    stubFetch(jsonResponse(409, errorBody('vehicle.plate_taken')));

    const error = await apiFetch('/api/v1/vehicles', parseOk, { method: 'POST', body: {} }).catch((e: unknown) => e);

    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({ code: 'vehicle.plate_taken', status: 409 });
  });

  test('a Retry-After header is copied into details.retry_after_header', async () => {
    stubFetch(jsonResponse(429, errorBody('rate_limit.exceeded'), { 'Retry-After': '30' }));

    const error = (await apiFetch('/api/auth/login', parseOk, { method: 'POST', body: {} }).catch((e: unknown) => e)) as ApiError;

    expect(error.code).toBe('rate_limit.exceeded');
    expect(error.details.retry_after_header).toBe('30');
  });
});

describe('handleSessionError', () => {
  const unauthenticated = new ApiError({ code: 'auth.unauthenticated', message: 'x', status: 401 });

  test('auth.unauthenticated redirects to /api/session/end with locale and next', () => {
    window.history.pushState({}, '', '/en/vehicles?x=1');
    const refresh = vi.fn();

    handleSessionError(unauthenticated, { refresh });

    const target = new URL(vi.mocked(hardNavigate).mock.calls[0]![0], 'http://localhost');
    expect(target.pathname).toBe('/api/session/end');
    expect(target.searchParams.get('locale')).toBe('en');
    expect(target.searchParams.get('next')).toBe('/en/vehicles?x=1');
    expect(refresh).not.toHaveBeenCalled();
  });

  test('auth.session_revoked redirects to /api/session/end', () => {
    window.history.pushState({}, '', '/es-DO/profile');

    handleSessionError(new ApiError({ code: 'auth.session_revoked', message: 'x', status: 401 }), { refresh: vi.fn() });

    expect(vi.mocked(hardNavigate).mock.calls[0]![0]).toMatch(/^\/api\/session\/end\?/);
  });

  test('auth.invalid_credentials stays on the form', () => {
    handleSessionError(new ApiError({ code: 'auth.invalid_credentials', message: 'x', status: 401 }), { refresh: vi.fn() });

    expect(hardNavigate).not.toHaveBeenCalled();
  });

  test('legal.acceptance_required calls router.refresh', () => {
    const refresh = vi.fn();

    handleSessionError(new ApiError({ code: 'legal.acceptance_required', message: 'x', status: 403 }), { refresh });

    expect(refresh).toHaveBeenCalledOnce();
    expect(hardNavigate).not.toHaveBeenCalled();
  });

  test('ignores errors that are not ApiError', () => {
    const refresh = vi.fn();

    handleSessionError(new Error('boom'), { refresh });

    expect(refresh).not.toHaveBeenCalled();
    expect(hardNavigate).not.toHaveBeenCalled();
  });
});
