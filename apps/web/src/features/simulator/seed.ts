import { CONSOLE_SCENARIOS } from '@mypark/contracts';
import { z } from 'zod';

const MAX_SEED = 9223372036854775807n;
const LOCAL_DATE_TIME = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/;

/** The exact digits of `"seed":<n>` in a response body, before JSON.parse rounds anything above 2^53 (M2B-33). */
export function extractRawSeed(text: string): string | null {
  return /"seed"\s*:\s*(\d+)/.exec(text)?.[1] ?? null;
}

/** The start form: the seed is a digit string, and the start time is a local date-time. Messages are validation rules. */
export const startFormSchema = z.object({
  scenario: z.enum(CONSOLE_SCENARIOS, { error: 'oneof' }),
  speed: z.number({ error: 'required' }).int({ error: 'integer' }).min(1, { error: 'min:1' }).max(120, { error: 'max:120' }).default(30),
  seed: z.string().superRefine((value, ctx) => {
    if (value === '') return;
    if (!/^\d+$/.test(value)) ctx.addIssue({ code: 'custom', message: 'integer' });
    else if (BigInt(value) > MAX_SEED) ctx.addIssue({ code: 'custom', message: `max:${MAX_SEED}` });
  }),
  start_at: z.string().refine((value) => value === '' || LOCAL_DATE_TIME.test(value), { error: 'pattern' }),
});

export type StartFormValues = z.output<typeof startFormSchema>;

/**
 * The request body text. The seed digits are written unquoted so a copied seed replays exactly,
 * and the start time is Santo Domingo (UTC-4, no daylight saving).
 */
export function toStartBody(values: StartFormValues): string {
  const parts = [`"scenario":${JSON.stringify(values.scenario)}`, `"speed":${values.speed}`];
  // BigInt drops leading zeros, which JSON does not allow.
  if (values.seed !== '') parts.push(`"seed":${BigInt(values.seed)}`);
  if (values.start_at !== '') parts.push(`"start_at":${JSON.stringify(`${values.start_at}:00-04:00`)}`);
  return `{${parts.join(',')}}`;
}
