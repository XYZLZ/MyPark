'use client';

import { createContext, use, useState, type ReactNode } from 'react';
import { useStore } from 'zustand';
import { createUiStore, type UiState, type UiStore } from '@/store/ui';

const UiStoreContext = createContext<UiStore | null>(null);

/** Creates the UI store once per provider instance (never a module singleton). */
export function UiStoreProvider({ children }: { children: ReactNode }) {
  const [store] = useState(() => createUiStore());
  return <UiStoreContext value={store}>{children}</UiStoreContext>;
}

export function useUiStore<T>(selector: (state: UiState) => T): T {
  const store = use(UiStoreContext);
  if (!store) throw new Error('useUiStore must be used inside <UiStoreProvider>.');
  return useStore(store, selector);
}
