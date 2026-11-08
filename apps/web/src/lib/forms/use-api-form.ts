'use client';

import { zodResolver } from '@hookform/resolvers/zod';
import { ApiError } from '@mypark/contracts';
import { useTranslations } from 'next-intl';
import { useEffect, useRef, type SubmitEvent } from 'react';
import { useForm, type DefaultValues, type FieldValues, type UseFormReturn } from 'react-hook-form';
import type { z } from 'zod';
import { translateFieldMessage, type Translator } from '@/lib/i18n/translate-error';
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
 * The form behavior every auth screen shares (spec 7.1): react-hook-form with the
 * shared zod schema, one request in flight, API errors mapped onto fields or the
 * banner, a 429 that locks submit, and focus on the banner when no field is at fault.
 */
export function useApiForm<I extends FieldValues, O extends FieldValues>({ schema, defaultValues, submit, onError }: Options<I, O>) {
  const form = useForm<I, unknown, O>({ resolver: zodResolver(schema), defaultValues });
  const t: Translator = useTranslations();
  const lock = useRetryLock();
  const bannerRef = useRef<HTMLDivElement>(null);
  const inFlight = useRef(false);
  const waiting = useRef(false);

  const { errors, submitCount } = form.formState;
  const serverError = errors.root?.serverError?.message;
  const fieldFailed = Object.keys(errors).some((name) => name !== 'root');

  // Banners take focus when no field is at fault (spec 11).
  useEffect(() => {
    if (serverError && !fieldFailed) bannerRef.current?.focus();
  }, [serverError, fieldFailed, submitCount]);

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
    applyApiError(form, error, t);
    if (error instanceof ApiError && error.code === 'rate_limit.exceeded') {
      waiting.current = true;
      lock.lockFor(retryAfterSeconds(error));
    }
  }

  const run = (event: SubmitEvent<HTMLFormElement>) =>
    form.handleSubmit(async (values) => {
      try {
        await submit(values, form);
      } catch (error) {
        if (error instanceof ApiError && onError?.(error, values, form)) return;
        fail(error);
      }
    })(event);

  function onSubmit(event: SubmitEvent<HTMLFormElement>) {
    // The guard wraps validation too, so a second click cannot start a second request.
    if (inFlight.current || locked) {
      event.preventDefault();
      return;
    }
    inFlight.current = true;
    void run(event).finally(() => {
      inFlight.current = false;
    });
  }

  function fieldMessage(name: string): string | undefined {
    const message = (errors as Record<string, { message?: unknown } | undefined>)[name]?.message;
    return typeof message === 'string' ? translateFieldMessage(t, message) : undefined;
  }

  return { form, t, lock, bannerRef, serverError, fieldMessage, onSubmit, fail, submitting: form.formState.isSubmitting };
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
