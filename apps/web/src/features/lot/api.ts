import { availabilitySchema, lotSpacesSchema, parseEnvelope, type Availability, type SpaceState } from '@mypark/contracts';
import { apiFetch } from '@/lib/api/client';

const parseAvailability = parseEnvelope(availabilitySchema);
const parseSpaces = parseEnvelope(lotSpacesSchema);

/** The REST fallback while the stream is down, through the BFF's public lot GETs (M2B-18). */
export async function fetchLotSnapshot(lotId: string): Promise<{ availability: Availability; spaces: SpaceState[] }> {
  const base = `/api/v1/lots/${encodeURIComponent(lotId)}`;
  const [availability, { spaces }] = await Promise.all([
    apiFetch(`${base}/availability`, parseAvailability),
    apiFetch(`${base}/spaces`, parseSpaces),
  ]);
  return { availability, spaces };
}
