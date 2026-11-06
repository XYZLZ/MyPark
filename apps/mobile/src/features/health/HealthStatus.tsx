import type { ApiHealthState } from '@mypark/contracts';
import { useTranslation } from 'react-i18next';
import { Text, View } from 'react-native';

type Props = { state: ApiHealthState };

/*
 * The label carries the meaning; the colored dot is decorative.
 * Placeholder pairs (text on the white pill, WCAG AA needs 4.5:1; hex = sRGB of the Tailwind v4 oklch value):
 *   healthy     text-green-800 #016630: 7.13:1
 *   not_ready   text-amber-800 #973c00: 7.09:1
 *   unavailable text-red-800   #9f0712: 8.36:1
 */
const tone = {
  healthy: { dot: 'bg-green-600', text: 'text-green-800' },
  not_ready: { dot: 'bg-amber-500', text: 'text-amber-800' },
  unavailable: { dot: 'bg-red-600', text: 'text-red-800' },
} as const satisfies Record<ApiHealthState, { dot: string; text: string }>;

export function HealthStatus({ state }: Props) {
  const { t } = useTranslation();
  const { dot, text } = tone[state];

  return (
    <View className="flex-row items-center gap-2 rounded-full bg-white px-4 py-2">
      <View className={`size-3 rounded-full ${dot}`} accessible={false} importantForAccessibility="no" />
      <Text
        accessibilityRole="text"
        accessibilityLiveRegion="polite"
        className={`text-base font-semibold ${text}`}
      >
        {t(`health.${state}`)}
      </Text>
    </View>
  );
}
