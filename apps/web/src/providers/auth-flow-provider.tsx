'use client';

import { createContext, use, useState, type ReactNode } from 'react';
import { useStore } from 'zustand';
import { createAuthFlowStore, type AuthFlowState, type AuthFlowStore } from '@/store/auth-flow';

const AuthFlowContext = createContext<AuthFlowStore | null>(null);

/** Creates the auth flow store once per provider instance (never a module singleton). */
export function AuthFlowProvider({ children }: { children: ReactNode }) {
  const [store] = useState(() => createAuthFlowStore());
  return <AuthFlowContext value={store}>{children}</AuthFlowContext>;
}

export function useAuthFlow<T>(selector: (state: AuthFlowState) => T): T {
  const store = use(AuthFlowContext);
  if (!store) throw new Error('useAuthFlow must be used inside <AuthFlowProvider>.');
  return useStore(store, selector);
}
