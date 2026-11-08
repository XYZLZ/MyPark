import { parseIssueMessage, type ApiError } from '@mypark/contracts';
import type { i18n as I18n } from 'i18next';

type Values = Record<string, string | number>;

function translateCode(i18n: I18n, code: string, values?: Values): string {
  const key = `errors.${code}`;
  return i18n.exists(key) ? i18n.t(key, values) : i18n.t('errors.fallback');
}

/** Translates an ApiError by its code. `ApiError.message` is never shown. */
export function translateError(i18n: I18n, error: ApiError): string {
  const values: Values = {};
  const { retry_after_seconds: seconds, limit } = error.details;
  if (typeof seconds === 'number') values.seconds = seconds;
  if (typeof limit === 'number') values.limit = limit;
  return translateCode(i18n, error.code, values);
}

/** A field message is `rule`, `rule:param` (validation) or `errors:<code>` (a code shown under a field). */
export function translateFieldMessage(i18n: I18n, message: string): string {
  if (message.startsWith('errors:')) return translateCode(i18n, message.slice('errors:'.length));
  const { rule, param } = parseIssueMessage(message);
  const key = `validation.${rule}`;
  return i18n.exists(key) ? i18n.t(key, param === undefined ? undefined : { param }) : i18n.t('validation.unknown');
}
