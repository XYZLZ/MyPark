import { CAR_ID_PATTERN, SPACE_CODE_PATTERN } from '@mypark/contracts';

export type SimulatorRoute = { method: 'GET' | 'POST'; upstreamPath: string };

type Part = string | RegExp;

// Design §7.2. Paths are relative to /api/admin/simulator and to /control upstream.
const ENTRIES: ReadonlyArray<{ method: SimulatorRoute['method']; parts: readonly Part[] }> = [
  { method: 'GET', parts: ['status'] },
  { method: 'POST', parts: ['scenarios', 'start'] },
  { method: 'POST', parts: ['scenarios', 'stop'] },
  { method: 'POST', parts: ['cars'] },
  { method: 'POST', parts: ['cars', CAR_ID_PATTERN, 'leave'] },
  { method: 'POST', parts: ['sensors', SPACE_CODE_PATTERN, 'fault'] },
  { method: 'GET', parts: ['events'] },
];

const AFTER = /^(0|[1-9]\d{0,18})$/;
const INT64_MAX = 9223372036854775807n;

/** The `after` cursor as a query string ('' when absent), or null when it is not one non-negative int64. */
function afterQuery(search: URLSearchParams): string | null {
  const values = search.getAll('after');
  if (values.length === 0) return '';
  const [value] = values;
  if (values.length > 1 || !AFTER.test(value!) || BigInt(value!) > INT64_MAX) return null;
  return `?after=${value}`;
}

/**
 * The upstream call for a console request, or null (M2B-28). Literal segments
 * must be equal and ids must match the contract patterns, which are anchored,
 * so no segment can carry a separator or a dot-dot. Only `after` on `events`
 * is forwarded; every other query parameter is dropped.
 */
export function matchSimulatorRoute(method: string, segments: string[], search: URLSearchParams): SimulatorRoute | null {
  for (const entry of ENTRIES) {
    if (entry.method !== method || entry.parts.length !== segments.length) continue;
    const ok = entry.parts.every((part, i) => (typeof part === 'string' ? part === segments[i] : part.test(segments[i]!)));
    if (!ok) continue;
    const path = `/control/${segments.join('/')}`;
    if (segments[0] !== 'events') return { method: entry.method, upstreamPath: path };
    const query = afterQuery(search);
    return query === null ? null : { method: entry.method, upstreamPath: `${path}${query}` };
  }
  return null;
}
