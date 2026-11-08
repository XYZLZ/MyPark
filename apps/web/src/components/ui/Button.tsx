import type { ComponentProps, MouseEvent } from 'react';

export type ButtonVariant = 'primary' | 'onDark' | 'quiet' | 'danger';

const BASE =
  'inline-flex h-12 items-center justify-center gap-2 rounded-full px-6 font-semibold ' +
  'transition-[background-color,color,scale,box-shadow] duration-(--motion-duration) ease-(--motion-ease) ' +
  'outline-none active:scale-[.98] disabled:cursor-not-allowed disabled:bg-disabled-bg disabled:text-disabled-ink';

// Focus (spec §9.3): 2 px surface gap + 3 px primary; on navy a 3 px surface ring only (CC-11).
const RING = 'focus-visible:shadow-[0_0_0_2px_var(--color-surface),0_0_0_5px_var(--color-brand-primary)]';
const RING_ON_DARK = 'focus-visible:shadow-[0_0_0_3px_var(--color-surface)]';

const VARIANTS: Record<ButtonVariant, string> = {
  primary: `bg-brand-action text-brand-action-contrast hover:bg-brand-action-hover active:bg-brand-action-press ${RING}`,
  onDark: `border-2 border-current bg-transparent text-brand-primary-contrast hover:bg-white/10 ${RING_ON_DARK}`,
  quiet: `bg-transparent text-brand-primary hover:bg-brand-primary-tint ${RING}`,
  danger: `bg-danger text-white hover:brightness-110 ${RING}`,
};

/** Class names for a Button look, so links can share it (`<Link className={buttonClass('primary')}>`). */
export function buttonClass(variant: ButtonVariant = 'primary'): string {
  return `${BASE} ${VARIANTS[variant]}`;
}

type ButtonProps = ComponentProps<'button'> & {
  variant?: ButtonVariant;
  /** Shows a spinner and `loadingLabel`, ignores clicks, but stays enabled so focus is not lost. */
  loading?: boolean;
  loadingLabel?: string;
};

export function Button({
  variant = 'primary',
  loading = false,
  loadingLabel,
  type = 'button',
  className,
  onClick,
  children,
  ...rest
}: ButtonProps) {
  function handleClick(event: MouseEvent<HTMLButtonElement>) {
    if (loading) {
      // Also stops the form submit that a type="submit" click would trigger.
      event.preventDefault();
      return;
    }
    onClick?.(event);
  }

  return (
    <button
      {...rest}
      type={type}
      aria-busy={loading || undefined}
      onClick={handleClick}
      className={className ? `${buttonClass(variant)} ${className}` : buttonClass(variant)}
    >
      {loading ? (
        <>
          <span
            aria-hidden="true"
            className="size-4 animate-spin rounded-full border-2 border-current border-t-transparent"
          />
          {loadingLabel ?? children}
        </>
      ) : (
        children
      )}
    </button>
  );
}
