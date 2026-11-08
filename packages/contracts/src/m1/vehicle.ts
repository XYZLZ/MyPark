import { z } from 'zod';
import { colorField, makeField, modelField, plateField } from './fields';

export const vehicleSchema = z.strictObject({
  id: z.string(),
  plate: z.string(),
  plate_normalized: z.string(),
  make: z.string(),
  model: z.string(),
  color: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
});

export const vehicleInputSchema = z.object({
  plate: plateField,
  make: makeField,
  model: modelField,
  color: colorField,
});

export type Vehicle = z.output<typeof vehicleSchema>;
export type VehicleInputValues = z.output<typeof vehicleInputSchema>;
