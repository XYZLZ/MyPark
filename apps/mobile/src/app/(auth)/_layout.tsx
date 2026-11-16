import { Stack } from 'expo-router';
import { useTranslation } from 'react-i18next';

import { tokens } from '@/design/tokens';

// Welcome is the first screen even when a notice sends the user back here.
export const unstable_settings = { anchor: 'welcome' };

/** Signed-out flow: welcome, login, sign up, verify and the legal pages. The full map has a navigation header with a way back. */
export default function AuthLayout() {
  const { t } = useTranslation();
  return (
    <Stack screenOptions={{ headerShown: false }}>
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
