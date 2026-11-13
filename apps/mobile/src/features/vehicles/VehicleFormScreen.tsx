import { ApiError, vehicleInputSchema, type Vehicle } from '@mypark/contracts';
import { useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';
import { useRouter } from 'expo-router';
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, View } from 'react-native';

import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { ScreenScroll } from '@/components/ui/ScreenScroll';
import { Skeleton } from '@/components/ui/Skeleton';
import { FormField } from '@/features/auth/FormField';
import { useApiForm } from '@/lib/forms/use-api-form';

import { createVehicle, deleteVehicle, getVehicle, updateVehicle, VEHICLES_QUERY_KEY } from './api';

type VehicleField = 'plate' | 'make' | 'model' | 'color';
const FIELDS: VehicleField[] = ['plate', 'make', 'model', 'color'];

const detailKey = (id: string) => [...VEHICLES_QUERY_KEY, id] as const;

/** The list is stale after any change here. Only the list is refetched: the detail query is updated or dropped by the caller. */
function refreshList(queryClient: QueryClient) {
  void queryClient.invalidateQueries({ queryKey: VEHICLES_QUERY_KEY, exact: true });
}

/** Add (no id) or edit (with the vehicle's id) one vehicle. */
export function VehicleFormScreen({ vehicleId }: { vehicleId?: string }) {
  return vehicleId === undefined ? <VehicleFields vehicle={undefined} /> : <EditVehicle vehicleId={vehicleId} />;
}

function EditVehicle({ vehicleId }: { vehicleId: string }) {
  const { t } = useTranslation();
  const router = useRouter();
  const queryClient = useQueryClient();
  const detail = useQuery({ queryKey: detailKey(vehicleId), queryFn: () => getVehicle(vehicleId) });

  // Gone elsewhere (deleted on another device): back to a refreshed list.
  const missing = detail.error instanceof ApiError && detail.error.code === 'vehicle.not_found';
  useEffect(() => {
    if (!missing) return;
    queryClient.removeQueries({ queryKey: detailKey(vehicleId) });
    refreshList(queryClient);
    router.dismissTo('/vehicles');
  }, [missing, queryClient, router, vehicleId]);

  if (detail.data) return <VehicleFields key={detail.data.id} vehicle={detail.data} />;
  if (detail.isError && !missing) {
    return (
      <ScreenScroll>
        <Banner tone="error">{t('vehicles.loadError.title')}</Banner>
        <Button variant="primary" label={t('common.retry')} loading={detail.isFetching} onPress={() => void detail.refetch()} />
      </ScreenScroll>
    );
  }
  return (
    <ScreenScroll>
      <View accessible accessibilityRole="progressbar" accessibilityLabel={t('common.loading')} className="gap-4">
        <Skeleton className="h-12 w-full" />
        <Skeleton className="h-12 w-full" />
      </View>
    </ScreenScroll>
  );
}

function VehicleFields({ vehicle }: { vehicle: Vehicle | undefined }) {
  const { t } = useTranslation();
  const router = useRouter();
  const queryClient = useQueryClient();
  const deleting = useRef(false);
  const [deletingNow, setDeletingNow] = useState(false);

  const { form, lock, serverError, onSubmit, submitting, fail } = useApiForm({
    schema: vehicleInputSchema,
    defaultValues: {
      plate: vehicle?.plate ?? '',
      make: vehicle?.make ?? '',
      model: vehicle?.model ?? '',
      color: vehicle?.color ?? '',
    },
    submit: async (values) => {
      // The plate goes as typed; the server normalizes it.
      if (vehicle) {
        // PATCH is partial: only what changed goes out, so a concurrent edit of another field survives (ruling T6).
        const changes = Object.fromEntries(FIELDS.filter((name) => values[name] !== vehicle[name]).map((name) => [name, values[name]]));
        if (Object.keys(changes).length > 0) {
          const updated = await updateVehicle(vehicle.id, changes);
          queryClient.setQueryData(detailKey(vehicle.id), updated);
          refreshList(queryClient);
        }
      } else {
        await createVehicle(values);
        refreshList(queryClient);
      }
      router.dismissTo('/vehicles');
    },
  });
  const { control, setFocus } = form;

  function leave(id: string) {
    queryClient.removeQueries({ queryKey: detailKey(id) });
    refreshList(queryClient);
    router.dismissTo('/vehicles');
  }

  async function confirmDelete(target: Vehicle) {
    // Review Focus 4: one DELETE however fast the button is tapped.
    if (deleting.current) return;
    deleting.current = true;
    setDeletingNow(true);
    form.clearErrors('root.serverError');
    try {
      await deleteVehicle(target.id);
      leave(target.id);
    } catch (error) {
      // Already gone elsewhere is the outcome the user wanted.
      if (error instanceof ApiError && error.code === 'vehicle.not_found') {
        leave(target.id);
        return;
      }
      fail(error);
    } finally {
      deleting.current = false;
      setDeletingNow(false);
    }
  }

  function askDelete(target: Vehicle) {
    Alert.alert(t('vehicles.delete.confirm', { plate: target.plate }), undefined, [
      { text: t('vehicles.delete.cancel'), style: 'cancel' },
      { text: t('vehicles.delete.confirmAction'), style: 'destructive', onPress: () => void confirmDelete(target) },
    ]);
  }

  return (
    <ScreenScroll>
      {serverError ? <Banner tone="error">{serverError}</Banner> : null}
      <FormField
        control={control}
        name="plate"
        label={t('vehicles.form.plate')}
        hint={t('vehicles.form.plateHint')}
        autoCapitalize="characters"
        autoCorrect={false}
        spellCheck={false}
        returnKeyType="next"
        onSubmitEditing={() => setFocus('make')}
      />
      <FormField control={control} name="make" label={t('vehicles.form.make')} autoCapitalize="words" returnKeyType="next" onSubmitEditing={() => setFocus('model')} />
      <FormField control={control} name="model" label={t('vehicles.form.model')} autoCapitalize="words" returnKeyType="next" onSubmitEditing={() => setFocus('color')} />
      <FormField control={control} name="color" label={t('vehicles.form.color')} autoCapitalize="words" returnKeyType="go" onSubmitEditing={() => void onSubmit()} />
      <Button
        variant="primary"
        label={vehicle ? t('vehicles.form.save') : t('vehicles.form.create')}
        loading={submitting}
        disabled={lock.locked || deletingNow}
        onPress={() => void onSubmit()}
      />
      {vehicle ? <Button variant="danger" label={t('vehicles.delete.action')} loading={deletingNow} disabled={submitting || deletingNow} onPress={() => askDelete(vehicle)} /> : null}
    </ScreenScroll>
  );
}
