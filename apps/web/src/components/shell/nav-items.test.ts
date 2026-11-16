import { describe, expect, test } from 'vitest';
import { currentHref, navItems } from './nav-items';

describe('navItems', () => {
  test('navItems per variant', () => {
    expect(navItems('guest').map((i) => i.key)).toEqual(['home']);
    expect(navItems('driver').map((i) => i.key)).toEqual(['home', 'vehicle', 'profile']);
    expect(navItems('admin').map((i) => i.key)).toEqual(['home', 'admin', 'simulator']);
  });
});

describe('currentHref', () => {
  test('currentHref picks the longest match', () => {
    const admin = navItems('admin');
    expect(currentHref('/admin/simulator', admin)).toBe('/admin/simulator');
    expect(currentHref('/admin', admin)).toBe('/admin');
  });

  test('home is current only on /', () => {
    const driver = navItems('driver');
    expect(currentHref('/vehicles/new', driver)).toBe('/vehicles');
    expect(currentHref('/', driver)).toBe('/');
    expect(currentHref('/login', driver)).toBeNull();
  });

  test('a sibling path that only shares a prefix does not match', () => {
    expect(currentHref('/vehicles-old', navItems('driver'))).toBeNull();
  });
});
