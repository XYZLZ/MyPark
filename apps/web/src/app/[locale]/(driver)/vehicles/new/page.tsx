import type { Metadata } from 'next';
import { getLocale, getTranslations } from 'next-intl/server';
import { VehicleForm } from '@/features/vehicles/VehicleForm';
import { RefreshOnMount } from '@/features/session/RefreshOnMount';
import { resolveLocale } from '@/i18n/routing';
import { requireDriver } from '@/lib/auth/guards';

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('vehicles.form');
  return { title: t('createTitle') };
}

export default async function NewVehiclePage() {
  const gate = await requireDriver(resolveLocale(await getLocale()));
  if (gate.state !== 'ready') return <RefreshOnMount />;

  return <VehicleForm mode="create" />;
}
