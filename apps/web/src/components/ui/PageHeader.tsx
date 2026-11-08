import type { ReactNode, Ref } from 'react';

/** `titleRef` lets a screen move focus to the heading (the heading is focusable by script only). */
export function PageHeader({ title, action, titleRef }: { title: string; action?: ReactNode; titleRef?: Ref<HTMLHeadingElement> }) {
  return (
    <div className="mb-3.5 flex items-center justify-between gap-4">
      <h1 ref={titleRef} tabIndex={titleRef ? -1 : undefined} className="text-2xl font-bold text-ink outline-none">
        {title}
      </h1>
      {action}
    </div>
  );
}
