import { create } from 'zustand';

import type { AppLanguage } from '@/lib/i18n/pickLanguage';

type SettingsState = {
  /** User-chosen language; null means "follow the device". Persistence arrives in M1. */
  language: AppLanguage | null;
  setLanguage: (language: AppLanguage | null) => void;
};

export const useSettingsStore = create<SettingsState>()((set) => ({
  language: null,
  setLanguage: (language) => set({ language }),
}));
