import type { User } from '@mypark/contracts';
import { create } from 'zustand';

export type AuthStatus = 'restoring' | 'signedOut' | 'signedIn' | 'restoreFailed';
export type AuthNotice = 'session_ended' | 'storage_failed' | 'admin_web_only' | 'logout_all_failed' | null;

type AuthState = {
  status: AuthStatus;
  /** Memory only. The refresh token never enters this store: it lives in secure storage. */
  accessToken: string | null;
  user: User | null;
  legalPending: boolean;
  notice: AuthNotice;
  setSession: (s: { accessToken: string; user: User; legalPending: boolean }) => void;
  setAccessToken: (token: string) => void;
  setStatus: (status: AuthStatus) => void;
  clearSession: (notice?: AuthNotice) => void;
  setLegalPending: (value: boolean) => void;
  setUser: (user: User) => void;
  /** Raises a notice without ending the session (secure-store write failure, spec §6.4). */
  setNotice: (notice: AuthNotice) => void;
  clearNotice: () => void;
};

/** Not persisted on purpose (spec §6.1). */
export const useAuthStore = create<AuthState>()((set) => ({
  status: 'restoring',
  accessToken: null,
  user: null,
  legalPending: false,
  notice: null,
  setSession: ({ accessToken, user, legalPending }) => set({ status: 'signedIn', accessToken, user, legalPending }),
  setAccessToken: (accessToken) => set({ accessToken }),
  setStatus: (status) => set({ status }),
  clearSession: (notice = null) =>
    set({ status: 'signedOut', accessToken: null, user: null, legalPending: false, notice }),
  setLegalPending: (legalPending) => set({ legalPending }),
  setUser: (user) => set({ user }),
  setNotice: (notice) => set({ notice }),
  clearNotice: () => set({ notice: null }),
}));
