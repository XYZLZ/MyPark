import { createStore } from 'zustand/vanilla';

/** What survives a screen change inside the sign-up and recovery flows: the email, never a password (CC-13). */
export type AuthFlowState = {
  email: string;
  setEmail: (email: string) => void;
  clear: () => void;
};

export type AuthFlowStore = ReturnType<typeof createAuthFlowStore>;

/** In memory only: nothing goes to a URL, storage or cookie. A factory so each provider owns its state. */
export function createAuthFlowStore() {
  return createStore<AuthFlowState>()((set) => ({
    email: '',
    setEmail: (email) => set({ email }),
    clear: () => set({ email: '' }),
  }));
}
