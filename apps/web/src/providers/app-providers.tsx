'use client';

import type { ReactNode } from 'react';
import { QueryProvider } from './query-provider';
import { UiStoreProvider } from './ui-store-provider';

export function AppProviders({ children }: { children: ReactNode }) {
  return (
    <QueryProvider>
      <UiStoreProvider>{children}</UiStoreProvider>
    </QueryProvider>
  );
}
