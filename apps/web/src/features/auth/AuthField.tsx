'use client';

import type { ComponentProps, ReactNode } from 'react';
import { Field } from '@/components/ui/Field';

/** A labelled control with an optional hint and its error message. The control is the single child. */
export function AuthField({
  name,
  label,
  error,
  hint,
  children,
}: {
  name: string;
  label?: ReactNode;
  error?: string;
  hint?: ReactNode;
  children: ComponentProps<typeof Field.Control>['children'];
}) {
  return (
    <Field.Root name={name} invalid={Boolean(error)}>
      {label ? <Field.Label>{label}</Field.Label> : null}
      {hint ? <Field.Hint>{hint}</Field.Hint> : null}
      <Field.Control>{children}</Field.Control>
      <Field.Error message={error} />
    </Field.Root>
  );
}
