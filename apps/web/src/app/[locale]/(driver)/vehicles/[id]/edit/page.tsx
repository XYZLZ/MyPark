import { ApiError, type Vehicle } from '@mypark/contracts';
import type { Metadata } from 'next';
import { getLocale, getTranslations } from 'next-intl/server';
import { VehicleForm } from '@/features/vehicles/VehicleForm';
import { redirect } from '@/i18n/navigation';
import { resolveLocale } from '@/i18n/routing';
import { getVehicle } from '@/lib/api/server';
import { requireDriver } from '@/lib/auth/guards';

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('vehicles.form');
  return { title: t('editTitle') };
}

export default async function EditVehiclePage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const locale = await getLocale();
  const gate = await requireDriver(resolveLocale(locale));
  if (gate.state !== 'ready') return null;

  let vehicle: Vehicle;
  try {
    vehicle = await getVehicle(id);
  } catch (error) {
    // Only vehicle.not_found (404) means "not found" (PF7); anything else, a 422 included, reaches the error page.
    if (error instanceof ApiError && error.code === 'vehicle.not_found') {
      redirect({ href: '/vehicles?notice=vehicle_not_found', locale });
    }
    throw error;
  }

  return <VehicleForm mode="edit" vehicle={vehicle} />;
}
