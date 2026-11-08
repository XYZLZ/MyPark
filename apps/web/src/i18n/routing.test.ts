import { describe, expect, test } from 'vitest';
import { defaultLocale, locales, resolveLocale, routing, unsupportedLocaleRedirect } from './routing';

describe('routing', () => {
  test('declares es-DO as the default and en as the second locale', () => {
    expect(locales).toEqual(['es-DO', 'en']);
    expect(defaultLocale).toBe('es-DO');
    expect(routing.localePrefix).toBe('always');
  });

  test('resolveLocale falls back to es-DO', () => {
    expect(resolveLocale('fr')).toBe('es-DO');
    expect(resolveLocale(undefined)).toBe('es-DO');
    expect(resolveLocale('en')).toBe('en');
  });

  test('resolveLocale matches supported locales case-insensitively', () => {
    expect(resolveLocale('es-do')).toBe('es-DO');
    expect(resolveLocale('EN')).toBe('en');
    expect(resolveLocale('')).toBe('es-DO');
  });
});

describe('unsupportedLocaleRedirect', () => {
  test('sends an unsupported locale prefix to the default locale', () => {
    expect(unsupportedLocaleRedirect('/fr')).toBe('/es-DO');
    expect(unsupportedLocaleRedirect('/fr-FR/')).toBe('/es-DO');
    expect(unsupportedLocaleRedirect('/de/lots')).toBe('/es-DO/lots');
  });

  test('leaves supported prefixes and non-locale paths to next-intl', () => {
    expect(unsupportedLocaleRedirect('/es-DO')).toBeNull();
    expect(unsupportedLocaleRedirect('/en/lots')).toBeNull();
    expect(unsupportedLocaleRedirect('/')).toBeNull();
    expect(unsupportedLocaleRedirect('/dashboard')).toBeNull();
  });
});

describe('locale fixes', () => {
  test('resolveLocale maps base languages', () => {
    expect(resolveLocale('en-US')).toBe('en');
    expect(resolveLocale('es-MX')).toBe('es-DO');
    expect(resolveLocale('fr-FR')).toBe('es-DO');
  });

  test('/me and /qr are not treated as locales', () => {
    expect(unsupportedLocaleRedirect('/me')).toBeNull();
    expect(unsupportedLocaleRedirect('/qr')).toBeNull();
    expect(unsupportedLocaleRedirect('/qr/abc')).toBeNull();
  });

  test('a real but unsupported locale still redirects', () => {
    expect(unsupportedLocaleRedirect('/fr')).toBe('/es-DO');
    expect(unsupportedLocaleRedirect('/en-US/lots')).toBe('/en/lots');
    expect(unsupportedLocaleRedirect('/es-MX')).toBe('/es-DO');
  });
});
