import { Redirect, type Href } from 'expo-router';

import { rootHref, selectRootRoute } from '@/features/session/root-route';
import { useAuthStore } from '@/store/auth';

/** Sends the user to the area the auth state allows. Renders nothing while the session restores. */
export default function Index() {
  const status = useAuthStore((s) => s.status);
  const legalPending = useAuthStore((s) => s.legalPending);
  const href = rootHref(selectRootRoute(status, legalPending));
  return href === null ? null : <Redirect href={href as Href} />;
}
