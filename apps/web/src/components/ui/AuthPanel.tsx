import type { ReactNode } from 'react';

const BAY_LINE = 'absolute -bottom-7 -top-7 w-1.5 rounded-sm bg-white/25';

/** Soft white panel on navy, between two faint rounded bay lines. */
export function AuthPanel({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="flex min-h-[480px] justify-center bg-brand-primary-strong px-4 py-7">
      <div className="relative flex w-full max-w-[346px]">
        <span aria-hidden="true" className={`${BAY_LINE} -left-3.5`} />
        <span aria-hidden="true" className={`${BAY_LINE} -right-3.5`} />
        <section className="relative flex-1 self-start rounded-panel bg-surface px-5 py-6">
          <h1 className="mb-4 text-2xl font-bold text-ink">{title}</h1>
          {children}
        </section>
      </div>
    </div>
  );
}
