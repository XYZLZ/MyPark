import { View } from 'react-native';

import { AppText } from './AppText';

/** A license plate as real text, so a screen reader reads it (spec §11). The font scale is capped in AppText. */
export function Plate({ value }: { value: string }) {
  return (
    <View className="w-36 self-start overflow-hidden rounded-plate border-2 border-ink bg-surface">
      <View accessible={false} importantForAccessibility="no-hide-descendants" className="h-2 bg-brand-primary" />
      <AppText weight="plate" className="px-1 pb-2 pt-2 text-center text-3xl tracking-wider">
        {value}
      </AppText>
    </View>
  );
}
