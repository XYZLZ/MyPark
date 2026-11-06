import { defineRouting } from 'next-intl/routing';

export const locales = ['es-DO', 'en'] as const;
export type Locale = (typeof locales)[number];
export const defaultLocale: Locale = 'es-DO';

export const routing = defineRouting({
  locales,
  defaultLocale,
  localePrefix: 'always',
});

/**
 * Maps any requested locale (URL segment, cookie, device setting) to a
 * supported one. Matching is case-insensitive; anything unknown or missing
 * falls back to es-DO, so an unsupported locale never errors.
 */
export function resolveLocale(input: string | undefined): Locale {
  if (!input) return defaultLocale;
  const wanted = input.toLowerCase();
  return locales.find((locale) => locale.toLowerCase() === wanted) ?? defaultLocale;
}

// A first path segment shaped like a BCP 47 tag: `fr`, `fr-FR`, `zh-Hant`.
const LOCALE_TAG = /^[a-z]{2}(?:-[a-z0-9]{2,8})?$/i;

/**
 * Returns the path to redirect to when the URL starts with a locale we do not
 * support (`/fr/x` -> `/es-DO/x`), or null to let next-intl handle the request.
 * Without this, next-intl treats `/fr` as an unprefixed page and redirects to
 * `/es-DO/fr`, which is a 404.
 */
export function unsupportedLocaleRedirect(pathname: string): string | null {
  const [, first = '', ...rest] = pathname.split('/');
  if (!LOCALE_TAG.test(first)) return null;
  if (locales.some((locale) => locale.toLowerCase() === first.toLowerCase())) return null;

  const tail = rest.filter(Boolean).join('/');
  return `/${resolveLocale(first)}${tail ? `/${tail}` : ''}`;
}
