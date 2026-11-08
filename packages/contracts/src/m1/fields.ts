import { z } from 'zod';
import { isValidPlate, normalizePhone } from './normalize';

/**
 * Shared field rules (spec §8.3). Issue messages are `rule` or `rule:param`,
 * the same vocabulary the server uses in `details.fields[].rule`.
 */

/** A required string: empty reports `required`, then `min`/`max` apply. */
function bounded(max: number, opts: { min?: number; trim?: boolean } = {}) {
  const { min = 1, trim = false } = opts;
  const first = trim ? z.string({ error: 'required' }).trim() : z.string({ error: 'required' });
  const range = z.string().min(min, { error: `min:${min}`, abort: true }).max(max, { error: `max:${max}`, abort: true });
  return first.min(1, { error: 'required', abort: true }).pipe(range);
}

export const emailField = z
  .string({ error: 'required' })
  .trim()
  .min(1, { error: 'required' })
  .pipe(z.string().max(254, { error: 'max:254' }).pipe(z.email({ error: 'email' })));

/** New passwords: 10 to 128 characters. */
export const newPasswordField = bounded(128, { min: 10 });
/** Existing passwords (login, delete): no minimum. */
export const currentPasswordField = bounded(128);

export const firstNameField = bounded(60, { trim: true });
export const lastNameField = bounded(80, { trim: true });

/** Normalizes before the regex. An empty result is valid. */
export const phoneField = z
  .string({ error: 'required' })
  .transform(normalizePhone)
  .pipe(z.string().regex(/^(\+[1-9][0-9]{6,14})?$/, { error: 'phone' }));

export const codeField = z.string({ error: 'required' }).regex(/^[0-9]{6}$/, { error: 'code6' });

/** Length is checked on the typed value; the output is the typed value too. */
export const plateField = bounded(15, { min: 2 }).refine(isValidPlate, { error: 'plate' });

export const makeField = bounded(50);
export const modelField = bounded(50);
export const colorField = bounded(30);
