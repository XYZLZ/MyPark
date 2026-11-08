import type { Metadata } from 'next';
import { getLocale, getTranslations } from 'next-intl/server';
import { VehiclesScreen } from '@/features/vehicles/VehiclesScreen';
import { RefreshOnMount } from '@/features/session/RefreshOnMount';
import { resolveLocale } from '@/i18n/routing';
import { listVehicles } from '@/lib/api/server';
import { requireDriver } from '@/lib/auth/guards';

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('vehicles');
  return { title: t('title') };
}

// Outside the component so the render stays pure: this is the moment the RSC data was read.
const timestamp = () => Date.now();

type SearchParams = Promise<Record<string, string | string[] | undefined>>;

export default async function VehiclesPage({ searchParams }: { searchParams: SearchParams }) {
  const { notice } = await searchParams;
  const gate = await requireDriver(resolveLocale(await getLocale()));
  // No data is read for a non-ready state; a refresh re-runs the layout, which shows the right panel.
  if (gate.state !== 'ready') return <RefreshOnMount />;
  const initial = await listVehicles();
  const fetchedAt = timestamp();

  return (
    <VehiclesScreen
      initial={initial}
      initialUpdatedAt={fetchedAt}
      notice={notice === 'vehicle_not_found' ? 'vehicle_not_found' : undefined}
    />
  );
}
