'use client';

import { ApiError } from '@mypark/contracts';
import { MutationCache, QueryCache, QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useState, type ReactNode } from 'react';
import { useRouter } from '@/i18n/navigation';
import { handleSessionError } from '@/lib/api/client';

const MAX_RETRIES = 2;

/** Retries only what may be transient: a network failure or a 5xx, at most twice (spec 5.8). */
export function shouldRetry(failureCount: number, error: unknown): boolean {
  if (failureCount >= MAX_RETRIES || !(error instanceof ApiError)) return false;
  return error.code === 'client.network' || (error.status ?? 0) >= 500;
}

function makeQueryClient(effects: { refresh(): void }) {
  const onError = (error: unknown) => handleSessionError(error, effects);
  return new QueryClient({
    queryCache: new QueryCache({ onError }),
    mutationCache: new MutationCache({ onError }),
    defaultOptions: {
      // With SSR, a staleTime above 0 avoids refetching right after hydration.
      // Only queries retry: repeating a POST could create the same record twice.
      queries: { staleTime: 60 * 1000, retry: shouldRetry },
    },
  });
}

/** One QueryClient per provider instance: per request on the server, per session in the browser. */
export function QueryProvider({ children }: { children: ReactNode }) {
  const router = useRouter();
  const [queryClient] = useState(() => makeQueryClient({ refresh: () => router.refresh() }));
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}
