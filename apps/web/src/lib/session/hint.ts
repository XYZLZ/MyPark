import { ACCESS_COOKIE, REFRESH_COOKIE } from './cookies';

/**
 * Whether the request carries a session cookie. A hint to pick the public pages' actions and
 * sidebar, never a check: the guards on protected pages verify the session (CC-24).
 * Known limit (CC-24): the admin session uses the same cookies, so an admin browsing a public
 * page also sees the driver items there.
 */
export function hasSessionHint(store: { has(name: string): boolean }): boolean {
  return store.has(ACCESS_COOKIE) || store.has(REFRESH_COOKIE);
}
