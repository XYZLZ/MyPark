export type ShellVariant = 'guest' | 'driver' | 'admin';
export type NavKey = 'home' | 'vehicle' | 'profile' | 'admin' | 'simulator';
export type NavItem = { key: NavKey; href: '/' | '/vehicles' | '/profile' | '/admin' | '/admin/simulator' };

const HOME = { key: 'home', href: '/' } as const;
const VEHICLE = { key: 'vehicle', href: '/vehicles' } as const;
const PROFILE = { key: 'profile', href: '/profile' } as const;
const ADMIN = { key: 'admin', href: '/admin' } as const;
const SIMULATOR = { key: 'simulator', href: '/admin/simulator' } as const;

// An admin has no Vehicle or Profile items: `requireDriver` sends admins to /admin (PF7).
const ITEMS: Record<ShellVariant, readonly NavItem[]> = {
  guest: [HOME],
  driver: [HOME, VEHICLE, PROFILE],
  admin: [HOME, ADMIN, SIMULATOR],
};

export function navItems(variant: ShellVariant): readonly NavItem[] {
  return ITEMS[variant];
}

/** The item whose section contains `pathname`: the longest prefix wins, and `/` matches only itself. */
export function currentHref(pathname: string, items: readonly NavItem[]): NavItem['href'] | null {
  let best: NavItem | null = null;
  for (const item of items) {
    const matches = item.href === '/' ? pathname === '/' : pathname === item.href || pathname.startsWith(`${item.href}/`);
    if (matches && (best === null || item.href.length > best.href.length)) best = item;
  }
  return best?.href ?? null;
}
