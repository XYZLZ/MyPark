import { parseEnvelope, simulatorStatusSchema, type SimulatorStatus } from '@mypark/contracts';
import { apiFetch } from '@/lib/api/client';
import { extractRawSeed, toStartBody, type StartFormValues } from './seed';

const BASE = '/api/admin/simulator';

export const simulatorKeys = {
  status: ['simulator', 'status'],
  events: ['simulator', 'events'],
} as const;

export type StatusResult = { status: SimulatorStatus; rawSeed: string };

const parseStatus = parseEnvelope(simulatorStatusSchema);

/** The status plus its seed digits taken from the raw text, because the parsed number may be rounded (M2B-33). */
function parseStatusResult(body: unknown, httpStatus?: number, text = ''): StatusResult {
  const status = parseStatus(body, httpStatus);
  return { status, rawSeed: extractRawSeed(text) ?? String(status.seed) };
}

export function fetchStatus(): Promise<StatusResult> {
  return apiFetch(`${BASE}/status`, parseStatusResult);
}

export function startScenario(values: StartFormValues): Promise<StatusResult> {
  return apiFetch(`${BASE}/scenarios/start`, parseStatusResult, { method: 'POST', bodyText: toStartBody(values) });
}

export function stopScenario(drain: boolean): Promise<StatusResult> {
  return apiFetch(`${BASE}/scenarios/stop`, parseStatusResult, { method: 'POST', body: { drain } });
}
