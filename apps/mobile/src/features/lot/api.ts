import {
  availabilitySchema,
  lotSchema,
  lotSpacesSchema,
  lotSummarySchema,
  parseEnvelope,
  parseListEnvelope,
  tariffSchema,
  type Availability,
  type Lot,
  type LotSummary,
  type SpaceState,
  type Tariff,
} from '@mypark/contracts';

import { unwrap } from '@/services/httpClient';
import { http } from '@/services/runtime';

const parseLots = parseListEnvelope(lotSummarySchema);
const parseLot = parseEnvelope(lotSchema);
const parseAvailability = parseEnvelope(availabilitySchema);
const parseSpaces = parseEnvelope(lotSpacesSchema);
const parseTariff = parseEnvelope(tariffSchema);

/** Public GETs: no bearer. Failures reject with an ApiError. */
async function get<T>(path: string, parse: (body: unknown, status?: number) => T): Promise<T> {
  return unwrap(await http.request({ method: 'GET', path }), parse);
}

/** The id goes into the path, so it is encoded: a crafted value cannot reach another route. */
const lotPath = (id: string) => `/v1/lots/${encodeURIComponent(id)}`;

export async function getLots(): Promise<LotSummary[]> {
  return (await get('/v1/lots', parseLots)).items;
}

export function getLot(id: string): Promise<Lot> {
  return get(lotPath(id), parseLot);
}

export function getAvailability(id: string): Promise<Availability> {
  return get(`${lotPath(id)}/availability`, parseAvailability);
}

export async function getSpaces(id: string): Promise<SpaceState[]> {
  return (await get(`${lotPath(id)}/spaces`, parseSpaces)).spaces;
}

export function getTariff(id: string): Promise<Tariff> {
  return get(`${lotPath(id)}/tariff`, parseTariff);
}

/** The REST fallback while the stream is down (M2B-18). */
export async function fetchLotSnapshot(id: string): Promise<{ availability: Availability; spaces: SpaceState[] }> {
  const [availability, spaces] = await Promise.all([getAvailability(id), getSpaces(id)]);
  return { availability, spaces };
}
