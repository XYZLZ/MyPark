import { parseIssueMessage, type ApiError } from '@mypark/contracts';

/** The part of next-intl's `t` (created without a namespace) that these helpers need. */
export type Translator = {
  (key: string, values?: Record<string, string | number>): string;
  has(key: string): boolean;
};

function translateCode(t: Translator, code: string, values?: Record<string, string | number>): string {
  const key = `errors.${code}`;
  return t.has(key) ? t(key, values) : t('errors.fallback');
}

/** Translates an ApiError by its code. `ApiError.message` is never shown. */
export function translateError(t: Translator, error: ApiError): string {
  const values: Record<string, string | number> = {};
  const { retry_after_seconds: seconds, limit } = error.details;
  if (typeof seconds === 'number') values.seconds = seconds;
  if (typeof limit === 'number') values.limit = limit;
  return translateCode(t, error.code, values);
}

/** A field message is `rule`, `rule:param` (validation) or `errors:<code>` (a code shown under a field). */
export function translateFieldMessage(t: Translator, message: string): string {
  if (message.startsWith('errors:')) return translateCode(t, message.slice('errors:'.length));
  const { rule, param } = parseIssueMessage(message);
  return t(`validation.${rule}`, param === undefined ? undefined : { param });
}
