import { z } from 'zod';

// Hand-written single-line literals: scripts/check-i18n-parity.mjs reads them with a regex.
export const VALIDATION_RULES = ['required', 'email', 'min', 'max', 'oneof', 'phone', 'code6', 'password_common', 'plate', 'unknown'] as const;
export type ValidationRule = (typeof VALIDATION_RULES)[number];

export const CLIENT_ERROR_CODES = ['client.network', 'client.timeout'] as const;
export type ClientErrorCode = (typeof CLIENT_ERROR_CODES)[number];

/** One entry of `details.fields` in a `validation.failed` response. The server sends `param: ""` when there is none. */
export const validationFieldErrorSchema = z.object({
  field: z.string(),
  rule: z.string(),
  param: z.string().optional(),
});

export type FieldError = { field: string; rule: string; param?: string };

/** Reads `details.fields`, dropping malformed entries and empty params. */
export function parseFieldErrors(details: Record<string, unknown>): FieldError[] {
  const fields = details.fields;
  if (!Array.isArray(fields)) return [];
  const out: FieldError[] = [];
  for (const entry of fields) {
    const parsed = validationFieldErrorSchema.safeParse(entry);
    if (!parsed.success) continue;
    const { field, rule, param } = parsed.data;
    out.push(param ? { field, rule, param } : { field, rule });
  }
  return out;
}

const knownRules: readonly string[] = VALIDATION_RULES;

/** Splits a zod issue message (`rule` or `rule:param`). An unlisted rule maps to `unknown`. */
export function parseIssueMessage(message: string): { rule: ValidationRule; param?: string } {
  const at = message.indexOf(':');
  const rule = at === -1 ? message : message.slice(0, at);
  if (rule === 'unknown' || !knownRules.includes(rule)) return { rule: 'unknown' };
  return at === -1 ? { rule: rule as ValidationRule } : { rule: rule as ValidationRule, param: message.slice(at + 1) };
}
