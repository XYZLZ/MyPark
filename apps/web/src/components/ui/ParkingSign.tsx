const SIZES = {
  sm: 'size-[26px] rounded-lg text-lg',
  lg: 'size-28 rounded-3xl text-[80px]',
} as const;

/** The "P" mark. Decorative: the wordmark next to it carries the name. */
export function ParkingSign({ size }: { size: keyof typeof SIZES }) {
  return (
    <span
      aria-hidden="true"
      className={`inline-grid place-items-center bg-surface font-bold leading-none text-brand-primary ${SIZES[size]}`}
    >
      P
    </span>
  );
}
