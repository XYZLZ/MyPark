import type { Metadata } from 'next';
import { getTranslations } from 'next-intl/server';
import { VehicleForm } from '@/features/vehicles/VehicleForm';

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('vehicles.form');
  return { title: t('createTitle') };
}

export default function NewVehiclePage() {
  return <VehicleForm mode="create" />;
}
