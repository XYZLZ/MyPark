import { useLocalSearchParams } from 'expo-router';

import { VehicleFormScreen } from '@/features/vehicles/VehicleFormScreen';

export default function EditVehicleRoute() {
  const { id } = useLocalSearchParams<{ id: string }>();
  return <VehicleFormScreen vehicleId={id} />;
}
