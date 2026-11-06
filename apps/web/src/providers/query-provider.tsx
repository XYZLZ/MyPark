'use client';

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useState, type ReactNode } from 'react';

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: {
      // With SSR, a staleTime above 0 avoids refetching right after hydration.
      queries: { staleTime: 60 * 1000 },
    },
  });
}

/** One QueryClient per provider instance: per request on the server, per session in the browser. */
export function QueryProvider({ children }: { children: ReactNode }) {
  const [queryClient] = useState(makeQueryClient);
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}
