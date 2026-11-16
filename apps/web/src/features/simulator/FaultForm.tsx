'use client';

import { SPACE_CODE_PATTERN, sensorFaultSchema } from '@mypark/contracts';
import { useState } from 'react';
import { z } from 'zod';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { Field, inputClass } from '@/components/ui/Field';
import { useApiForm } from '@/lib/forms/use-api-form';
import { useNotConfiguredHandler, useRefreshConsole, type ActionHooks } from './action-support';
import { setFault } from './api';

const faultFormSchema = z.object({
  code: z.string().trim().min(1, { error: 'required' }).regex(SPACE_CODE_PATTERN, { error: 'pattern' }),
  fault: sensorFaultSchema.shape.fault,
});

/** A sensor fault on one space. The simulator answers 409 while nothing runs, so it is enabled only while running (M2B-44). */
export function FaultForm({ disabled, reason, onDone, onNotConfigured }: { disabled: boolean; reason?: string } & ActionHooks) {
  const refresh = useRefreshConsole();
  const notConfigured = useNotConfiguredHandler(onNotConfigured);
  const [applied, setApplied] = useState<string | null>(null);
  const { form, t, bannerRef, serverError, fieldMessage, onSubmit, submitting } = useApiForm({
    schema: faultFormSchema,
    defaultValues: { code: '', fault: 'stuck_occupied' },
    submit: async ({ code, fault }) => {
      setApplied(null);
      await setFault(code, fault);
      setApplied(code);
      await refresh();
      onDone?.();
    },
    onError: notConfigured,
  });
  const { register } = form;
  const codeError = fieldMessage('code');
  const faultError = fieldMessage('fault');

  return (
    <form noValidate onSubmit={onSubmit} aria-labelledby="sim-fault-title" className="rounded-panel bg-surface p-5">
      <h2 id="sim-fault-title" className="mb-3 text-lg font-bold text-ink">
        {t('simulator.fault.title')}
      </h2>
      {serverError ? (
        <Banner tone="error" ref={bannerRef}>
          {serverError}
        </Banner>
      ) : null}
      <div className="grid grid-cols-2 gap-3">
        <Field.Root name="code" invalid={Boolean(codeError)}>
          <Field.Label>{t('simulator.fault.code')}</Field.Label>
          <Field.Control>
            <input type="text" autoComplete="off" spellCheck={false} placeholder="E017" className={`${inputClass} font-mono`} {...register('code')} />
          </Field.Control>
          <Field.Error message={codeError} />
        </Field.Root>
        <Field.Root name="fault" invalid={Boolean(faultError)}>
          <Field.Label>{t('simulator.fault.fault')}</Field.Label>
          <Field.Control>
            <select className={inputClass} {...register('fault')}>
              {sensorFaultSchema.shape.fault.options.map((kind) => (
                <option key={kind} value={kind}>
                  {t(`simulator.fault.kind.${kind}`)}
                </option>
              ))}
            </select>
          </Field.Control>
          <Field.Error message={faultError} />
        </Field.Root>
      </div>
      <Button
        type="submit"
        className="w-full"
        disabled={disabled}
        loading={submitting}
        loadingLabel={t('simulator.fault.submit')}
        aria-describedby={disabled && reason ? 'sim-fault-reason' : undefined}
      >
        {t('simulator.fault.submit')}
      </Button>
      {disabled && reason ? (
        <p id="sim-fault-reason" className="mt-2 text-sm text-ink-muted">
          {reason}
        </p>
      ) : null}
      <p role="status" className="mt-2 text-sm font-semibold text-ink">
        {applied ? t('simulator.fault.applied', { code: applied }) : ''}
      </p>
    </form>
  );
}
