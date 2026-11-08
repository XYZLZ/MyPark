/** A license plate: real text (readable by assistive tech) under a navy band. */
export function Plate({ value }: { value: string }) {
  return (
    <span className="inline-flex w-[142px] flex-col overflow-hidden rounded-plate border-[1.5px] border-ink bg-surface">
      <span aria-hidden="true" className="block h-2 bg-brand-primary" />
      <span className="px-1 pb-[9px] pt-2 text-center text-3xl font-semibold leading-none tracking-wider text-ink">
        {value}
      </span>
    </span>
  );
}
