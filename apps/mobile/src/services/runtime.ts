import { QueryClient } from '@tanstack/react-query';

import { getApiBaseUrl } from '@/lib/env';
import { useAuthStore } from '@/store/auth';
import { useSettingsStore } from '@/store/settings';

import { createHttpClient } from './httpClient';
import { createAuthAdapter, createSessionService } from './session';
import { tokenStorage } from './tokenStorage';

/** App-wide singletons. Tests inject their own through `createSessionService` and `createHttpClient`. */
export const http = createHttpClient(getApiBaseUrl());
export const queryClient = new QueryClient();

export const session = createSessionService({
  http,
  storage: tokenStorage,
  store: useAuthStore,
  queryClient,
  // The account's language wins after login and restore; the choice is kept for the next start.
  onUserLocale: (locale) => useSettingsStore.getState().setLanguage(locale),
});

http.setAuthAdapter(createAuthAdapter(session, useAuthStore));
