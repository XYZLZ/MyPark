import type { ReactNode } from 'react';

export function PageHeader({ title, action }: { title: string; action?: ReactNode }) {
  return (
    <div className="mb-3.5 flex items-center justify-between gap-4">
      <h1 className="text-2xl font-bold text-ink">{title}</h1>
      {action}
    </div>
  );
}
