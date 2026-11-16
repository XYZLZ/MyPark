import { Stack } from 'expo-router';
import { useTranslation } from 'react-i18next';

import { tokens } from '@/design/tokens';

/** The Inicio tab: the live lot, which draws its own header, and the full map under a navigation header. */
export default function HomeLayout() {
  const { t } = useTranslation();
  return (
    <Stack screenOptions={{ headerShown: false }}>
      <Stack.Screen name="index" />
      <Stack.Screen
        name="map"
        options={{
          headerShown: true,
          title: t('spaces.title'),
          headerStyle: { backgroundColor: tokens.brandNavy },
          headerTintColor: tokens.brandPrimaryContrast,
          headerBackButtonDisplayMode: 'minimal',
        }}
      />
    </Stack>
  );
}
