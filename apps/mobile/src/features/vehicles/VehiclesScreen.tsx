import { useQuery } from '@tanstack/react-query';
import type { Vehicle } from '@mypark/contracts';
import { useRouter } from 'expo-router';
import { useTranslation } from 'react-i18next';
import { Pressable, View } from 'react-native';

import { AppText } from '@/components/ui/AppText';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { EmptyState } from '@/components/ui/EmptyState';
import { Plate } from '@/components/ui/Plate';
import { ScreenScroll } from '@/components/ui/ScreenScroll';
import { Skeleton } from '@/components/ui/Skeleton';

import { listVehicles, VEHICLES_QUERY_KEY } from './api';

function VehicleRow({ vehicle, onPress }: { vehicle: Vehicle; onPress: () => void }) {
  const { t } = useTranslation();
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={t('vehicles.editFor', { plate: vehicle.plate })}
      onPress={onPress}
      className="min-h-12 gap-2 rounded-panel border border-hairline bg-surface p-4 active:bg-brand-primary-tint focus:border-2 focus:border-brand-primary"
    >
      <Plate value={vehicle.plate} />
      <AppText>{`${vehicle.make} ${vehicle.model} · ${vehicle.color}`}</AppText>
    </Pressable>
  );
}

/** The signed-in home: the driver's vehicles, how many of the allowed ones are used, and the way to add one. */
export function VehiclesScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const list = useQuery({ queryKey: VEHICLES_QUERY_KEY, queryFn: listVehicles });

  if (list.isPending) {
    return (
      <ScreenScroll>
        <View accessible accessibilityRole="progressbar" accessibilityLabel={t('vehicles.loading')} className="gap-3">
          <Skeleton className="h-28 w-full" />
          <Skeleton className="h-28 w-full" />
        </View>
      </ScreenScroll>
    );
  }

  if (list.isError) {
    return (
      <ScreenScroll>
        <Banner tone="error">{t('vehicles.loadError.title')}</Banner>
        <Button variant="primary" label={t('common.retry')} loading={list.isFetching} onPress={() => void list.refetch()} />
      </ScreenScroll>
    );
  }

  const { items, page } = list.data;
  const atCap = items.length >= page.limit;
  const addVehicle = () => router.push('/vehicles/new');

  if (items.length === 0) {
    return (
      <ScreenScroll>
        <EmptyState
          title={t('vehicles.empty.title')}
          body={t('vehicles.empty.body')}
          action={<Button variant="primary" label={t('vehicles.add')} onPress={addVehicle} />}
        />
      </ScreenScroll>
    );
  }

  return (
    <ScreenScroll>
      <AppText variant="caption">{t('vehicles.count', { used: items.length, limit: page.limit })}</AppText>
      {items.map((vehicle) => (
        <VehicleRow key={vehicle.id} vehicle={vehicle} onPress={() => router.push(`/vehicles/${vehicle.id}`)} />
      ))}
      <Button variant="primary" label={t('vehicles.add')} disabled={atCap} onPress={addVehicle} />
      {/* The reason is on screen, not only in a disabled style. */}
      {atCap ? <AppText variant="caption">{t('vehicles.capReached', { limit: page.limit })}</AppText> : null}
    </ScreenScroll>
  );
}
