import type { ComponentProps, ReactNode } from 'react';

/** Native checkbox with its label; the whole row is the click target (min 44 px). */
export function Checkbox({ label, ...rest }: Omit<ComponentProps<'input'>, 'type'> & { label: ReactNode }) {
  return (
    <label className="flex min-h-11 items-center gap-3 text-ink">
      <input
        {...rest}
        type="checkbox"
        className="size-5 shrink-0 accent-brand-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand-primary"
      />
      <span>{label}</span>
    </label>
  );
}
