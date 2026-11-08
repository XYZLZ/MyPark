/** Loading placeholder. Size it with `className`; the pulse stops under reduced motion. */
export function Skeleton({ className = 'h-12 w-full' }: { className?: string }) {
  return <div aria-hidden="true" className={`animate-pulse rounded-control bg-hairline motion-reduce:animate-none ${className}`} />;
}
