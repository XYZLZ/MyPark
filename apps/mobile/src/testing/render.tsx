import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render } from '@testing-library/react-native';
import type { ReactElement } from 'react';
import { I18nextProvider } from 'react-i18next';

import { initI18n, type AppLanguage } from '@/lib/i18n';

/**
 * Renders a screen inside the providers the app root supplies: i18n and a TanStack Query client.
 * The language defaults to English. A fresh client per call keeps tests isolated, and queries do not retry or expire.
 * Testing Library 14 renders asynchronously, so await this.
 */
export async function renderScreen(ui: ReactElement, opts: { language?: AppLanguage } = {}): Promise<void> {
  const i18n = initI18n(opts.language ?? 'en');
  // `gcTime: Infinity` leaves no garbage-collection timer behind, so Jest can exit.
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  await render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>{ui}</I18nextProvider>
    </QueryClientProvider>,
  );
}
