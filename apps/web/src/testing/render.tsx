import { render, type RenderOptions } from '@testing-library/react';
import { NextIntlClientProvider } from 'next-intl';
import type { ReactElement, ReactNode } from 'react';
import en from '@/i18n/messages/en.json';
import esDO from '@/i18n/messages/es-DO.json';
import type { Locale } from '@/i18n/routing';
import { AppProviders } from '@/providers/app-providers';

const MESSAGES = { 'es-DO': esDO, en } as const;

type ProviderOptions = Omit<RenderOptions, 'wrapper'> & { locale?: Locale };

/** Renders with the same providers as the app: next-intl messages, TanStack Query and the UI store. */
export function renderWithProviders(ui: ReactElement, { locale = 'es-DO', ...options }: ProviderOptions = {}) {
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <NextIntlClientProvider locale={locale} messages={MESSAGES[locale]} timeZone="America/Santo_Domingo">
        <AppProviders>{children}</AppProviders>
      </NextIntlClientProvider>
    );
  }
  return render(ui, { wrapper: Wrapper, ...options });
}
