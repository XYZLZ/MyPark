'use client';

import { cloneElement, createContext, use, useId, type ReactElement, type ReactNode } from 'react';

type FieldContext = { name: string; invalid: boolean; controlId: string; hintId: string; errorId: string };

const FieldCtx = createContext<FieldContext | null>(null);

function useField(): FieldContext {
  const ctx = use(FieldCtx);
  if (!ctx) throw new Error('Field parts must be rendered inside <Field.Root>');
  return ctx;
}

/** Classes for a text input inside a Field: 2 px `brand-primary` border on focus (CC-11), `danger` when invalid. */
export const inputClass =
  'block h-12 w-full rounded-control border border-control-line bg-surface px-4 text-ink outline-none ' +
  'transition-[border-color,box-shadow] duration-(--motion-duration) hover:border-ink-muted ' +
  'focus:border-2 focus:border-brand-primary focus:px-[15px] focus:shadow-[0_0_0_3px_var(--color-brand-primary-tint)] ' +
  'aria-invalid:border-2 aria-invalid:border-danger aria-invalid:px-[15px] ' +
  'disabled:border-hairline disabled:bg-disabled-bg disabled:text-disabled-ink';

function Root({ name, invalid = false, children }: { name: string; invalid?: boolean; children: ReactNode }) {
  const id = useId();
  const value: FieldContext = { name, invalid, controlId: `${id}-control`, hintId: `${id}-hint`, errorId: `${id}-error` };
  return (
    <FieldCtx value={value}>
      <div className="mb-4 flex flex-col gap-1.5">{children}</div>
    </FieldCtx>
  );
}

function Label({ children }: { children: ReactNode }) {
  const { controlId } = useField();
  return (
    <label htmlFor={controlId} className="text-sm font-medium text-ink">
      {children}
    </label>
  );
}

type ControlInjected = { id: string; name: string; 'aria-invalid'?: true; 'aria-describedby'?: string };

/** Slot: clones its single child, adding `id`, `name`, `aria-invalid` and `aria-describedby`. */
function Control({ children }: { children: ReactElement<Partial<ControlInjected>> }) {
  const { name, invalid, controlId, hintId, errorId } = useField();
  const describedBy = [children.props['aria-describedby'], hintId, invalid ? errorId : undefined]
    .filter(Boolean)
    .join(' ');
  return cloneElement(children, {
    id: controlId,
    name,
    'aria-invalid': invalid ? true : undefined,
    'aria-describedby': describedBy || undefined,
  });
}

function Hint({ children }: { children: ReactNode }) {
  const { hintId } = useField();
  return (
    <p id={hintId} className="text-sm text-ink-muted">
      {children}
    </p>
  );
}

/** Renders only while the field is invalid and has a message. */
function FieldError({ message }: { message?: string }) {
  const { invalid, errorId } = useField();
  if (!invalid || !message) return null;
  return (
    <p id={errorId} className="text-sm font-medium text-danger">
      {message}
    </p>
  );
}

export const Field = { Root, Label, Control, Hint, Error: FieldError };
