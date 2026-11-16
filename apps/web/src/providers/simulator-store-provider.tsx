'use client';

import { createContext, use, useState, type ReactNode } from 'react';
import { useStore } from 'zustand';
import { createConsoleStore, type ConsoleState, type ConsoleStore } from '@/features/simulator/store';

const ConsoleStoreContext = createContext<ConsoleStore | null>(null);

/** Creates the console store once per provider instance (never a module singleton). */
export function SimulatorStoreProvider({ children }: { children: ReactNode }) {
  const [store] = useState(() => createConsoleStore());
  return <ConsoleStoreContext value={store}>{children}</ConsoleStoreContext>;
}

export function useConsoleStore<T>(selector: (state: ConsoleState) => T): T {
  const store = use(ConsoleStoreContext);
  if (!store) throw new Error('useConsoleStore must be used inside <SimulatorStoreProvider>.');
  return useStore(store, selector);
}
