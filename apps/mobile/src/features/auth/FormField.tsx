import { useTranslation } from 'react-i18next';
import { Controller, type Control, type FieldPath, type FieldValues } from 'react-hook-form';

import { TextField } from '@/components/ui/TextField';
import { translateFieldMessage } from '@/lib/i18n/translate-error';

/** A pasted code often carries spaces or a line break. */
export const stripWhitespace = (value: string) => value.replace(/\s+/g, '');

type FieldProps = Parameters<typeof TextField>[0];

type Props<T extends FieldValues, O extends FieldValues> = Omit<FieldProps, 'value' | 'onChangeText' | 'onBlur' | 'error' | 'ref'> & {
  control: Control<T, unknown, O>;
  name: FieldPath<T>;
  /** Cleans a value as it is typed (a pasted code loses its spaces). */
  clean?: (value: string) => string;
};

/**
 * A TextField bound to a react-hook-form field. The field's ref is forwarded, so `setFocus` and
 * `shouldFocus` reach the input, and the error message key is translated here when it renders.
 */
export function FormField<T extends FieldValues, O extends FieldValues>({ control, name, clean, ...inputProps }: Props<T, O>) {
  const { i18n } = useTranslation();
  return (
    <Controller
      control={control as unknown as Control<FieldValues>}
      name={name}
      render={({ field, fieldState }) => (
        <TextField
          {...inputProps}
          ref={field.ref}
          value={String(field.value ?? '')}
          onChangeText={(value) => field.onChange(clean ? clean(value) : value)}
          onBlur={field.onBlur}
          error={fieldState.error?.message ? translateFieldMessage(i18n, fieldState.error.message) : undefined}
        />
      )}
    />
  );
}
