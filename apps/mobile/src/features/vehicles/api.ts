import {
  parseEnvelope,
  parseListEnvelope,
  parseNoContent,
  vehicleSchema,
  type Page,
  type Vehicle,
  type VehicleInputValues,
} from '@mypark/contracts';

import { unwrap, type HttpRequest } from '@/services/httpClient';
import { http } from '@/services/runtime';

export const VEHICLES_QUERY_KEY = ['vehicles'] as const;

const parseVehicle = parseEnvelope(vehicleSchema);
const parseVehicles = parseListEnvelope(vehicleSchema);
/** For 204 responses. `unwrap` hands the parser an optional status, `parseNoContent` wants one. */
const parseNothing = (body: unknown, status?: number) => parseNoContent(body, status ?? 0);

/** Signed-in calls: the client sends the bearer and refreshes on a 401. Failures reject with an ApiError. */
async function call<T>(req: Omit<HttpRequest, 'auth'>, parse: (body: unknown, status?: number) => T): Promise<T> {
  return unwrap(await http.request({ ...req, auth: true }), parse);
}

/** The id goes into the path, so it is encoded: a crafted value cannot reach another route. */
const vehiclePath = (id: string) => `/v1/vehicles/${encodeURIComponent(id)}`;

export function listVehicles(): Promise<{ items: Vehicle[]; page: Page }> {
  return call({ method: 'GET', path: '/v1/vehicles' }, parseVehicles);
}

export function getVehicle(id: string): Promise<Vehicle> {
  return call({ method: 'GET', path: vehiclePath(id) }, parseVehicle);
}

export function createVehicle(input: VehicleInputValues): Promise<Vehicle> {
  return call({ method: 'POST', path: '/v1/vehicles', body: input }, parseVehicle);
}

/** PATCH is partial: the form sends only the fields that changed (ruling T6). */
export function updateVehicle(id: string, input: Partial<VehicleInputValues>): Promise<Vehicle> {
  return call({ method: 'PATCH', path: vehiclePath(id), body: input }, parseVehicle);
}

export function deleteVehicle(id: string): Promise<void> {
  return call({ method: 'DELETE', path: vehiclePath(id) }, parseNothing);
}
