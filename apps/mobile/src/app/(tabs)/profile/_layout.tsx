import { Stack } from 'expo-router';
import { useTranslation } from 'react-i18next';

import { tokens } from '@/design/tokens';

/** The Profile tab: the profile, with account deletion pushed inside the tab. */
export default function ProfileLayout() {
  const { t } = useTranslation();
  return (
    <Stack
      screenOptions={{
        headerStyle: { backgroundColor: tokens.brandPrimary },
        headerTintColor: tokens.brandPrimaryContrast,
        headerBackButtonDisplayMode: 'minimal',
      }}
    >
      <Stack.Screen name="index" options={{ title: t('profile.title') }} />
      <Stack.Screen name="delete" options={{ title: t('profile.danger.title') }} />
    </Stack>
  );
}
