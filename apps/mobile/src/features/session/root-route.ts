import type { AuthStatus } from '@/store/auth';

export type RootRoute = 'restoring' | 'restore-failed' | 'auth' | 'gate' | 'tabs';

/** Which route group the auth state allows. Signed-out and failed-restore states never reach the gate. */
export function selectRootRoute(status: AuthStatus, legalPending: boolean): RootRoute {
  switch (status) {
    case 'restoring':
      return 'restoring';
    case 'restoreFailed':
      return 'restore-failed';
    case 'signedOut':
      return 'auth';
    case 'signedIn':
      return legalPending ? 'gate' : 'tabs';
  }
}

/** Where `index` sends the user. Null while restoring: the splash is still up. */
export function rootHref(route: RootRoute): string | null {
  switch (route) {
    case 'restoring':
      return null;
    case 'restore-failed':
      return '/restore-failed';
    case 'auth':
      return '/welcome';
    case 'gate':
      return '/legal-acceptance';
    case 'tabs':
      return '/vehicles';
  }
}
