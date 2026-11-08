import * as SecureStore from 'expo-secure-store';
import { create } from 'zustand';

import { SUPPORTED_LANGUAGES, type AppLanguage } from '@/lib/i18n/pickLanguage';

export const LANGUAGE_KEY = 'mypark.language';

type SettingsState = {
  /** User-chosen language; null means "follow the device". */
  language: AppLanguage | null;
  /** True once the stored choice has been read (or the read failed), so the splash can hide. */
  hydrated: boolean;
  /** Sets the language and persists it best effort: a storage failure keeps the in-memory choice. */
  setLanguage: (language: AppLanguage | null) => void;
  hydrateSettings: () => Promise<void>;
};

function isAppLanguage(value: string | null): value is AppLanguage {
  return SUPPORTED_LANGUAGES.some((language) => language === value);
}

export const useSettingsStore = create<SettingsState>()((set, get) => ({
  language: null,
  hydrated: false,
  setLanguage: (language) => {
    set({ language });
    const write = language === null ? SecureStore.deleteItemAsync(LANGUAGE_KEY) : SecureStore.setItemAsync(LANGUAGE_KEY, language);
    write.catch(() => undefined);
  },
  hydrateSettings: async () => {
    let stored: AppLanguage | null = null;
    try {
      const value = await SecureStore.getItemAsync(LANGUAGE_KEY);
      stored = isAppLanguage(value) ? value : null;
    } catch {
      stored = null;
    }
    // A choice made while the read was pending (the user's locale after a restore) wins.
    set({ language: get().language ?? stored, hydrated: true });
  },
}));

export const hydrateSettings = (): Promise<void> => useSettingsStore.getState().hydrateSettings();
