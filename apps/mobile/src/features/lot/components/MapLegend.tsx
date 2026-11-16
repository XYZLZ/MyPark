import { useTranslation } from 'react-i18next';
import { View } from 'react-native';

import { AppText } from '@/components/ui/AppText';

import { SpaceShape } from './SpaceCell';

const ITEMS = [
  { state: 'free', label: 'spaces.legend.free' },
  { state: 'occupied', label: 'spaces.legend.occupied' },
  { state: 'out_of_service', label: 'spaces.legend.outOfService' },
] as const;

/** Each cell shape next to its name (design §9.3). The swatches are decoration; the words are plain text. */
export function MapLegend() {
  const { t } = useTranslation();
  return (
    <View className="flex-row flex-wrap justify-center gap-x-5 gap-y-2">
      {ITEMS.map(({ state, label }) => (
        <View key={state} className="flex-row items-center gap-2">
          <View accessibilityElementsHidden importantForAccessibility="no-hide-descendants">
            <SpaceShape state={state} size={16} />
          </View>
          <AppText variant="caption">{t(label)}</AppText>
        </View>
      ))}
    </View>
  );
}
