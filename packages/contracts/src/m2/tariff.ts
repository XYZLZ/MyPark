import { z } from 'zod';

export const tariffSchema = z.strictObject({
  effective_from: z.string(),
  currency: z.string(),
  first_hour_cents: z.number(),
  additional_hour_cents: z.number(),
  exit_grace_minutes: z.number(),
  post_payment_exit_minutes: z.number(),
  daily_cap_cents: z.number(),
  lost_ticket_cents: z.number(),
  reservation_fee_cents: z.number(),
});

export type Tariff = z.output<typeof tariffSchema>;
