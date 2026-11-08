import { useState, type ReactNode } from 'react';
import { useAuthFlow } from '@/providers/auth-flow-provider';

/** Puts an email in the auth flow store, then renders the children (so they see it on first render). */
export function SeedAuthFlow({ email, children }: { email: string; children: ReactNode }) {
  const setEmail = useAuthFlow((s) => s.setEmail);
  // Seeds during the first render so the children read the email on theirs.
  useState(() => setEmail(email));
  return children;
}

/** Renders the whole auth flow state as JSON, so a test can prove what the store holds. */
export function AuthFlowProbe() {
  const state = useAuthFlow((s) => s);
  return <div data-testid="auth-flow">{JSON.stringify(state)}</div>;
}
