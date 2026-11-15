import { z } from 'zod';
import { lotStatusSchema } from './lot';

export const availabilitySchema = z.strictObject({
  lot_id: z.string(),
  available: z.number(),
  occupied: z.number(),
  effective_capacity: z.number(),
  capacity: z.number(),
  effective_holds: z.number(),
  upcoming_reservations_2h: z.number(),
  status: lotStatusSchema,
  version: z.number(),
  updated_at: z.string(),
});

/** Payload of the `heartbeat` SSE event. */
export const heartbeatSchema = z.strictObject({ as_of: z.string() });

export type Availability = z.output<typeof availabilitySchema>;
export type Heartbeat = z.output<typeof heartbeatSchema>;
