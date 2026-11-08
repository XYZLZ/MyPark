import type { ReactNode } from 'react';

export function EmptyState({ title, body, action }: { title: string; body: string; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center gap-2 px-4 py-10 text-center">
      <div aria-hidden="true" className="mb-1.5 h-[60px] w-[142px] rounded-plate border-2 border-dashed border-control-line" />
      <h2 className="text-lg font-semibold text-ink">{title}</h2>
      <p className="max-w-sm text-ink-muted">{body}</p>
      {action ? <div className="mt-3">{action}</div> : null}
    </div>
  );
}
