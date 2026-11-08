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

export const usePathname = vi.fn(() => '/');
export const useRouter = vi.fn();
export const redirect = vi.fn();
export const getPathname = vi.fn();
