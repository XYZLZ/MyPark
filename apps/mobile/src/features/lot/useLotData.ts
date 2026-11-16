import { lotQueryKeys, type Availability, type Lot, type SpaceState, type Tariff } from '@mypark/contracts';
import { useQuery } from '@tanstack/react-query';

import { getAvailability, getLot, getSpaces, getTariff } from './api';
import { CONNECTING, type Connection } from './live/connection-store';
import { useLiveLot } from './live/useLiveLot';
import { useLotId } from './live/useLotId';

/** Lot and tariff change rarely; five minutes is the same window as web. */
const SLOW_STALE_MS = 300_000;

export type LotData = {
  lotId: string | null;
  lot?: Lot;
  availability?: Availability;
  spaces?: SpaceState[];
  tariff?: Tariff;
  connection: Connection;
  /** Nothing to show and no way to get it right now (design §4.3): the error panel replaces the live block. */
  loadError: boolean;
  retry: () => void;
  /** Client time of the last write to the availability key (the REST snapshot, or a later stream frame): the stale and unavailable lines' fallback when no frame has arrived. */
  snapshotAt: number;
};

/**
 * The live block's data. REST fills the four Query keys once; the stream then owns availability and spaces, so those two are
 * never stale and never refetched on a foreground: a refetch would overwrite newer stream data with an older snapshot.
 */
export function useLotData(): LotData {
  const id = useLotId();
  const lotId = id.lotId;
  const connection = useLiveLot(lotId);
  const enabled = lotId !== null;
  const key = lotId ?? '';

  const lot = useQuery({ queryKey: lotQueryKeys.lot(key), queryFn: () => getLot(key), enabled, staleTime: SLOW_STALE_MS });
  const availability = useQuery({
    queryKey: lotQueryKeys.availability(key),
    queryFn: () => getAvailability(key),
    enabled,
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  });
  const spaces = useQuery({
    queryKey: lotQueryKeys.spaces(key),
    queryFn: () => getSpaces(key),
    enabled,
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  });
  const tariff = useQuery({ queryKey: lotQueryKeys.tariff(key), queryFn: () => getTariff(key), enabled, staleTime: SLOW_STALE_MS });

  const noLot = id.status === 'error' || (id.status === 'success' && lotId === null);
  const connectionDown = connection.status === 'stale' || connection.status === 'unavailable';
  const noFigures =
    enabled && availability.data === undefined && !availability.isFetching && (availability.isError || connectionDown);

  return {
    lotId,
    lot: lot.data,
    availability: availability.data,
    spaces: spaces.data,
    tariff: tariff.data,
    connection: lotId === null ? CONNECTING : connection,
    loadError: noLot || noFigures,
    retry: () => {
      if (lotId === null) return id.refetch();
      for (const query of [lot, availability, spaces, tariff]) void query.refetch();
    },
    snapshotAt: availability.dataUpdatedAt,
  };
}
