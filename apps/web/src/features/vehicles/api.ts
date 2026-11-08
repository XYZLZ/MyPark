import {
  parseEnvelope,
  parseListEnvelope,
  vehicleSchema,
  type Page,
  type Vehicle,
  type VehicleInputValues,
} from '@mypark/contracts';
import { apiFetch, parseNoContentBody } from '@/lib/api/client';

export const VEHICLES_QUERY_KEY = ['vehicles'] as const;

const parseVehicle = parseEnvelope(vehicleSchema);
const parseVehicles = parseListEnvelope(vehicleSchema);

export function fetchVehicles(): Promise<{ items: Vehicle[]; page: Page }> {
  return apiFetch('/api/v1/vehicles', parseVehicles);
}

export function createVehicle(input: VehicleInputValues): Promise<Vehicle> {
  return apiFetch('/api/v1/vehicles', parseVehicle, { method: 'POST', body: input });
}

export function updateVehicle(id: string, input: Partial<VehicleInputValues>): Promise<Vehicle> {
  return apiFetch(`/api/v1/vehicles/${encodeURIComponent(id)}`, parseVehicle, { method: 'PATCH', body: input });
}

export function deleteVehicle(id: string): Promise<void> {
  return apiFetch(`/api/v1/vehicles/${encodeURIComponent(id)}`, parseNoContentBody, { method: 'DELETE' });
}
