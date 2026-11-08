import 'server-only';
import { ApiError, type LegalCurrent, type User } from '@mypark/contracts';
import { cookies } from 'next/headers';
import { forbidden, redirect } from 'next/navigation';
import type { Locale } from '@/i18n/routing';
import { getLegalCurrent, getMe } from '@/lib/api/server';
import { ACCESS_COOKIE, REFRESH_COOKIE } from '@/lib/session/cookies';

export type DriverGate =
  | { state: 'ready'; user: User }
  | { state: 'legal'; user: User; legal: LegalCurrent }
  | { state: 'suspended' }
  | { state: 'unavailable' };

export type AdminGate = { state: 'ready'; user: User } | { state: 'suspended' } | { state: 'unavailable' };

type Area = 'driver' | 'admin';

type Identity = { state: 'user'; user: User } | { state: 'suspended' } | { state: 'unavailable' };

const home = (area: Area, locale: Locale) => `/${locale}/${area === 'admin' ? 'admin' : 'vehicles'}`;

/**
 * Who is asking, or a redirect. Cookies are tested by `?.value`: a cleared cookie is
 * still listed, with no value (spec 5.6). `redirect` throws, so it never sits in a try.
 */
async function identify(area: Area, locale: Locale): Promise<Identity> {
  const store = await cookies();
  const access = Boolean(store.get(ACCESS_COOKIE)?.value);
  const refresh = Boolean(store.get(REFRESH_COOKIE)?.value);

  if (!access && !refresh) {
    const login = area === 'admin' ? 'admin/login' : 'login';
    redirect(`/${locale}/${login}?next=${encodeURIComponent(home(area, locale))}`);
  }
  // The proxy could not refresh: transient, so the session is kept and the user may retry.
  if (!access) return { state: 'unavailable' };

  let user: User | undefined;
  let failure: ApiError | undefined;
  try {
    user = await getMe();
  } catch (error) {
    if (!(error instanceof ApiError)) throw error;
    failure = error;
  }
  if (user) return { state: 'user', user };

  if (failure?.status === 401) {
    const params = new URLSearchParams({ locale, next: home(area, locale) });
    redirect(`/api/session/end?${params}`);
  }
  if (failure?.code === 'auth.account_suspended') return { state: 'suspended' };
  throw failure;
}

export async function requireDriver(locale: Locale): Promise<DriverGate> {
  const who = await identify('driver', locale);
  if (who.state !== 'user') return who;
  // An admin cannot use the driver API (OC-16): send them to their own area (CC-24).
  if (who.user.role === 'admin') redirect(`/${locale}/admin`);

  let legal: LegalCurrent;
  try {
    legal = await getLegalCurrent();
  } catch {
    // Without the current versions the gate cannot be decided, so block instead of skipping it.
    return { state: 'unavailable' };
  }
  const { terms_version, privacy_version } = who.user.legal;
  if (terms_version !== legal.terms.version || privacy_version !== legal.privacy.version) {
    return { state: 'legal', user: who.user, legal };
  }
  return { state: 'ready', user: who.user };
}

export async function requireAdmin(locale: Locale): Promise<AdminGate> {
  const who = await identify('admin', locale);
  if (who.state !== 'user') return who;
  // A real 403 (CC-7). The admin area is exempt from the legal gate (OC-15).
  if (who.user.role !== 'admin') forbidden();
  return { state: 'ready', user: who.user };
}
