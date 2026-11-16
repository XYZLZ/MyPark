'use client';

import { CONSOLE_SCENARIOS } from '@mypark/contracts';
import type { ReactElement } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { Field, inputClass } from '@/components/ui/Field';
import { useApiForm } from '@/lib/forms/use-api-form';
import { simulatorKeys, startScenario } from './api';
import { startFormSchema } from './seed';

type Name = 'scenario' | 'speed' | 'seed' | 'start_at';

/** The disabled reason is text next to the button, not only a dimmed button. */
export function StartForm({ disabled, reason }: { disabled: boolean; reason?: string }) {
  const queryClient = useQueryClient();
  const { form, t, bannerRef, serverError, fieldMessage, onSubmit, submitting } = useApiForm({
    schema: startFormSchema,
    defaultValues: { scenario: 'weekday', speed: 30, seed: '', start_at: '' },
    submit: async (values) => {
      await startScenario(values);
      await queryClient.invalidateQueries({ queryKey: simulatorKeys.status });
    },
  });
  const { register } = form;

  function field(name: Name, label: string, control: (props: ReturnType<typeof register>) => ReactElement<Record<string, unknown>>, hint?: string) {
    const error = fieldMessage(name);
    return (
      <Field.Root name={name} invalid={Boolean(error)}>
        <Field.Label>{label}</Field.Label>
        <Field.Control>{control(register(name, name === 'speed' ? { valueAsNumber: true } : undefined))}</Field.Control>
        {hint ? <Field.Hint>{hint}</Field.Hint> : null}
        <Field.Error message={error} />
      </Field.Root>
    );
  }

  return (
    <form noValidate onSubmit={onSubmit} aria-labelledby="sim-start-title" className="rounded-panel bg-surface p-5">
      <h2 id="sim-start-title" className="mb-3 text-lg font-bold text-ink">
        {t('simulator.start.title')}
      </h2>
      {serverError ? (
        <Banner tone="error" ref={bannerRef}>
          {serverError}
        </Banner>
      ) : null}
      {field('scenario', t('simulator.start.scenario'), (props) => (
        <select className={inputClass} {...props}>
          {CONSOLE_SCENARIOS.map((scenario) => (
            <option key={scenario} value={scenario}>
              {t(`simulator.scenario.${scenario}`)}
            </option>
          ))}
        </select>
      ))}
      <div className="grid grid-cols-2 gap-3">
        {field('speed', `${t('simulator.start.speed')} ${t('simulator.start.speedRange')}`, (props) => (
          <input type="number" min={1} max={120} step={1} inputMode="numeric" className={inputClass} {...props} />
        ))}
        {field('seed', `${t('simulator.start.seed')} ${t('simulator.start.optional')}`, (props) => (
          <input
            type="text"
            inputMode="numeric"
            autoComplete="off"
            spellCheck={false}
            placeholder={t('simulator.start.seedPlaceholder')}
            className={`${inputClass} font-mono`}
            {...props}
          />
        ))}
      </div>
      {field(
        'start_at',
        `${t('simulator.start.startAt')} ${t('simulator.start.optional')}`,
        (props) => <input type="datetime-local" className={inputClass} {...props} />,
        t('simulator.start.startAtHint'),
      )}
      <Button
        type="submit"
        className="w-full"
        disabled={disabled}
        loading={submitting}
        loadingLabel={t('simulator.start.submit')}
        aria-describedby={disabled && reason ? 'sim-start-reason' : undefined}
      >
        {t('simulator.start.submit')}
      </Button>
      {disabled && reason ? (
        <p id="sim-start-reason" className="mt-2 text-sm text-ink-muted">
          {reason}
        </p>
      ) : null}
    </form>
  );
}
