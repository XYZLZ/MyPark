import '../global.css';

import {
  Barlow_400Regular,
  Barlow_500Medium,
  Barlow_600SemiBold,
  Barlow_700Bold,
} from '@expo-google-fonts/barlow';
import { BarlowSemiCondensed_600SemiBold } from '@expo-google-fonts/barlow-semi-condensed';
import { focusManager, QueryClientProvider } from '@tanstack/react-query';
import { useFonts } from 'expo-font';
import { getLocales } from 'expo-localization';
import { Stack } from 'expo-router';
import * as SplashScreen from 'expo-splash-screen';
import { StatusBar } from 'expo-status-bar';
import { useEffect, useState } from 'react';
import { I18nextProvider } from 'react-i18next';
import { AppState, Platform } from 'react-native';

import { tokens } from '@/design/tokens';
import { selectRootRoute } from '@/features/session/root-route';
import { initI18n, pickLanguage } from '@/lib/i18n';
import { queryClient, session } from '@/services/runtime';
import { useAuthStore } from '@/store/auth';
import { hydrateSettings, useSettingsStore } from '@/store/settings';

// Hold the splash until fonts, settings and the session are ready (spec 4.2).
void SplashScreen.preventAutoHideAsync();

function deviceLanguageTags(): string[] {
  return getLocales().map((locale) => locale.languageTag);
}

export default function RootLayout() {
  const [fontsLoaded, fontError] = useFonts({
    Barlow_400Regular,
    Barlow_500Medium,
    Barlow_600SemiBold,
    Barlow_700Bold,
    BarlowSemiCondensed_600SemiBold,
  });
  const hydrated = useSettingsStore((s) => s.hydrated);
  const chosenLanguage = useSettingsStore((s) => s.language);
  const status = useAuthStore((s) => s.status);
  const legalPending = useAuthStore((s) => s.legalPending);

  const language = chosenLanguage ?? pickLanguage(deviceLanguageTags());
  const [i18n] = useState(() => initI18n(language));
  const route = selectRootRoute(status, legalPending);
  // A font error must not trap the app on the splash: system fonts are an acceptable fallback.
  const ready = (fontsLoaded || fontError !== null) && hydrated && route !== 'restoring';

  useEffect(() => {
    void hydrateSettings();
    void session.restore();
  }, []);

  useEffect(() => {
    if (i18n.language !== language) void i18n.changeLanguage(language);
  }, [i18n, language]);

  // TanStack's React Native guide: refetch stale queries when the app returns to the foreground.
  useEffect(() => {
    if (Platform.OS === 'web') return;
    const subscription = AppState.addEventListener('change', (state) => focusManager.setFocused(state === 'active'));
    return () => subscription.remove();
  }, []);

  useEffect(() => {
    if (ready) void SplashScreen.hideAsync();
  }, [ready]);

  if (!ready) return null;

  return (
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <StatusBar style="light" />
        <Stack
          screenOptions={{
            headerShown: false,
            headerStyle: { backgroundColor: tokens.brandPrimary },
            headerTintColor: tokens.brandPrimaryContrast,
          }}
        >
          <Stack.Protected guard={route === 'auth'}>
            <Stack.Screen name="(auth)" />
          </Stack.Protected>
          <Stack.Protected guard={route === 'gate'}>
            <Stack.Screen name="(gate)" />
          </Stack.Protected>
          <Stack.Protected guard={route === 'tabs'}>
            <Stack.Screen name="(tabs)" />
          </Stack.Protected>
          <Stack.Protected guard={route === 'restore-failed'}>
            <Stack.Screen name="restore-failed" />
          </Stack.Protected>
          {/* Reachable signed in (from Profile) and signed out, so it sits outside the guards. */}
          <Stack.Screen name="(recovery)" />
        </Stack>
      </I18nextProvider>
    </QueryClientProvider>
  );
}
