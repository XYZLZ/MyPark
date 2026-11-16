import { z } from 'zod';

export const lotStatusSchema = z.enum(['open', 'full', 'closed']);
export type LotStatus = z.output<typeof lotStatusSchema>;

export const lotSummarySchema = z.strictObject({
  id: z.string(),
  name: z.string(),
  slug: z.string(),
});

export const lotSchema = z.strictObject({
  id: z.string(),
  slug: z.string(),
  name: z.string(),
  address: z.string(),
  latitude: z.number().nullable(),
  longitude: z.number().nullable(),
  map_url: z.string(),
  timezone: z.string(),
  capacity: z.number(),
  opening_hours: z.strictObject({ always_open: z.boolean() }),
  status: lotStatusSchema,
});

export type LotSummary = z.output<typeof lotSummarySchema>;
export type Lot = z.output<typeof lotSchema>;
