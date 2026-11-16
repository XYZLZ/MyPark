import { lotQueryKeys } from '@mypark/contracts';
import { useQuery } from '@tanstack/react-query';

import { getLots } from '../api';

/** The lot this build shows: the first of `GET /v1/lots` (M2B-7). `null` until known, or when there is none. */
export function useLotId(): { lotId: string | null; status: 'pending' | 'error' | 'success'; refetch: () => void } {
  const query = useQuery({ queryKey: lotQueryKeys.all, queryFn: getLots, select: (lots) => lots[0]?.id ?? null });
  return {
    lotId: query.data ?? null,
    status: query.status,
    refetch: () => void query.refetch(),
  };
}
