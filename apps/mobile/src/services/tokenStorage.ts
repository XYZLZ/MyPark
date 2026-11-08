import * as SecureStore from 'expo-secure-store';

export const REFRESH_TOKEN_KEY = 'mypark.refresh_token';

export type TokenStorage = {
  get(): Promise<string | null>;
  set(token: string): Promise<void>;
  remove(): Promise<void>;
};

/** The refresh token lives only here (Keychain / Keystore). Callers decide how to handle rejections (spec §6.4). */
export const tokenStorage: TokenStorage = {
  get: () => SecureStore.getItemAsync(REFRESH_TOKEN_KEY),
  set: (token) => SecureStore.setItemAsync(REFRESH_TOKEN_KEY, token),
  remove: () => SecureStore.deleteItemAsync(REFRESH_TOKEN_KEY),
};
