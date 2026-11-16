'use client';

import { createContext, use, useState, type ReactNode } from 'react';
import { useStore } from 'zustand';
import { createConsoleStore, type ConsoleState, type ConsoleStore } from '@/features/simulator/store';

/** Exported so a test can mount a component with a store it created itself. */
export const SimulatorStoreContext = createContext<ConsoleStore | null>(null);

/** Creates the console store once per provider instance (never a module singleton). */
export function SimulatorStoreProvider({ children }: { children: ReactNode }) {
  const [store] = useState(() => createConsoleStore());
  return <SimulatorStoreContext value={store}>{children}</SimulatorStoreContext>;
}

/** The store itself, for code that reads or writes outside a render (a query function). */
export function useConsoleStoreApi(): ConsoleStore {
  const store = use(SimulatorStoreContext);
  if (!store) throw new Error('useConsoleStoreApi must be used inside <SimulatorStoreProvider>.');
  return store;
}

export function useConsoleStore<T>(selector: (state: ConsoleState) => T): T {
  return useStore(useConsoleStoreApi(), selector);
}
