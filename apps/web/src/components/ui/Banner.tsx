import type { ReactNode, Ref } from 'react';

const TONES = {
  error: { role: 'alert', classes: 'bg-danger-tint text-ink border-danger/40' },
  status: { role: 'status', classes: 'bg-brand-primary-tint text-ink border-brand-primary/30' },
} as const;

/** Message above a form. Focusable by script (`tabIndex={-1}`) so a failed submit can move focus here. */
export function Banner({
  tone,
  children,
  ref,
}: {
  tone: keyof typeof TONES;
  children: ReactNode;
  ref?: Ref<HTMLDivElement>;
}) {
  const { role, classes } = TONES[tone];
  return (
    <div ref={ref} role={role} tabIndex={-1} className={`mb-4 rounded-control border p-3 text-sm outline-none ${classes}`}>
      {children}
    </div>
  );
}
