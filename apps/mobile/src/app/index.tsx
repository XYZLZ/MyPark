import { useQuery } from '@tanstack/react-query';
import { Stack } from 'expo-router';
import { useTranslation } from 'react-i18next';
import { ActivityIndicator, Text, View } from 'react-native';

import { HealthStatus } from '@/features/health/HealthStatus';
import { getApiBaseUrl } from '@/lib/env';
import { createHttpClient, getApiHealth } from '@/services/httpClient';

const apiClient = createHttpClient(getApiBaseUrl());
const HEALTH_REFRESH_MS = 15_000;

export default function HomeScreen() {
  const { t } = useTranslation();
  const health = useQuery({
    queryKey: ['health'],
    queryFn: () => getApiHealth(apiClient),
    refetchInterval: HEALTH_REFRESH_MS,
  });

  return (
    <View className="flex-1 items-center justify-center gap-6 bg-brand-primary px-6">
      <Stack.Screen options={{ title: t('common.appName') }} />
      <Text accessibilityRole="header" className="text-center text-2xl font-bold text-brand-primary-contrast">
        {t('common.appName')}
      </Text>
      {health.data ? (
        <HealthStatus state={health.data} />
      ) : (
        <ActivityIndicator
          accessibilityLabel={t('common.loading')}
          colorClassName="accent-brand-primary-contrast"
        />
      )}
    </View>
  );
}
