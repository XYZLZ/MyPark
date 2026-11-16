import 'server-only';
import type { Availability, Lot, SpaceState, Tariff } from '@mypark/contracts';
import { getAvailability, getLot, getSpaces, getTariff, listLots } from '@/lib/api/server';

export type LotSnapshot = {
  lot: Lot;
  availability: Availability;
  spaces: SpaceState[];
  tariff: Tariff;
  /** When the four reads started: the data is at least this fresh. It seeds `initialDataUpdatedAt`. */
  fetchedAt: number;
};

/**
 * The first lot and its four resources, for the home's first paint (M2B-7, M2B-17).
 * Returns `null` for an empty list or when any call fails, so the page renders the error panel
 * instead of throwing: the hero, Log in and Sign up never depend on the API.
 */
export async function loadLotSnapshot(now: () => number = Date.now): Promise<LotSnapshot | null> {
  let lots;
  try {
    lots = await listLots();
  } catch {
    return null;
  }
  const first = lots[0];
  if (!first) return null;

  const fetchedAt = now();
  const [lot, availability, spaces, tariff] = await Promise.allSettled([
    getLot(first.id),
    getAvailability(first.id),
    getSpaces(first.id),
    getTariff(first.id),
  ]);
  if (
    lot.status === 'rejected' ||
    availability.status === 'rejected' ||
    spaces.status === 'rejected' ||
    tariff.status === 'rejected'
  ) {
    return null;
  }
  return { lot: lot.value, availability: availability.value, spaces: spaces.value, tariff: tariff.value, fetchedAt };
}
