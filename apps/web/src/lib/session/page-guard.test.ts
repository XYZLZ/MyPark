// @vitest-environment node
import { NextRequest } from 'next/server';
import { describe, expect, test } from 'vitest';
import { guardPublicAndProtected } from './page-guard';

const get = (path: string, cookie?: string) =>
  new NextRequest(`http://localhost:3000${path}`, { headers: cookie ? { cookie } : {} });

const location = (res: ReturnType<typeof guardPublicAndProtected>) => {
  const header = res?.headers.get('location');
  return header ? new URL(header) : null;
};

describe('guardPublicAndProtected', () => {
  test.each([
    ['/es-DO/vehicles', '/es-DO/login'],
    ['/en/profile', '/en/login'],
    ['/es-DO/admin', '/es-DO/admin/login'],
    ['/en/vehicles/new', '/en/login'],
  ])('no cookies on %s redirect to %s with next', (path, login) => {
    const target = location(guardPublicAndProtected(get(`${path}?x=1`)));
    expect(target?.pathname).toBe(login);
    expect(target?.searchParams.get('next')).toBe(`${path}?x=1`);
  });

  test('admin login itself is not guarded', () => {
    expect(guardPublicAndProtected(get('/es-DO/admin/login'))).toBeNull();
    expect(guardPublicAndProtected(get('/es-DO/login'))).toBeNull();
  });

  test.each(['/es-DO/login', '/en/register', '/es-DO/admin/login'])('live mp_access on %s goes to /vehicles', (path) => {
    const locale = path.split('/')[1];
    const target = location(guardPublicAndProtected(get(path, 'mp_access=a; mp_refresh=r')));
    expect(target?.pathname).toBe(`/${locale}/vehicles`);
  });

  test('matches with and without a locale prefix', () => {
    expect(location(guardPublicAndProtected(get('/vehicles')))?.pathname).toBe('/es-DO/login');
    expect(location(guardPublicAndProtected(get('/admin')))?.pathname).toBe('/es-DO/admin/login');
    expect(location(guardPublicAndProtected(get('/login', 'mp_access=a')))?.pathname).toBe('/es-DO/vehicles');
  });

  test('a refresh cookie alone does not redirect away from protected pages', () => {
    expect(guardPublicAndProtected(get('/es-DO/vehicles', 'mp_refresh=r'))).toBeNull();
    expect(guardPublicAndProtected(get('/es-DO/admin', 'mp_refresh=r'))).toBeNull();
    // ...nor away from login: only a live access cookie counts.
    expect(guardPublicAndProtected(get('/es-DO/login', 'mp_refresh=r'))).toBeNull();
  });

  test('an empty cookie value counts as absent', () => {
    expect(location(guardPublicAndProtected(get('/es-DO/vehicles', 'mp_access=; mp_refresh=')))?.pathname).toBe('/es-DO/login');
  });

  test('other pages pass through', () => {
    expect(guardPublicAndProtected(get('/es-DO'))).toBeNull();
    expect(guardPublicAndProtected(get('/es-DO/vehiclesx'))).toBeNull();
    expect(guardPublicAndProtected(get('/es-DO/legal/terms'))).toBeNull();
  });
});
