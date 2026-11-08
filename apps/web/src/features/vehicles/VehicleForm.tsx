'use client';

import { vehicleInputSchema, type Vehicle } from '@mypark/contracts';
import { useQueryClient } from '@tanstack/react-query';
import { Banner } from '@/components/ui/Banner';
import { Button, buttonClass } from '@/components/ui/Button';
import { Field, inputClass } from '@/components/ui/Field';
import { PageHeader } from '@/components/ui/PageHeader';
import { Link, useRouter } from '@/i18n/navigation';
import { useApiForm } from '@/lib/forms/use-api-form';
import { createVehicle, updateVehicle, VEHICLES_QUERY_KEY } from './api';

type VehicleField = 'plate' | 'make' | 'model' | 'color';

export function VehicleForm({ mode, vehicle }: { mode: 'create' | 'edit'; vehicle?: Vehicle }) {
  const router = useRouter();
  const queryClient = useQueryClient();

  const { form, t, bannerRef, serverError, fieldMessage, onSubmit, submitting } = useApiForm({
    schema: vehicleInputSchema,
    defaultValues: {
      plate: vehicle?.plate ?? '',
      make: vehicle?.make ?? '',
      model: vehicle?.model ?? '',
      color: vehicle?.color ?? '',
    },
    submit: async (values) => {
      // The plate goes as typed; the server normalizes it.
      await (mode === 'edit' && vehicle ? updateVehicle(vehicle.id, values) : createVehicle(values));
      await queryClient.invalidateQueries({ queryKey: VEHICLES_QUERY_KEY });
      router.push('/vehicles');
    },
  });
  const { register } = form;

  function field(name: VehicleField, label: string, props: { hint?: string; autoCapitalize?: string } = {}) {
    const error = fieldMessage(name);
    return (
      <Field.Root name={name} invalid={Boolean(error)}>
        <Field.Label>{label}</Field.Label>
        {props.hint ? <Field.Hint>{props.hint}</Field.Hint> : null}
        <Field.Control>
          <input
            type="text"
            autoComplete="off"
            spellCheck={false}
            autoCapitalize={props.autoCapitalize}
            className={inputClass}
            {...register(name)}
          />
        </Field.Control>
        <Field.Error message={error} />
      </Field.Root>
    );
  }

  return (
    <div className="mx-auto w-full max-w-md">
      <PageHeader title={mode === 'edit' ? t('vehicles.form.editTitle') : t('vehicles.form.createTitle')} />
      {serverError ? (
        <Banner tone="error" ref={bannerRef}>
          {serverError}
        </Banner>
      ) : null}
      <form noValidate onSubmit={onSubmit} className="rounded-panel bg-surface p-5">
        {field('plate', t('vehicles.form.plate'), { hint: t('vehicles.form.plateHint'), autoCapitalize: 'characters' })}
        {field('make', t('vehicles.form.make'))}
        {field('model', t('vehicles.form.model'))}
        {field('color', t('vehicles.form.color'))}
        <div className="flex flex-wrap gap-3">
          <Button type="submit" loading={submitting} loadingLabel={t('vehicles.form.saving')}>
            {mode === 'edit' ? t('vehicles.form.save') : t('vehicles.form.create')}
          </Button>
          <Link href="/vehicles" className={buttonClass('quiet')}>
            {t('vehicles.form.cancel')}
          </Link>
        </div>
      </form>
    </div>
  );
}
