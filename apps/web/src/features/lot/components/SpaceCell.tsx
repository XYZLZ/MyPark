import type { SpaceStatus } from '@mypark/contracts';

/** The top-down car: a body in the occupied token with a lighter windscreen (design §9.3). */
export function CarShape({ className = 'absolute inset-[9%_13%] size-auto' }: { className?: string }) {
  return (
    <svg data-shape="car" viewBox="0 0 12 18" aria-hidden="true" focusable="false" className={className} preserveAspectRatio="none">
      <rect x="0" y="0" width="12" height="18" rx="4" className="fill-space-occupied" />
      <rect x="3" y="4" width="6" height="3" rx="0.8" className="fill-surface/80" />
    </svg>
  );
}

const HATCH =
  'repeating-linear-gradient(45deg, var(--color-surface) 0 3px, var(--color-hairline) 3px 5px)';

/**
 * One bay. The shape carries the state, not the color: free is an empty green outline, occupied a
 * car, out of service a hatch with an x. Not interactive: the grid is hidden from assistive tech.
 */
export function SpaceCell({ code, state }: { code: string; state: SpaceStatus }) {
  if (state === 'occupied') {
    return (
      <i data-code={code} data-state={state} className="relative block aspect-[2/3] rounded-[3px] bg-lot-concrete">
        <CarShape />
      </i>
    );
  }
  if (state === 'out_of_service') {
    return (
      <i
        data-code={code}
        data-state={state}
        style={{ backgroundImage: HATCH }}
        className="grid aspect-[2/3] place-items-center rounded-[3px] text-[0.7rem] leading-none font-extrabold text-ink shadow-[inset_0_0_0_1px_var(--color-ink-muted)]"
      >
        ×
      </i>
    );
  }
  return (
    <i
      data-code={code}
      data-state={state}
      className="block aspect-[2/3] rounded-[3px] bg-surface shadow-[inset_0_0_0_2px_var(--color-status-open)]"
    />
  );
}
