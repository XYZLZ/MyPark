import { createStore } from 'zustand/vanilla';

export type ConsoleState = {
  /** Set when "Stop after cars leave" returns 202 and cleared once status reports not running (M2B-45). Lost on reload. */
  draining: boolean;
  setDraining: (value: boolean) => void;
};

export type ConsoleStore = ReturnType<typeof createConsoleStore>;

/** A factory, not a module singleton: each provider creates its own store (the `store/ui.ts` pattern). */
export function createConsoleStore() {
  return createStore<ConsoleState>()((set) => ({
    draining: false,
    setDraining: (draining) => set({ draining }),
  }));
}
