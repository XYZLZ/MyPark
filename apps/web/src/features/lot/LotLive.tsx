'use client';

import { availabilityFigures, lotQueryKeys, type Availability, type Lot, type SpaceState, type Tariff } from '@mypark/contracts';
import { useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';
import { useLayoutEffect, useState } from 'react';
import { useTranslations } from 'next-intl';
import { useLiveLot } from './live/useLiveLot';
import { Announcer } from './components/Announcer';
import { CountCard } from './components/CountCard';
import { FreshnessLine } from './components/FreshnessLine';
import { FreeSpacesList } from './components/FreeSpacesList';
import { Icon } from './components/icons';
import { LotInfo } from './components/LotInfo';
import { MapLegend } from './components/MapLegend';
import { RatesList } from './components/RatesList';
import { SpaceMap, type ZoomLevel } from './components/SpaceMap';
import { SpacesSummary } from './components/SpacesSummary';
import { StatTiles } from './components/StatTiles';
import { ZoomControls } from './components/ZoomControls';
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
  const tSpaces = useTranslations('spaces');
  const [zoom, setZoom] = useState<ZoomLevel>(1);
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
  // The stream writes the sensor map into this key; seeding it keeps the first write a replace.
  const { data: spaces } = useQuery<SpaceState[]>({
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

  // The map is not authoritative between a (re)connect and its first full frame (design §9.3). Before
  // the first connection and with no stream at all, the server snapshot is all there is, so it is not dimmed.
  const mapReady = connection.spacesReady || connection.status === 'connecting' || connection.status === 'unavailable';
  const mapDetails = (
    <>
      <MapLegend />
      <SpacesSummary spaces={spaces} />
      <FreeSpacesList spaces={spaces} />
    </>
  );

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
        <div className="mt-4">
          <SpaceMap spaces={spaces} ready={mapReady} zoom={1} />
          {mapDetails}
        </div>
      </section>
    );
  }

  // PublicHome places these: it wraps the slot in one column.
  return (
    <>
      <section className="overflow-hidden rounded-panel bg-brand-sky-strong shadow-sm">
        <h2 className="sr-only">{t('title')}</h2>
        <StatTiles figures={figures} status={availability.status} />
        <div className="border-t border-hairline bg-surface px-4 py-2.5">{freshness}</div>
        {announcer}
      </section>
      <section className="rounded-panel bg-surface px-5.5 py-5 shadow-sm">
        <div className="mb-3.5 flex flex-wrap items-center gap-x-3 gap-y-1">
          <h2 className="flex items-center gap-2.5 text-xl font-bold text-brand-navy">
            <Icon name="pin" />
            {tSpaces('title')}
          </h2>
          <span className="ml-auto text-sm text-ink-muted">{tSpaces('sensorsLive')}</span>
        </div>
        <div className="relative">
          <SpaceMap spaces={spaces} ready={mapReady} zoom={zoom} />
          <div className="absolute right-3 bottom-3">
            <ZoomControls zoom={zoom} onZoom={setZoom} />
          </div>
        </div>
        {mapDetails}
      </section>
      <div className="grid content-start gap-5.5">
        <LotInfo lot={lot} />
        <RatesList tariff={tariff} />
      </div>
    </>
  );
}
