import '../global.css';

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { getLocales } from 'expo-localization';
import { Stack } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { useEffect, useState } from 'react';
import { I18nextProvider } from 'react-i18next';

import { brand } from '@/design/tokens';
import { initI18n, pickLanguage } from '@/lib/i18n';
import { useSettingsStore } from '@/store/settings';

function deviceLanguageTags(): string[] {
  return getLocales().map((locale) => locale.languageTag);
}

export default function RootLayout() {
  const chosenLanguage = useSettingsStore((s) => s.language);
  const language = chosenLanguage ?? pickLanguage(deviceLanguageTags());

  const [queryClient] = useState(() => new QueryClient());
  const [i18n] = useState(() => initI18n(language));

  useEffect(() => {
    if (i18n.language !== language) void i18n.changeLanguage(language);
  }, [i18n, language]);

  return (
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <StatusBar style="light" />
        <Stack
          screenOptions={{
            headerStyle: { backgroundColor: brand.primary },
            headerTintColor: brand.primaryContrast,
          }}
        >
          <Stack.Screen name="(auth)" options={{ headerShown: false }} />
        </Stack>
      </I18nextProvider>
    </QueryClientProvider>
  );
}
