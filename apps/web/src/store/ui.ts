import { createStore } from 'zustand/vanilla';

export type Theme = 'system' | 'light' | 'dark';

export type UiState = {
  theme: Theme;
  setTheme: (theme: Theme) => void;
};

export type UiStore = ReturnType<typeof createUiStore>;

/**
 * Client-only UI preferences. A factory, not a module singleton: each
 * provider instance creates its own store, so state never leaks between
 * requests during SSR.
 */
export function createUiStore(initial: Partial<Pick<UiState, 'theme'>> = {}) {
  return createStore<UiState>()((set) => ({
    theme: initial.theme ?? 'system',
    setTheme: (theme) => set({ theme }),
  }));
}
