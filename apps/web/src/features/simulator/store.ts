import { createStore } from 'zustand/vanilla';
import { initialLogState, type LogState } from './log-cursor';

export type ConsoleState = {
  /** Set when "Stop after cars leave" returns 202 and cleared once status reports not running (M2B-45). Lost on reload. */
  draining: boolean;
  setDraining: (value: boolean) => void;
  /**
   * Car ids returned by inject with `hold` and not yet released (M2B-32). Cleared when a run ends,
   * not when `started_at` changes: a takeover of an idle session keeps its cars (M2B-44).
   * Like `log`, this is server-derived data kept here on purpose (design 7.3, M2B-32; PF11).
   */
  heldCars: string[];
  addHeld: (id: string) => void;
  removeHeld: (id: string) => void;
  clearHeld: () => void;
  /** The event log and its cursor (design 7.3, M2B-31; PF11). */
  log: LogState;
  setLog: (next: LogState) => void;
};

export type ConsoleStore = ReturnType<typeof createConsoleStore>;

/** A factory, not a module singleton: each provider creates its own store (the `store/ui.ts` pattern). */
export function createConsoleStore() {
  return createStore<ConsoleState>()((set) => ({
    draining: false,
    setDraining: (draining) => set({ draining }),
    heldCars: [],
    addHeld: (id) => set((state) => (state.heldCars.includes(id) ? state : { heldCars: [...state.heldCars, id] })),
    removeHeld: (id) => set((state) => ({ heldCars: state.heldCars.filter((held) => held !== id) })),
    clearHeld: () => set((state) => (state.heldCars.length === 0 ? state : { heldCars: [] })),
    log: initialLogState,
    setLog: (log) => set({ log }),
  }));
}
