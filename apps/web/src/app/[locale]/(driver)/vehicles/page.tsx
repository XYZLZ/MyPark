import type { Metadata } from 'next';
import { getTranslations } from 'next-intl/server';
import { VehiclesScreen } from '@/features/vehicles/VehiclesScreen';
import { listVehicles } from '@/lib/api/server';

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('vehicles');
  return { title: t('title') };
}

type SearchParams = Promise<Record<string, string | string[] | undefined>>;

export default async function VehiclesPage({ searchParams }: { searchParams: SearchParams }) {
  const { notice } = await searchParams;
  const initial = await listVehicles();
  const fetchedAt = Date.now();

  return (
    <VehiclesScreen
      initial={initial}
      initialUpdatedAt={fetchedAt}
      notice={notice === 'vehicle_not_found' ? 'vehicle_not_found' : undefined}
    />
  );
}
