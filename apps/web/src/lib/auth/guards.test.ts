// @vitest-environment node
import { ApiError } from '@mypark/contracts';
import { adminFixture, legalCurrentFixture, userFixture } from '@mypark/contracts/fixtures';
import { cookies } from 'next/headers';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { getLegalCurrent, getMe } from '@/lib/api/server';
import { requireAdmin, requireDriver } from './guards';

vi.mock('next/headers', () => ({ cookies: vi.fn() }));
vi.mock('next/navigation', () => ({
  redirect: vi.fn((url: string) => {
    throw new Error(`redirect:${url}`);
  }),
  forbidden: vi.fn(() => {
    throw new Error('forbidden');
  }),
}));
vi.mock('@/lib/api/server', () => ({ getMe: vi.fn(), getLegalCurrent: vi.fn() }));

type Jar = Record<string, string | undefined>;

/** `cookies()` the way Next returns it: a cleared cookie is still listed, with no value. */
function withCookies(jar: Jar) {
  vi.mocked(cookies).mockResolvedValue({
    get: (name: string) => (name in jar ? { name, value: jar[name] } : undefined),
    has: (name: string) => name in jar,
  } as unknown as Awaited<ReturnType<typeof cookies>>);
}

const LIVE = { mp_access: 'a', mp_refresh: 'r' };
const apiError = (code: string, status: number) => new ApiError({ code, message: code, status });
const outdated = (user = userFixture) => ({
  ...user,
  legal: { ...user.legal, terms_version: '2020-01-01' },
});

beforeEach(() => {
  withCookies(LIVE);
  vi.mocked(getMe).mockResolvedValue(userFixture);
  vi.mocked(getLegalCurrent).mockResolvedValue(legalCurrentFixture);
});

describe('requireDriver', () => {
  test('no cookies redirects a driver to login', async () => {
    withCookies({});
    await expect(requireDriver('es-DO')).rejects.toThrow(`redirect:/es-DO/login?next=${encodeURIComponent('/es-DO/vehicles')}`);
    expect(getMe).not.toHaveBeenCalled();
  });

  test('a cleared cookie counts as absent', async () => {
    withCookies({ mp_access: undefined, mp_refresh: '' });
    await expect(requireDriver('en')).rejects.toThrow('redirect:/en/login');
  });

  test('access absent, refresh present is unavailable', async () => {
    withCookies({ mp_refresh: 'r' });
    expect(await requireDriver('es-DO')).toEqual({ state: 'unavailable' });
    expect(getMe).not.toHaveBeenCalled();
  });

  test('401 redirects to /api/session/end', async () => {
    vi.mocked(getMe).mockRejectedValue(apiError('auth.unauthenticated', 401));
    const params = new URLSearchParams({ locale: 'en', next: '/en/vehicles' });
    await expect(requireDriver('en')).rejects.toThrow(`redirect:/api/session/end?${params}`);
  });

  test('403 account_suspended is suspended', async () => {
    vi.mocked(getMe).mockRejectedValue(apiError('auth.account_suspended', 403));
    expect(await requireDriver('es-DO')).toEqual({ state: 'suspended' });
  });

  test('admin at driver area is redirected to /admin', async () => {
    vi.mocked(getMe).mockResolvedValue(adminFixture);
    await expect(requireDriver('en')).rejects.toThrow('redirect:/en/admin');
  });

  test('legal gate renders when a version differs', async () => {
    vi.mocked(getMe).mockResolvedValue(outdated());
    expect(await requireDriver('es-DO')).toMatchObject({ state: 'legal', legal: legalCurrentFixture });

    const privacy = { ...userFixture, legal: { ...userFixture.legal, privacy_version: null } };
    vi.mocked(getMe).mockResolvedValue(privacy);
    expect(await requireDriver('es-DO')).toMatchObject({ state: 'legal' });
  });

  test('legal fetch failure does not skip the gate', async () => {
    vi.mocked(getMe).mockResolvedValue(outdated());
    vi.mocked(getLegalCurrent).mockRejectedValue(apiError('internal.unexpected', 502));
    expect(await requireDriver('es-DO')).toEqual({ state: 'unavailable' });
  });

  test('ready when both versions match', async () => {
    expect(await requireDriver('es-DO')).toEqual({ state: 'ready', user: userFixture });
  });

  test('getMe failure with 5xx rethrows for error.tsx', async () => {
    const failure = apiError('internal.unexpected', 502);
    vi.mocked(getMe).mockRejectedValue(failure);
    await expect(requireDriver('es-DO')).rejects.toBe(failure);
  });
});

describe('requireAdmin', () => {
  beforeEach(() => {
    vi.mocked(getMe).mockResolvedValue(adminFixture);
  });

  test('no cookies redirects to the admin login', async () => {
    withCookies({});
    await expect(requireAdmin('es-DO')).rejects.toThrow(`redirect:/es-DO/admin/login?next=${encodeURIComponent('/es-DO/admin')}`);
  });

  test('driver at admin area gets forbidden()', async () => {
    vi.mocked(getMe).mockResolvedValue(userFixture);
    await expect(requireAdmin('es-DO')).rejects.toThrow('forbidden');
  });

  test('admin area is exempt from the legal gate', async () => {
    vi.mocked(getMe).mockResolvedValue(outdated(adminFixture));
    expect(await requireAdmin('es-DO')).toMatchObject({ state: 'ready' });
    expect(getLegalCurrent).not.toHaveBeenCalled();
  });

  test('401 goes to session end with the admin home as next', async () => {
    vi.mocked(getMe).mockRejectedValue(apiError('auth.session_revoked', 401));
    const params = new URLSearchParams({ locale: 'es-DO', next: '/es-DO/admin' });
    await expect(requireAdmin('es-DO')).rejects.toThrow(`redirect:/api/session/end?${params}`);
  });

  test('suspended and unavailable states', async () => {
    vi.mocked(getMe).mockRejectedValue(apiError('auth.account_suspended', 403));
    expect(await requireAdmin('es-DO')).toEqual({ state: 'suspended' });
    withCookies({ mp_refresh: 'r' });
    expect(await requireAdmin('es-DO')).toEqual({ state: 'unavailable' });
  });
});
