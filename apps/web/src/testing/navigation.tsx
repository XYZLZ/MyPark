import { useLocale } from 'next-intl';
import type { ComponentProps } from 'react';
import { vi } from 'vitest';

/**
 * Test double for `@/i18n/navigation` (registered in setup.ts). The real
 * `Link` needs the Next.js router context; this one renders an anchor with the
 * locale prefix, which is what the real one produces.
 */
export function Link({
  href,
  locale,
  ...rest
}: Omit<ComponentProps<'a'>, 'href'> & { href: string; locale?: string }) {
  const current = useLocale();
  return <a href={`/${locale ?? current}${href === '/' ? '' : href}`} {...rest} />;
}

/** The one router every `useRouter()` call returns, so a test can assert on `push` and `refresh`. */
export const router = {
  push: vi.fn(),
  replace: vi.fn(),
  refresh: vi.fn(),
  back: vi.fn(),
  forward: vi.fn(),
  prefetch: vi.fn(),
};

export const usePathname = vi.fn(() => '/');
export const useRouter = vi.fn(() => router);
export const redirect = vi.fn();
export const getPathname = vi.fn();
