import { useTranslations } from 'next-intl';
import type { ApiHealthState } from '@mypark/contracts';

const DOT_CLASS: Record<ApiHealthState, string> = {
  healthy: 'bg-green-600',
  not_ready: 'bg-amber-500',
  unavailable: 'bg-red-600',
};

/** Presentational: shows the localized API health. The text carries the state, not the color. */
export function HealthStatus({ state }: { state: ApiHealthState }) {
  const t = useTranslations('health');

  return (
    <p
      role="status"
      data-state={state}
      className="inline-flex items-center gap-2 rounded-full border border-neutral-300 bg-white px-4 py-2 text-sm font-medium text-neutral-900"
    >
      <span aria-hidden="true" className={`size-2.5 rounded-full ${DOT_CLASS[state]}`} />
      {t(state)}
    </p>
  );
}
