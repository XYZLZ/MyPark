import { ApiError, parseFieldErrors } from '@mypark/contracts';
import type { i18n as I18n } from 'i18next';
import type { FieldPath, FieldValues, UseFormReturn } from 'react-hook-form';
import { AccessibilityInfo } from 'react-native';

import { translateError } from '@/lib/i18n/translate-error';

const FALLBACK_WAIT_SECONDS = 60;

/** Codes shown under a field instead of in the banner (spec 7.2). */
const FIELD_CODES: Record<string, string> = {
  'auth.code_invalid': 'code',
  'auth.password_incorrect': 'password',
  'vehicle.plate_taken': 'plate',
};

function positiveInteger(value: unknown): number | null {
  if (typeof value === 'number') return Number.isInteger(value) && value > 0 ? value : null;
  if (typeof value === 'string' && /^\d+$/.test(value)) {
    const parsed = Number(value);
    return parsed > 0 ? parsed : null;
  }
  return null;
}

/**
 * Seconds to wait after a 429: `details.retry_after_seconds`, then the
 * `Retry-After` header, then 60. Never NaN and never zero, so the form always locks.
 */
export function retryAfterSeconds(error: ApiError): number {
  return (
    positiveInteger(error.details.retry_after_seconds) ??
    positiveInteger(error.details.retry_after_header) ??
    FALLBACK_WAIT_SECONDS
  );
}

function hasField<T extends FieldValues, O extends FieldValues>(form: UseFormReturn<T, unknown, O>, name: string): boolean {
  return Object.hasOwn(form.getValues(), name);
}

/** The banner text lives in `root.serverError`. It is also announced, because a repeated message does not re-trigger a live region. */
function showInBanner<T extends FieldValues, O extends FieldValues>(form: UseFormReturn<T, unknown, O>, message: string) {
  form.setError('root.serverError', { type: 'server', message });
  AccessibilityInfo.announceForAccessibility(message);
}

/**
 * Puts an API failure on the form (spec 7.1). Validation rules and the field-mapped
 * codes land under their field as a message key (`rule`, `rule:param` or
 * `errors:<code>`), which the field translates when it renders. Everything else
 * becomes the banner text in `root.serverError`, already translated.
 */
export function applyApiError<T extends FieldValues, O extends FieldValues = T>(
  form: UseFormReturn<T, unknown, O>,
  error: unknown,
  i18n: I18n,
): void {
  const apiError =
    error instanceof ApiError ? error : new ApiError({ code: 'internal.unexpected', message: 'not an api error' });

  if (apiError.code === 'validation.failed') {
    const fields = parseFieldErrors(apiError.details);
    let focused = false;
    let leftover = fields.length === 0;
    for (const { field, rule, param } of fields) {
      if (!hasField(form, field)) {
        leftover = true;
        continue;
      }
      form.setError(
        field as FieldPath<T>,
        { type: 'server', message: param ? `${rule}:${param}` : rule },
        { shouldFocus: !focused },
      );
      focused = true;
    }
    if (leftover) showInBanner(form, translateError(i18n, apiError));
    return;
  }

  const field = FIELD_CODES[apiError.code];
  if (field && hasField(form, field)) {
    form.setError(field as FieldPath<T>, { type: 'server', message: `errors:${apiError.code}` }, { shouldFocus: true });
    return;
  }

  if (apiError.code === 'rate_limit.exceeded') {
    const seconds = retryAfterSeconds(apiError);
    showInBanner(
      form,
      translateError(i18n, new ApiError({ code: apiError.code, message: apiError.message, details: { retry_after_seconds: seconds } })),
    );
    return;
  }

  showInBanner(form, translateError(i18n, apiError));
}
