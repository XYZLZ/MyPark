import {
  emptyDataSchema,
  injectCarResultSchema,
  parseEnvelope,
  simulatorEventSchema,
  simulatorStatusSchema,
  type InjectCarValues,
  type SensorFaultValues,
  type SimulatorEvent,
  type SimulatorStatus,
} from '@mypark/contracts';
import { z } from 'zod';
import { apiFetch, parseNoContentBody } from '@/lib/api/client';
import { extractRawSeed, toStartBody, type StartFormValues } from './seed';

const BASE = '/api/admin/simulator';

export const simulatorKeys = {
  all: ['simulator'],
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

const parseCar = parseEnvelope(injectCarResultSchema);
const parseEmpty = parseEnvelope(emptyDataSchema);
const parseEvents = parseEnvelope(z.array(simulatorEventSchema));

/** Adds a car and returns its id. Works while idle: the simulator starts an implicit idle session (M2B-44). */
export async function injectCar(values: InjectCarValues): Promise<string> {
  const car = await apiFetch(`${BASE}/cars`, parseCar, { method: 'POST', body: values });
  return car.car_id;
}

/** `202 {data: {}}`. The id is validated by the caller against CAR_ID_PATTERN and again by the BFF allow-list. */
export function leaveCar(carId: string, loseTicket: boolean): Promise<void> {
  return apiFetch(`${BASE}/cars/${encodeURIComponent(carId)}/leave`, (body, status) => void parseEmpty(body, status), {
    method: 'POST',
    body: { lose_ticket: loseTicket },
  });
}

/** `204`. */
export function setFault(code: string, fault: SensorFaultValues['fault']): Promise<void> {
  return apiFetch(`${BASE}/sensors/${encodeURIComponent(code)}/fault`, parseNoContentBody, { method: 'POST', body: { fault } });
}

/** Events after the cursor. This envelope has no `meta.page`, so it is not a list envelope. */
export function fetchEvents(after: number, signal?: AbortSignal): Promise<SimulatorEvent[]> {
  return apiFetch(`${BASE}/events?after=${after}`, parseEvents, { signal });
}
