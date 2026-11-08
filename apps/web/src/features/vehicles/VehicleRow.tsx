'use client';

import type { Vehicle } from '@mypark/contracts';
import { useTranslations } from 'next-intl';
import { useEffect, useRef } from 'react';
import { Button } from '@/components/ui/Button';
import { Plate } from '@/components/ui/Plate';
import { Link } from '@/i18n/navigation';

const ACTION =
  'rounded-full px-3 py-2 text-sm font-medium outline-none transition-colors ' +
  'focus-visible:shadow-[0_0_0_2px_var(--color-surface),0_0_0_5px_var(--color-brand-primary)]';

/** One vehicle. Delete asks for confirmation inside the row, so there is no dialog to trap focus (CC-22). */
export function VehicleRow({
  vehicle,
  confirming,
  deleting,
  onAskDelete,
  onCancelDelete,
  onConfirmDelete,
}: {
  vehicle: Vehicle;
  confirming: boolean;
  deleting: boolean;
  onAskDelete(): void;
  onCancelDelete(): void;
  onConfirmDelete(): void;
}) {
  const t = useTranslations('vehicles');
  const deleteRef = useRef<HTMLButtonElement>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const wasConfirming = useRef(false);

  // Focus follows the row: to the safe choice when the prompt opens, back to Delete when it closes.
  useEffect(() => {
    if (confirming) cancelRef.current?.focus();
    else if (wasConfirming.current) deleteRef.current?.focus();
    wasConfirming.current = confirming;
  }, [confirming]);

  return (
    <li className="flex flex-wrap items-center gap-x-[18px] gap-y-3 py-3.5 not-first:border-t not-first:border-hairline">
      <Plate value={vehicle.plate} />
      <div className="min-w-32 flex-1">
        <strong className="block font-semibold text-ink">{`${vehicle.make} ${vehicle.model}`}</strong>
        <span className="text-sm text-ink-muted">{vehicle.color}</span>
      </div>
      {confirming ? (
        <div className="flex w-full flex-wrap items-center gap-2 rounded-control bg-danger-tint p-3 sm:w-auto">
          <p className="text-sm font-medium text-ink">{t('delete.confirm', { plate: vehicle.plate })}</p>
          <Button
            variant="danger"
            className="h-10 px-4 text-sm"
            loading={deleting}
            loadingLabel={t('delete.deleting')}
            onClick={onConfirmDelete}
          >
            {t('delete.confirmAction')}
          </Button>
          <Button ref={cancelRef} variant="quiet" className="h-10 px-4 text-sm" disabled={deleting} onClick={onCancelDelete}>
            {t('delete.cancel')}
          </Button>
        </div>
      ) : (
        <div className="flex gap-1">
          <Link
            href={`/vehicles/${vehicle.id}/edit`}
            aria-label={t('editFor', { plate: vehicle.plate })}
            className={`${ACTION} text-brand-primary hover:bg-brand-primary-tint`}
          >
            {t('edit')}
          </Link>
          <button
            ref={deleteRef}
            type="button"
            aria-label={t('delete.actionFor', { plate: vehicle.plate })}
            onClick={onAskDelete}
            className={`${ACTION} text-danger hover:bg-danger-tint`}
          >
            {t('delete.action')}
          </button>
        </div>
      )}
    </li>
  );
}
