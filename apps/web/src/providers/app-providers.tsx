'use client';

import type { ReactNode } from 'react';
import { AuthFlowProvider } from './auth-flow-provider';
import { QueryProvider } from './query-provider';
import { UiStoreProvider } from './ui-store-provider';

export function AppProviders({ children }: { children: ReactNode }) {
  return (
    <QueryProvider>
      <UiStoreProvider>
        <AuthFlowProvider>{children}</AuthFlowProvider>
      </UiStoreProvider>
    </QueryProvider>
  );
}
