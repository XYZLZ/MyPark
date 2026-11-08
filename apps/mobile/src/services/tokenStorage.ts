import * as SecureStore from 'expo-secure-store';

export const REFRESH_TOKEN_KEY = 'mypark.refresh_token';

/** iOS: keep the token out of backups and off other devices. Same options on every call so the item stays addressable. */
const STORE_OPTIONS: SecureStore.SecureStoreOptions = {
  keychainAccessible: SecureStore.WHEN_UNLOCKED_THIS_DEVICE_ONLY,
};

export type TokenStorage = {
  get(): Promise<string | null>;
  set(token: string): Promise<void>;
  remove(): Promise<void>;
};

/** The refresh token lives only here (Keychain / Keystore). Callers decide how to handle rejections (spec §6.4). */
export const tokenStorage: TokenStorage = {
  get: () => SecureStore.getItemAsync(REFRESH_TOKEN_KEY, STORE_OPTIONS),
  set: (token) => SecureStore.setItemAsync(REFRESH_TOKEN_KEY, token, STORE_OPTIONS),
  remove: () => SecureStore.deleteItemAsync(REFRESH_TOKEN_KEY, STORE_OPTIONS),
};
