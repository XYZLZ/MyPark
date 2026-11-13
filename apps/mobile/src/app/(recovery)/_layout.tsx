import { Stack } from 'expo-router';
import { useTranslation } from 'react-i18next';

import { tokens } from '@/design/tokens';

/** Password recovery, reachable signed in (from Profile) and signed out. The header gives it a way back. */
export default function RecoveryLayout() {
  const { t } = useTranslation();
  return (
    <Stack
      screenOptions={{
        headerStyle: { backgroundColor: tokens.brandPrimary },
        headerTintColor: tokens.brandPrimaryContrast,
        headerBackButtonDisplayMode: 'minimal',
      }}
    >
      <Stack.Screen name="forgot-password" options={{ title: t('auth.forgot.title') }} />
      <Stack.Screen name="reset-password" options={{ title: t('auth.reset.title') }} />
    </Stack>
  );
}
