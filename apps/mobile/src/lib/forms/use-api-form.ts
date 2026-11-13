import { zodResolver } from '@hookform/resolvers/zod';
import { ApiError } from '@mypark/contracts';
import { useEffect, useRef } from 'react';
import { useForm, type DefaultValues, type FieldValues, type UseFormReturn } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import type { z } from 'zod';

import { applyApiError, retryAfterSeconds } from './apply-api-error';
import { useRetryLock } from './use-retry-lock';

type Form<I extends FieldValues, O extends FieldValues> = UseFormReturn<I, unknown, O>;

type Options<I extends FieldValues, O extends FieldValues> = {
  schema: z.ZodType<O, I>;
  defaultValues: DefaultValues<I>;
  /** Runs with the validated values. Throw an ApiError to show it on the form. */
  submit: (values: O, form: Form<I, O>) => Promise<void>;
  /** Return true when the failure was handled here and needs no banner or field error. */
  onError?: (error: ApiError, values: O, form: Form<I, O>) => boolean;
};

/**
 * The form behavior every auth screen shares (spec 7.1): react-hook-form with the shared zod schema,
 * one request in flight, API errors mapped onto fields or the banner, and a 429 that locks submit.
 */
export function useApiForm<I extends FieldValues, O extends FieldValues>({ schema, defaultValues, submit, onError }: Options<I, O>) {
  const form = useForm<I, unknown, O>({ resolver: zodResolver(schema), defaultValues });
  const { i18n } = useTranslation();
  const lock = useRetryLock();
  const inFlight = useRef(false);
  const waiting = useRef(false);

  const serverError = form.formState.errors.root?.serverError?.message;

  // A wait message is stale once the wait is over.
  const { locked } = lock;
  const { clearErrors } = form;
  useEffect(() => {
    if (!locked && waiting.current) {
      waiting.current = false;
      clearErrors('root.serverError');
    }
  }, [locked, clearErrors]);

  /** Shows a failure on the form, and locks submit for the wait time of a 429. */
  function fail(error: unknown) {
    applyApiError(form, error, i18n);
    if (error instanceof ApiError && error.code === 'rate_limit.exceeded') {
      waiting.current = true;
      lock.lockFor(retryAfterSeconds(error));
    }
  }

  function onSubmit(): Promise<void> {
    // The guard wraps validation too, so a second tap cannot start a second request.
    if (inFlight.current || locked) return Promise.resolve();
    inFlight.current = true;
    return form
      .handleSubmit(async (values) => {
        try {
          await submit(values, form);
        } catch (error) {
          if (error instanceof ApiError && onError?.(error, values, form)) return;
          fail(error);
        }
      })()
      .finally(() => {
        inFlight.current = false;
      });
  }

  return { form, lock, serverError, onSubmit, fail, submitting: form.formState.isSubmitting };
}

/**
 * Anti-enumeration (spec 7.2): for "send me a code" requests, any 4xx other than
 * a validation failure or a rate limit looks the same as success, so the screen
 * never tells whether an account exists.
 */
export function isUndisclosedOutcome(error: unknown): boolean {
  if (!(error instanceof ApiError) || error.status === undefined) return false;
  if (error.code === 'validation.failed' || error.code === 'rate_limit.exceeded') return false;
  return error.status >= 400 && error.status < 500;
}
