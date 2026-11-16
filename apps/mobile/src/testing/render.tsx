import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render } from '@testing-library/react-native';
import type { ReactElement } from 'react';
import { I18nextProvider } from 'react-i18next';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { initI18n, type AppLanguage } from '@/lib/i18n';

/**
 * Renders a screen inside the providers the app root supplies: i18n, a TanStack Query client and safe-area insets (none by default;
 * `renderAtSize` in testing/layout.tsx supplies device insets by nesting its own provider).
 * The language defaults to English. A fresh client per call keeps tests isolated, and queries do not retry or expire;
 * pass `queryClient` to spy on one before the screen mounts.
 * Testing Library 14 renders asynchronously, so await this.
 */
export async function renderScreen(
  ui: ReactElement,
  opts: { language?: AppLanguage; queryClient?: QueryClient } = {},
): Promise<void> {
  const i18n = initI18n(opts.language ?? 'en');
  // `gcTime: Infinity` leaves no garbage-collection timer behind, so Jest can exit.
  const queryClient = opts.queryClient ?? new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  await render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <SafeAreaProvider initialMetrics={{ frame: { x: 0, y: 0, width: 360, height: 640 }, insets: { top: 0, bottom: 0, left: 0, right: 0 } }}>
          {ui}
        </SafeAreaProvider>
      </I18nextProvider>
    </QueryClientProvider>,
  );
}
