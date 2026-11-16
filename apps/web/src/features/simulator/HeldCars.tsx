'use client';

import { ApiError, CAR_ID_PATTERN } from '@mypark/contracts';
import { useMutation } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { useEffect, useRef, useState } from 'react';
import { z } from 'zod';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { Field, inputClass } from '@/components/ui/Field';
import { useApiForm } from '@/lib/forms/use-api-form';
import type { Translator } from '@/lib/i18n/translate-error';
import { useConsoleStore } from '@/providers/simulator-store-provider';
import { failureMessage, useNotConfiguredHandler, useRefreshConsole, type ActionHooks } from './action-support';
import { leaveCar } from './api';
import { isNotConfigured } from './controls';

const typedCarSchema = z.object({
  car_id: z.string().trim().min(1, { error: 'required' }).regex(CAR_ID_PATTERN, { error: 'pattern' }),
});

const isCarNotFound = (error: unknown) => error instanceof ApiError && error.code === 'simulator.car_not_found';

/**
 * The cars injected with "hold" (M2B-32) and a typed id, so a reload is not a dead end. A car the simulator
 * does not know (404) leaves the list with a banner. Leave follows `running` (M2B-44), so `disabled` says why.
 */
export function HeldCars({ disabled, reason, onDone, onNotConfigured }: { disabled: boolean; reason?: string } & ActionHooks) {
  const t = useTranslations('simulator.held');
  const tAll: Translator = useTranslations();
  const held = useConsoleStore((state) => state.heldCars);
  const removeHeld = useConsoleStore((state) => state.removeHeld);
  const refresh = useRefreshConsole();
  const notConfigured = useNotConfiguredHandler(onNotConfigured);
  const bannerRef = useRef<HTMLDivElement>(null);
  const [lost, setLost] = useState<Record<string, boolean>>({});

  const leave = useMutation({
    mutationFn: ({ carId, loseTicket }: { carId: string; loseTicket: boolean }) => leaveCar(carId, loseTicket),
    onSuccess: async (_result, { carId }) => {
      removeHeld(carId);
      await refresh();
      onDone?.();
    },
    onError: (error, { carId }) => {
      if (isCarNotFound(error)) removeHeld(carId);
      if (isNotConfigured(error)) {
        onNotConfigured?.();
        void refresh();
      }
    },
  });

  // A failed Leave puts focus on its banner (the button that was pressed may be gone).
  useEffect(() => {
    if (leave.isError) bannerRef.current?.focus();
  }, [leave.isError, leave.submittedAt]);

  const { form: typedForm, bannerRef: typedBannerRef, serverError: typedError, fieldMessage, onSubmit: onTypedSubmit, submitting: typedSubmitting } = useApiForm({
    schema: typedCarSchema,
    defaultValues: { car_id: '' },
    submit: async ({ car_id }, form) => {
      await leaveCar(car_id, false);
      removeHeld(car_id);
      form.reset();
      await refresh();
      onDone?.();
    },
    onError: (error, values, form) => {
      if (isCarNotFound(error)) removeHeld(values.car_id);
      return notConfigured(error, values, form);
    },
  });
  const idError = fieldMessage('car_id');
  const describedBy = disabled && reason ? 'sim-held-reason' : undefined;

  return (
    <section aria-labelledby="sim-held-title" className="rounded-panel bg-surface p-5">
      <h2 id="sim-held-title" className="mb-3 text-lg font-bold text-ink">
        {t('title')}
      </h2>
      {leave.isError ? (
        <Banner tone="error" ref={bannerRef}>
          {failureMessage(tAll, leave.error)}
        </Banner>
      ) : null}
      {held.length === 0 ? (
        <p className="mb-4 text-ink-muted">{t('empty')}</p>
      ) : (
        <ul className="mb-4 grid gap-1">
          {held.map((id) => (
            <li key={id} className="flex flex-wrap items-center gap-x-4">
              <span className="font-mono text-ink">{id}</span>
              <label className="inline-flex min-h-11 items-center gap-2 text-ink">
                <input
                  type="checkbox"
                  aria-label={t('lostTicketFor', { id })}
                  className="size-5 accent-brand-navy"
                  checked={lost[id] ?? false}
                  onChange={(event) => setLost((current) => ({ ...current, [id]: event.target.checked }))}
                />
                {t('lostTicket')}
              </label>
              <Button
                className="ml-auto h-11 px-5 text-sm"
                disabled={disabled}
                aria-label={t('leaveCar', { id })}
                aria-describedby={describedBy}
                loading={leave.isPending && leave.variables.carId === id}
                loadingLabel={t('leave')}
                onClick={() => leave.mutate({ carId: id, loseTicket: lost[id] ?? false })}
              >
                {t('leave')}
              </Button>
            </li>
          ))}
        </ul>
      )}
      <form noValidate onSubmit={onTypedSubmit}>
        {typedError ? (
          <Banner tone="error" ref={typedBannerRef}>
            {typedError}
          </Banner>
        ) : null}
        <div className="flex items-end gap-3">
          <div className="grow">
            <Field.Root name="car_id" invalid={Boolean(idError)}>
              <Field.Label>{`${t('other')} ${t('otherHint')}`}</Field.Label>
              <Field.Control>
                <input
                  type="text"
                  autoComplete="off"
                  spellCheck={false}
                  placeholder={t('otherPlaceholder')}
                  className={`${inputClass} font-mono`}
                  {...typedForm.register('car_id')}
                />
              </Field.Control>
              <Field.Error message={idError} />
            </Field.Root>
          </div>
          <Button
            type="submit"
            variant="outline"
            className="mb-4 h-12"
            disabled={disabled}
            aria-describedby={describedBy}
            loading={typedSubmitting}
            loadingLabel={t('leaveOther')}
          >
            {t('leaveOther')}
          </Button>
        </div>
      </form>
      {disabled && reason ? (
        <p id="sim-held-reason" className="text-sm text-ink-muted">
          {reason}
        </p>
      ) : null}
    </section>
  );
}
