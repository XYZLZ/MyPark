'use client';

import { ApiError, type Page, type Vehicle } from '@mypark/contracts';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { useRef, useState } from 'react';
import { Banner } from '@/components/ui/Banner';
import { Button, buttonClass } from '@/components/ui/Button';
import { EmptyState } from '@/components/ui/EmptyState';
import { PageHeader } from '@/components/ui/PageHeader';
import { Skeleton } from '@/components/ui/Skeleton';
import { Link } from '@/i18n/navigation';
import { translateError, type Translator } from '@/lib/i18n/translate-error';
import { deleteVehicle, fetchVehicles, VEHICLES_QUERY_KEY } from './api';
import { VehicleRow } from './VehicleRow';

export function VehiclesScreen({
  initial,
  initialUpdatedAt,
  notice,
}: {
  initial: { items: Vehicle[]; page: Page };
  initialUpdatedAt: number;
  notice?: 'vehicle_not_found';
}) {
  const t = useTranslations('vehicles');
  const translate: Translator = useTranslations();
  const queryClient = useQueryClient();
  const [confirmingId, setConfirmingId] = useState<string | null>(null);
  const inFlight = useRef(false);

  const list = useQuery({
    queryKey: VEHICLES_QUERY_KEY,
    queryFn: fetchVehicles,
    initialData: initial,
    initialDataUpdatedAt: initialUpdatedAt,
  });

  const remove = useMutation({
    mutationFn: deleteVehicle,
    onSuccess: () => {
      setConfirmingId(null);
      void queryClient.invalidateQueries({ queryKey: VEHICLES_QUERY_KEY });
    },
    onError: (error) => {
      // Already gone elsewhere: close the prompt and reload the list.
      if (error instanceof ApiError && error.code === 'vehicle.not_found') {
        setConfirmingId(null);
        void queryClient.invalidateQueries({ queryKey: VEHICLES_QUERY_KEY });
      }
    },
    onSettled: () => {
      inFlight.current = false;
    },
  });

  function confirmDelete(id: string) {
    // The ref closes the gap before React re-renders with isPending (a double click).
    if (inFlight.current || remove.isPending) return;
    inFlight.current = true;
    remove.mutate(id);
  }

  function askDelete(id: string) {
    remove.reset();
    setConfirmingId(id);
  }

  const { items, page } = list.data;
  const atCap = items.length >= page.limit;
  const refetching = list.isFetching && !list.isError;
  const deleteFailure = remove.error
    ? remove.error instanceof ApiError
      ? translateError(translate, remove.error)
      : translate('errors.fallback')
    : null;

  const addAction = atCap ? (
    <div className="flex flex-col items-end gap-1 text-right">
      <Button disabled aria-describedby="vehicles-cap" className="h-11">
        {t('add')}
      </Button>
      <p id="vehicles-cap" className="max-w-xs text-sm text-ink-muted">
        {t('capReached', { limit: page.limit })}
      </p>
    </div>
  ) : (
    <Link href="/vehicles/new" className={`${buttonClass('primary')} h-11`}>
      {t('add')}
    </Link>
  );

  return (
    <div>
      <PageHeader title={t('title')} action={items.length > 0 ? addAction : undefined} />
      <p className="-mt-2 mb-4 text-sm text-ink-muted">{t('count', { count: items.length, limit: page.limit })}</p>

      {notice === 'vehicle_not_found' ? <Banner tone="error">{translate('errors.vehicle.not_found')}</Banner> : null}
      {deleteFailure ? <Banner tone="error">{deleteFailure}</Banner> : null}

      {list.isError ? (
        <div role="alert" className="flex flex-col items-start gap-3 rounded-panel bg-surface p-5">
          <p className="font-semibold text-ink">{t('loadError.title')}</p>
          <Button onClick={() => void list.refetch()}>{t('loadError.retry')}</Button>
        </div>
      ) : refetching ? (
        <div role="status" aria-label={t('loading')} className="flex flex-col gap-3 rounded-panel bg-surface p-5">
          {(items.length > 0 ? items : [null]).map((vehicle, index) => (
            <Skeleton key={vehicle?.id ?? index} className="h-16 w-full" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <div className="rounded-panel bg-surface">
          <EmptyState
            title={t('empty.title')}
            body={t('empty.body')}
            action={
              <Link href="/vehicles/new" className={buttonClass('primary')}>
                {t('add')}
              </Link>
            }
          />
        </div>
      ) : (
        <ul className="rounded-panel bg-surface px-[18px] py-0.5">
          {items.map((vehicle) => (
            <VehicleRow
              key={vehicle.id}
              vehicle={vehicle}
              confirming={confirmingId === vehicle.id}
              deleting={remove.isPending && confirmingId === vehicle.id}
              onAskDelete={() => askDelete(vehicle.id)}
              onCancelDelete={() => setConfirmingId(null)}
              onConfirmDelete={() => confirmDelete(vehicle.id)}
            />
          ))}
        </ul>
      )}
    </div>
  );
}
