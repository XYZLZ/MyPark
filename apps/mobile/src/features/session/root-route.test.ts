import type { AuthStatus } from '@/store/auth';

import { rootHref, selectRootRoute, type RootRoute } from './root-route';

test.each<[AuthStatus, boolean, RootRoute]>([
  ['restoring', false, 'restoring'],
  ['restoreFailed', false, 'restore-failed'],
  ['signedOut', false, 'auth'],
  ['signedIn', true, 'gate'],
  ['signedIn', false, 'tabs'],
])('%s with legalPending %s goes to %s', (status, legalPending, expected) => {
  expect(selectRootRoute(status, legalPending)).toBe(expected);
});

test('a signedOut user never reaches gate even with a stale legalPending', () => {
  expect(selectRootRoute('signedOut', true)).toBe('auth');
  expect(selectRootRoute('restoreFailed', true)).toBe('restore-failed');
});

test('each route has a landing href and restoring has none', () => {
  expect(rootHref('restore-failed')).toBe('/restore-failed');
  expect(rootHref('auth')).toBe('/welcome');
  expect(rootHref('gate')).toBe('/legal-acceptance');
  expect(rootHref('tabs')).toBe('/vehicles');
  expect(rootHref('restoring')).toBeNull();
});
