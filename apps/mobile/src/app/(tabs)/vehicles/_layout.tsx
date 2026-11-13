import { Stack } from 'expo-router';
import { useTranslation } from 'react-i18next';

import { tokens } from '@/design/tokens';

/** The Vehicles tab: the list, with add and edit pushed inside the tab. */
export default function VehiclesLayout() {
  const { t } = useTranslation();
  return (
    <Stack
      screenOptions={{
        headerStyle: { backgroundColor: tokens.brandPrimary },
        headerTintColor: tokens.brandPrimaryContrast,
        headerBackButtonDisplayMode: 'minimal',
      }}
    >
      <Stack.Screen name="index" options={{ title: t('vehicles.title') }} />
      <Stack.Screen name="new" options={{ title: t('vehicles.form.createTitle') }} />
      <Stack.Screen name="[id]" options={{ title: t('vehicles.form.editTitle') }} />
    </Stack>
  );
}
