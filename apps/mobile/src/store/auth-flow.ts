import { create } from 'zustand';

type AuthFlowState = {
  /** Email carried between auth screens for pre-fill (CC-13). Memory only, never in a URL. */
  email: string;
  setEmail: (email: string) => void;
  clear: () => void;
};

export const useAuthFlowStore = create<AuthFlowState>()((set) => ({
  email: '',
  setEmail: (email) => set({ email }),
  clear: () => set({ email: '' }),
}));
