'use client';

import { injectCarSchema } from '@mypark/contracts';
import { useState } from 'react';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { Field, inputClass } from '@/components/ui/Field';
import { useApiForm } from '@/lib/forms/use-api-form';
import { useConsoleStore } from '@/providers/simulator-store-provider';
import { useNotConfiguredHandler, useRefreshConsole, type ActionHooks } from './action-support';
import { injectCar } from './api';

const CREDENTIALS = [
  ['ticket', 'ticket'],
  ['employee_card', 'employeeCard'],
] as const;

/** An empty text box means "not given", so the contract's own rules (`required`, `pattern`) decide. */
const blankToUndefined = (value: unknown) => (typeof value === 'string' && value.trim() === '' ? undefined : value);

/** Works while idle too: the simulator starts an implicit idle session (M2B-44). `reason` says why it is disabled. */
export function InjectForm({ disabled, reason, onDone, onNotConfigured }: { disabled: boolean; reason?: string } & ActionHooks) {
  const addHeld = useConsoleStore((state) => state.addHeld);
  const refresh = useRefreshConsole();
  const notConfigured = useNotConfiguredHandler(onNotConfigured);
  const [added, setAdded] = useState<string | null>(null);
  const { form, t, bannerRef, serverError, fieldMessage, onSubmit, submitting } = useApiForm({
    schema: injectCarSchema,
    defaultValues: { credential: 'ticket', hold: false },
    submit: async (values) => {
      setAdded(null);
      const id = await injectCar(values);
      if (values.hold) addHeld(id);
      setAdded(id);
      await refresh();
      onDone?.();
    },
    onError: notConfigured,
  });
  const { register, watch } = form;
  const employee = watch('credential') === 'employee_card';
  const cardError = fieldMessage('card');
  const dwellError = fieldMessage('dwell_minutes');

  return (
    <form noValidate onSubmit={onSubmit} aria-labelledby="sim-inject-title" className="rounded-panel bg-surface p-5">
      <h2 id="sim-inject-title" className="mb-3 text-lg font-bold text-ink">
        {t('simulator.inject.title')}
      </h2>
      {serverError ? (
        <Banner tone="error" ref={bannerRef}>
          {serverError}
        </Banner>
      ) : null}
      <fieldset className="mb-4">
        <legend className="mb-1.5 text-sm font-medium text-ink">{t('simulator.inject.credential')}</legend>
        <div className="flex flex-wrap gap-x-5">
          {CREDENTIALS.map(([value, key]) => (
            <label key={value} className="inline-flex min-h-11 items-center gap-2 text-ink">
              <input type="radio" value={value} className="size-5 accent-brand-navy" {...register('credential')} />
              {t(`simulator.inject.${key}`)}
            </label>
          ))}
        </div>
      </fieldset>
      <div className="grid grid-cols-2 gap-3">
        {employee ? (
          <Field.Root name="card" invalid={Boolean(cardError)}>
            <Field.Label>{t('simulator.inject.card')}</Field.Label>
            <Field.Control>
              <input
                type="text"
                autoComplete="off"
                spellCheck={false}
                placeholder={t('simulator.inject.cardPlaceholder')}
                className={`${inputClass} font-mono`}
                {...register('card', { setValueAs: blankToUndefined, shouldUnregister: true })}
              />
            </Field.Control>
            <Field.Error message={cardError} />
          </Field.Root>
        ) : null}
        <Field.Root name="dwell_minutes" invalid={Boolean(dwellError)}>
          <Field.Label>{`${t('simulator.inject.dwell')} ${t('simulator.inject.dwellRange')}`}</Field.Label>
          <Field.Control>
            <input
              type="number"
              min={1}
              max={600}
              step={1}
              inputMode="numeric"
              placeholder={t('simulator.inject.dwellPlaceholder')}
              className={inputClass}
              {...register('dwell_minutes', { setValueAs: (value: unknown) => (blankToUndefined(value) === undefined ? undefined : Number(value)) })}
            />
          </Field.Control>
          <Field.Error message={dwellError} />
        </Field.Root>
      </div>
      <label className="mb-4 flex min-h-11 items-center gap-2 text-ink">
        <input type="checkbox" className="size-5 accent-brand-navy" {...register('hold')} />
        {t('simulator.inject.hold')}
      </label>
      <Button
        type="submit"
        className="w-full"
        disabled={disabled}
        loading={submitting}
        loadingLabel={t('simulator.inject.submit')}
        aria-describedby={disabled && reason ? 'sim-inject-reason' : undefined}
      >
        {t('simulator.inject.submit')}
      </Button>
      {disabled && reason ? (
        <p id="sim-inject-reason" className="mt-2 text-sm text-ink-muted">
          {reason}
        </p>
      ) : null}
      <p role="status" className="mt-2 text-sm font-semibold text-ink">
        {added ? t('simulator.inject.added', { id: added }) : ''}
      </p>
    </form>
  );
}
