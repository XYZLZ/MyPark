/** Used when EXPO_PUBLIC_API_URL is unset. On a physical device this is unreachable, so the
 * health screen shows the localized "unavailable" state instead of crashing. */
export const DEFAULT_API_URL = 'http://localhost:8080';

/**
 * API base URL without a trailing slash.
 * Must read `process.env.EXPO_PUBLIC_API_URL` literally: Expo only inlines
 * static member access, not destructuring or computed keys.
 */
export function getApiBaseUrl(): string {
  const fromEnv = process.env.EXPO_PUBLIC_API_URL?.trim();
  return (fromEnv || DEFAULT_API_URL).replace(/\/+$/, '');
}
