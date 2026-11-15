import { Skeleton } from '@/components/ui/Skeleton';

/** While the server reads the lot. The hero and the actions are already on screen (design §6.1). */
export function LotSkeleton() {
  return (
    <div aria-hidden="true" className="flex flex-col gap-3 lg:col-start-2 lg:row-start-1">
      <Skeleton className="h-24 w-full" />
      <Skeleton className="h-28 w-full" />
      <Skeleton className="h-10 w-full" />
    </div>
  );
}
