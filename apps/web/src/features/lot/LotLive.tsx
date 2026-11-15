'use client';

import { availabilityFigures, lotQueryKeys, type Availability, type Lot, type SpaceState, type Tariff } from '@mypark/contracts';
import { useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';
import { useLayoutEffect } from 'react';
import { useTranslations } from 'next-intl';
import { useLiveLot } from './live/useLiveLot';
import { Announcer } from './components/Announcer';
import { CountCard } from './components/CountCard';
import { FreshnessLine } from './components/FreshnessLine';
import { LotInfo } from './components/LotInfo';
import { RatesList } from './components/RatesList';
import { StatTiles } from './components/StatTiles';
import type { LotSnapshot } from './server';

/** Lot and tariff have no browser route (the BFF allowlist has availability and spaces only): the RSC refreshes them. */
const SERVER_ONLY_STALE_MS = 300_000;
const neverFetched = () => Promise.reject(new Error('this key is filled by the server render and the stream'));

/**
 * `initialData` only seeds an empty cache, so after a client-side navigation (or a `router.refresh()`)
 * a fresher server snapshot would be ignored. Writes each key whose cache is older than the snapshot.
 * Availability also keeps the stream's version guard: it is replaced only by the same or a newer version.
 */
function reseedNewer(queryClient: QueryClient, lotId: string, snapshot: LotSnapshot): void {
  const { fetchedAt } = snapshot;
  const seed = <T,>(key: readonly unknown[], data: T, allowed: (cached: T | undefined) => boolean) => {
    const state = queryClient.getQueryState<T>(key);
    if (!state || state.dataUpdatedAt >= fetchedAt || !allowed(state.data)) return;
    queryClient.setQueryData(key, data, { updatedAt: fetchedAt });
  };
  seed(lotQueryKeys.availability(lotId), snapshot.availability, (cached) => !cached || snapshot.availability.version >= cached.version);
  seed(lotQueryKeys.spaces(lotId), snapshot.spaces, () => true);
  seed(lotQueryKeys.lot(lotId), snapshot.lot, () => true);
  seed(lotQueryKeys.tariff(lotId), snapshot.tariff, () => true);
}

type LotLiveProps = { variant: 'home' | 'compact'; lotId: string; apiOrigin: string | null; snapshot: LotSnapshot };

/**
 * The live block (design §4.3). The server snapshot seeds the five Query keys; the stream then
 * writes availability and spaces into the cache, and the figures come only from availability
 * (the gate counter), never from the map (M2B-15).
 */
export function LotLive({ variant, lotId, apiOrigin, snapshot }: LotLiveProps) {
  const t = useTranslations('availability');
  const queryClient = useQueryClient();
  const connection = useLiveLot(lotId, apiOrigin);
  // Before paint, so a stale cached figure never shows next to a newer time label.
  useLayoutEffect(() => reseedNewer(queryClient, lotId, snapshot), [queryClient, lotId, snapshot]);
  const seeded = { initialDataUpdatedAt: snapshot.fetchedAt, queryFn: neverFetched, enabled: false } as const;

  const { data: availability } = useQuery<Availability>({
    ...seeded,
    queryKey: lotQueryKeys.availability(lotId),
    initialData: snapshot.availability,
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  });
  // The map reads the spaces key (Task 9); seeding it here keeps the stream's first write a replace.
  useQuery<SpaceState[]>({
    ...seeded,
    queryKey: lotQueryKeys.spaces(lotId),
    initialData: snapshot.spaces,
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  });
  const { data: lot } = useQuery<Lot>({
    ...seeded,
    queryKey: lotQueryKeys.lot(lotId),
    initialData: snapshot.lot,
    staleTime: SERVER_ONLY_STALE_MS,
  });
  const { data: tariff } = useQuery<Tariff>({
    ...seeded,
    queryKey: lotQueryKeys.tariff(lotId),
    initialData: snapshot.tariff,
    staleTime: SERVER_ONLY_STALE_MS,
  });

  const figures = availabilityFigures(availability);
  const freshness = (
    <FreshnessLine
      connection={connection.status}
      updatedAt={availability.updated_at}
      lastFrameAt={connection.lastFrameAt}
      snapshotAt={snapshot.fetchedAt}
    />
  );
  const announcer = (
    <Announcer
      figures={figures}
      status={availability.status}
      connection={connection.status}
      lastUpdateAt={connection.lastFrameAt ?? snapshot.fetchedAt}
    />
  );

  if (variant === 'compact') {
    return (
      <section className="rounded-panel bg-surface p-4.5 shadow-sm">
        <CountCard figures={figures} status={availability.status} />
        <div className="mt-3">{freshness}</div>
        {announcer}
      </section>
    );
  }

  // PublicHome places these: it wraps the slot in one column. The map joins in Task 9.
  return (
    <>
      <section className="overflow-hidden rounded-panel bg-brand-sky-strong shadow-sm">
        <h2 className="sr-only">{t('title')}</h2>
        <StatTiles figures={figures} status={availability.status} />
        <div className="border-t border-hairline bg-surface px-4 py-2.5">{freshness}</div>
        {announcer}
      </section>
      <div className="grid content-start gap-5.5">
        <LotInfo lot={lot} />
        <RatesList tariff={tariff} />
      </div>
    </>
  );
}
